package tunnel

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	testAddrA   = "10.66.0.1"
	testAddrB   = "10.66.0.2"
	testEcho    = 8080
	testDoHPort = 8443
)

type keyPair struct {
	private [32]byte
	public  [32]byte
}

func newKeyPair(t *testing.T) keyPair {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var pair keyPair
	copy(pair.private[:], key.Bytes())
	copy(pair.public[:], key.PublicKey().Bytes())
	return pair
}

func mustAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatalf("parse addr %s: %v", raw, err)
	}
	return addr
}

func mustPrefix(t *testing.T, raw string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		t.Fatalf("parse prefix %s: %v", raw, err)
	}
	return prefix
}

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := conn.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}
	return uint16(port)
}

type pairOptions struct {
	withDoH bool
	stale   time.Duration
}

type testPair struct {
	a     *Device
	b     *Device
	doh   *testDoH
	portA uint16
	portB uint16
	keyA  keyPair
	keyB  keyPair
}

func startPair(t *testing.T, opts pairOptions) *testPair {
	t.Helper()
	portA, portB := freeUDPPort(t), freeUDPPort(t)
	for portB == portA {
		portB = freeUDPPort(t)
	}
	keyA, keyB := newKeyPair(t), newKeyPair(t)
	stale := opts.stale
	if stale == 0 {
		stale = DefaultStaleAfter
	}
	loopback := mustAddr(t, "127.0.0.1")

	cfgB := Config{
		PrivateKey:     keyB.private,
		PeerPublicKey:  keyA.public,
		Endpoint:       netip.AddrPortFrom(loopback, portA),
		AllowedIPs:     []netip.Prefix{mustPrefix(t, testAddrA+"/32")},
		LocalAddresses: []netip.Addr{mustAddr(t, testAddrB)},
		ListenPort:     portB,
		MTU:            DefaultMTU,
		Keepalive:      time.Second,
		StaleAfter:     stale,
	}
	b, err := Start(context.Background(), cfgB)
	if err != nil {
		t.Fatalf("start peer B: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	pair := &testPair{a: nil, b: b, portA: portA, portB: portB, keyA: keyA, keyB: keyB}
	cfgA := Config{
		PrivateKey:     keyA.private,
		PeerPublicKey:  keyB.public,
		Endpoint:       netip.AddrPortFrom(loopback, portB),
		AllowedIPs:     []netip.Prefix{mustPrefix(t, testAddrB+"/32")},
		LocalAddresses: []netip.Addr{mustAddr(t, testAddrA)},
		ListenPort:     portA,
		MTU:            DefaultMTU,
		Keepalive:      time.Second,
		StaleAfter:     stale,
	}
	if opts.withDoH {
		pair.doh = newTestDoH(t, b)
		cfgA.DoHURL = pair.doh.url
		cfgA.RootCAs = pair.doh.pool
	}
	a, err := Start(context.Background(), cfgA)
	if err != nil {
		t.Fatalf("start peer A: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	pair.a = a

	waitHealthy(t, a)
	waitHealthy(t, b)
	startEcho(t, b)
	return pair
}

func waitHealthy(t *testing.T, dev *Device) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if dev.Healthy() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tunnel did not become healthy, status %+v", dev.Status())
}

func waitUnhealthy(t *testing.T, dev *Device, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !dev.Healthy() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tunnel stayed healthy, status %+v", dev.Status())
}

func startEcho(t *testing.T, dev *Device) {
	t.Helper()
	listener, err := dev.net.ListenTCP(&net.TCPAddr{IP: net.IP(mustAddr(t, testAddrB).AsSlice()), Port: testEcho})
	if err != nil {
		t.Fatalf("listen in netstack: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
}

type testDoH struct {
	url  string
	pool *x509.CertPool
	hits atomic.Int64
}

func newTestDoH(t *testing.T, dev *Device) *testDoH {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate tls key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "geomind-test-doh"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.IP(mustAddr(t, testAddrB).AsSlice())},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)

	server := &testDoH{
		url:  "https://" + testAddrB + ":" + strconv.Itoa(testDoHPort) + "/dns-query",
		pool: pool,
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.hits.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, maxDoHResponseBytes))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		var parser dnsmessage.Parser
		header, err := parser.Start(body)
		if err != nil {
			http.Error(w, "decode", http.StatusBadRequest)
			return
		}
		question, err := parser.Question()
		if err != nil {
			http.Error(w, "question", http.StatusBadRequest)
			return
		}
		response := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID:                 header.ID,
				Response:           true,
				Authoritative:      true,
				RecursionAvailable: true,
			},
			Questions: []dnsmessage.Question{question},
		}
		if question.Type == dnsmessage.TypeA {
			response.Answers = []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{
					Name:  question.Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   60,
				},
				Body: &dnsmessage.AResource{A: [4]byte{10, 66, 0, 2}},
			}}
		}
		packed, err := response.Pack()
		if err != nil {
			http.Error(w, "pack", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(packed)
	})
	listener, err := dev.net.ListenTCP(&net.TCPAddr{IP: net.IP(mustAddr(t, testAddrB).AsSlice()), Port: testDoHPort})
	if err != nil {
		t.Fatalf("listen DoH in netstack: %v", err)
	}
	httpServer := &http.Server{Handler: handler}
	tlsListener := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	})
	go func() {
		_ = httpServer.Serve(tlsListener)
	}()
	t.Cleanup(func() {
		_ = httpServer.Close()
		_ = listener.Close()
	})
	return server
}

func TestTwoPeersCarryTrafficAndDoH(t *testing.T) {
	pair := startPair(t, pairOptions{withDoH: true})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pair.a.DialContext(ctx, "tcp", testAddrB+":"+strconv.Itoa(testEcho))
	if err != nil {
		t.Fatalf("dial by ip: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(reply) != "ping" {
		t.Fatalf("echo = %q", reply)
	}
	_ = conn.Close()

	before := pair.doh.hits.Load()
	hostConn, err := pair.a.DialContext(ctx, "tcp", "example.test:"+strconv.Itoa(testEcho))
	if err != nil {
		t.Fatalf("dial by hostname: %v", err)
	}
	defer hostConn.Close()
	if _, err := hostConn.Write([]byte("dns")); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply = make([]byte, 3)
	if _, err := io.ReadFull(hostConn, reply); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(reply) != "dns" {
		t.Fatalf("echo = %q", reply)
	}
	if hits := pair.doh.hits.Load(); hits <= before {
		t.Fatalf("DoH server saw no query (%d -> %d)", before, hits)
	}
}

func TestKillPeerStopsTrafficAndGoesUnhealthy(t *testing.T) {
	pair := startPair(t, pairOptions{stale: 3 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pair.a.DialContext(ctx, "tcp", testAddrB+":"+strconv.Itoa(testEcho))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write before kill: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("echo before kill: %v", err)
	}

	if err := pair.b.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	waitUnhealthy(t, pair.a, 10*time.Second)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("connection still delivered data after the peer was killed")
	}
	if _, err := pair.a.DialContext(context.Background(), "tcp", testAddrB+":"+strconv.Itoa(testEcho)); !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("dial after kill error = %v, want ErrUnhealthy", err)
	}
}

func TestHandshakeFailureStaysUnhealthy(t *testing.T) {
	key := newKeyPair(t)
	cfg := Config{
		PrivateKey:     key.private,
		PeerPublicKey:  newKeyPair(t).public,
		Endpoint:       netip.AddrPortFrom(mustAddr(t, "127.0.0.1"), freeUDPPort(t)),
		AllowedIPs:     []netip.Prefix{mustPrefix(t, "10.77.0.2/32")},
		LocalAddresses: []netip.Addr{mustAddr(t, "10.77.0.1")},
		ListenPort:     freeUDPPort(t),
		MTU:            DefaultMTU,
		Keepalive:      time.Second,
		StaleAfter:     time.Second,
	}
	dev, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = dev.Close() }()

	time.Sleep(1500 * time.Millisecond)
	if dev.Healthy() {
		t.Fatalf("tunnel reported healthy without a handshake, status %+v", dev.Status())
	}
	if _, err := dev.DialContext(context.Background(), "tcp", "10.77.0.2:80"); !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("dial error = %v, want ErrUnhealthy", err)
	}
}
