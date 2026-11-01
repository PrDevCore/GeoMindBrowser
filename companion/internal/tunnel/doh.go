package tunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const maxDoHResponseBytes = 64 << 10

type dohResolver struct {
	endpoint *url.URL
	client   *http.Client
}

// newDoHResolver builds a resolver whose only transport is the netstack, so
// queries leave the process exclusively inside the tunnel.
func newDoHResolver(ns *netstack.Net, rawURL string, roots *x509.CertPool) (*dohResolver, error) {
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("tunnel: invalid DoH URL")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" && network != "tcp6" {
				return nil, fmt.Errorf("tunnel: unsupported DoH network %q", network)
			}
			return ns.DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{
			ServerName: endpoint.Hostname(),
			RootCAs:    roots,
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2:  false,
		DisableCompression: true,
		MaxIdleConns:       2,
		IdleConnTimeout:    30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &dohResolver{endpoint: endpoint, client: client}, nil
}

// Lookup returns the A and AAAA answers for host. It accepts only names that
// can be encoded as a DNS question and never falls back to the host resolver.
func (r *dohResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return nil, errors.New("tunnel: empty hostname")
	}
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil, fmt.Errorf("tunnel: invalid hostname")
	}

	var (
		addrs    []netip.Addr
		firstErr error
	)
	for _, qtype := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		found, queryErr := r.query(ctx, name, qtype)
		if queryErr != nil {
			if firstErr == nil {
				firstErr = queryErr
			}
			continue
		}
		addrs = append(addrs, found...)
	}
	addrs = dedupeAddrs(addrs)
	if len(addrs) == 0 {
		if firstErr == nil {
			firstErr = errors.New("tunnel: no addresses")
		}
		return nil, firstErr
	}
	return addrs, nil
}

func (r *dohResolver) query(ctx context.Context, name dnsmessage.Name, qtype dnsmessage.Type) ([]netip.Addr, error) {
	question := dnsmessage.Question{
		Name:  name,
		Type:  qtype,
		Class: dnsmessage.ClassINET,
	}
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, fmt.Errorf("tunnel: generate query id: %w", err)
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{question},
	}
	wire, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("tunnel: encode query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint.String(), bytes.NewReader(wire))
	if err != nil {
		return nil, fmt.Errorf("tunnel: build DoH request: %w", err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tunnel: DoH request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunnel: DoH status %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/dns-message") {
		return nil, fmt.Errorf("tunnel: unexpected DoH content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDoHResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("tunnel: read DoH response: %w", err)
	}
	if len(body) > maxDoHResponseBytes {
		return nil, fmt.Errorf("tunnel: DoH response too large")
	}

	var parser dnsmessage.Parser
	header, err := parser.Start(body)
	if err != nil {
		return nil, fmt.Errorf("tunnel: decode DoH response: %w", err)
	}
	if header.ID != id || !header.Response {
		return nil, fmt.Errorf("tunnel: DoH response does not match query")
	}
	if header.RCode != dnsmessage.RCodeSuccess {
		return nil, fmt.Errorf("tunnel: DoH response code %s", header.RCode)
	}
	got, err := parser.Question()
	if err != nil {
		return nil, fmt.Errorf("tunnel: decode DoH question: %w", err)
	}
	if got.Name.String() != name.String() || got.Type != qtype || got.Class != dnsmessage.ClassINET {
		return nil, fmt.Errorf("tunnel: DoH question mismatch")
	}
	// Consume the remaining question section so the parser advances to the
	// answers; with exactly one question the section advances on the next call.
	if _, err := parser.Question(); !errors.Is(err, dnsmessage.ErrSectionDone) {
		if err != nil {
			return nil, fmt.Errorf("tunnel: decode DoH question: %w", err)
		}
		return nil, fmt.Errorf("tunnel: unexpected extra DoH question")
	}

	var addrs []netip.Addr
	for {
		answer, err := parser.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tunnel: decode DoH answer: %w", err)
		}
		if answer.Name.String() != name.String() {
			if skipErr := parser.SkipAnswer(); skipErr != nil {
				return nil, fmt.Errorf("tunnel: decode DoH answer: %w", skipErr)
			}
			continue
		}
		switch answer.Type {
		case dnsmessage.TypeA:
			resource, err := parser.AResource()
			if err != nil {
				return nil, fmt.Errorf("tunnel: decode A answer: %w", err)
			}
			addrs = append(addrs, netip.AddrFrom4(resource.A))
		case dnsmessage.TypeAAAA:
			resource, err := parser.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("tunnel: decode AAAA answer: %w", err)
			}
			addrs = append(addrs, netip.AddrFrom16(resource.AAAA))
		default:
			if err := parser.SkipAnswer(); err != nil {
				return nil, fmt.Errorf("tunnel: decode DoH answer: %w", err)
			}
		}
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IsValid() && !addr.IsUnspecified() {
			out = append(out, addr)
		}
	}
	return out, nil
}

func dedupeAddrs(addrs []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(addrs))
	out := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}
