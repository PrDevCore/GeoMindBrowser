package tunnel

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

var (
	// ErrUnhealthy reports a dial attempt while the tunnel cannot carry traffic.
	ErrUnhealthy = errors.New("tunnel: tunnel is not healthy")
	// ErrClosed reports use of a device after it was closed.
	ErrClosed = errors.New("tunnel: device is closed")
)

// Device is a userspace WireGuard tunnel backed by gVisor netstack. All
// outbound traffic, including DNS, is dialed through the netstack, so there is
// no path to the host network.
type Device struct {
	sup    *Supervisor
	wg     *device.Device
	net    *netstack.Net
	doh    *dohResolver
	cancel context.CancelFunc
	closed chan struct{}

	closeOnce sync.Once
}

// Start creates and brings up a WireGuard device. Canceling ctx shuts the
// tunnel down.
func Start(ctx context.Context, cfg Config) (*Device, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("tunnel: invalid config: %w", err)
	}
	tunDev, ns, err := netstack.CreateNetTUN(cfg.LocalAddresses, nil, cfg.MTU)
	if err != nil {
		return nil, fmt.Errorf("tunnel: create netstack: %w", err)
	}

	d := &Device{
		sup:    NewSupervisor(cfg.StaleAfter, nil),
		net:    ns,
		closed: make(chan struct{}),
	}
	// The wireguard-go default logger writes to stdout, which would corrupt the
	// native messaging stream. Device logs are discarded.
	logger := &device.Logger{Verbosef: device.DiscardLogf, Errorf: device.DiscardLogf}
	// StdNetBind uses the Go network stack, which handles ICMP UDP resets on
	// Windows; the Windows ring bind does not and stops receiving after one.
	d.wg = device.NewDevice(tunDev, conn.NewStdNetBind(), logger)

	if cfg.DoHURL != "" {
		d.doh, err = newDoHResolver(ns, cfg.DoHURL, cfg.RootCAs)
		if err != nil {
			d.wg.Close()
			return nil, err
		}
	}

	d.sup.MarkStarting("", "")
	if err := d.wg.IpcSet(cfg.uapi()); err != nil {
		d.wg.Close()
		d.sup.MarkDown()
		return nil, fmt.Errorf("tunnel: configure device: %w", err)
	}
	if err := d.wg.Up(); err != nil {
		d.wg.Close()
		d.sup.MarkDown()
		return nil, fmt.Errorf("tunnel: bring device up: %w", err)
	}

	var runCtx context.Context
	runCtx, d.cancel = context.WithCancel(ctx)
	go d.monitor(runCtx)
	go func() {
		<-runCtx.Done()
		_ = d.Close()
	}()
	return d, nil
}

// Healthy reports whether the tunnel completed a handshake recently enough to
// carry traffic.
func (d *Device) Healthy() bool {
	select {
	case <-d.closed:
		return false
	default:
		return d.sup.Healthy()
	}
}

// Status is a snapshot for the extension.
func (d *Device) Status() Status {
	return d.sup.Status()
}

// DialContext dials through the tunnel only. Hostnames are resolved over DoH
// inside the tunnel; they are never passed to the host resolver.
func (d *Device) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("tunnel: unsupported network %q", network)
	}
	if !d.Healthy() {
		return nil, ErrUnhealthy
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("tunnel: invalid address: %w", err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return nil, fmt.Errorf("tunnel: invalid port")
	}

	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		return d.dialAddr(ctx, network, netip.AddrPortFrom(ip, uint16(port)))
	}

	if d.doh == nil {
		return nil, fmt.Errorf("tunnel: hostname resolution is disabled")
	}
	addrs, err := d.doh.Lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("tunnel: resolve host: %w", err)
	}
	var lastErr error
	for _, ip := range addrs {
		conn, dialErr := d.dialAddr(ctx, network, netip.AddrPortFrom(ip, uint16(port)))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no addresses returned")
	}
	return nil, fmt.Errorf("tunnel: dial host: %w", lastErr)
}

func (d *Device) dialAddr(ctx context.Context, network string, addr netip.AddrPort) (net.Conn, error) {
	select {
	case <-d.closed:
		return nil, ErrClosed
	default:
	}
	return d.net.DialContext(ctx, network, addr.String())
}

// Close tears down the device and marks it down. It is idempotent.
func (d *Device) Close() error {
	d.closeOnce.Do(func() {
		if d.cancel != nil {
			d.cancel()
		}
		d.wg.Close()
		d.sup.MarkDown()
		close(d.closed)
	})
	return nil
}

func (d *Device) monitor(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			text, err := d.wg.IpcGet()
			if err != nil {
				d.sup.MarkDown()
				continue
			}
			state, err := parseUAPIState(text)
			if err != nil {
				d.sup.MarkDown()
				continue
			}
			if !state.handshake.IsZero() {
				d.sup.MarkHandshake(state.handshake)
			}
		}
	}
}

// uapi renders the configuration protocol payload. It contains the private
// key and must never be logged or returned in an error.
func (c *Config) uapi() string {
	var b strings.Builder
	b.WriteString("private_key=")
	b.WriteString(hex.EncodeToString(c.PrivateKey[:]))
	b.WriteString("\nlisten_port=")
	b.WriteString(strconv.Itoa(int(c.ListenPort)))
	b.WriteString("\npublic_key=")
	b.WriteString(hex.EncodeToString(c.PeerPublicKey[:]))
	b.WriteString("\nendpoint=")
	b.WriteString(c.Endpoint.String())
	b.WriteString("\n")
	for _, prefix := range c.AllowedIPs {
		b.WriteString("allowed_ip=")
		b.WriteString(prefix.String())
		b.WriteString("\n")
	}
	b.WriteString("persistent_keepalive_interval=")
	b.WriteString(strconv.Itoa(int(c.Keepalive / time.Second)))
	b.WriteString("\n")
	return b.String()
}

type uapiState struct {
	listenPort uint16
	handshake  time.Time
}

// parseUAPIState extracts only non-secret fields. The private key in the UAPI
// output is intentionally left untouched.
func parseUAPIState(text string) (uapiState, error) {
	var (
		state    uapiState
		sec      int64
		nsec     int64
		haveSec  bool
		haveNsec bool
	)
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "listen_port":
			port, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return uapiState{}, fmt.Errorf("tunnel: parse listen port")
			}
			state.listenPort = uint16(port)
		case "last_handshake_time_sec":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return uapiState{}, fmt.Errorf("tunnel: parse handshake time")
			}
			sec = parsed
			haveSec = true
		case "last_handshake_time_nsec":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return uapiState{}, fmt.Errorf("tunnel: parse handshake time")
			}
			nsec = parsed
			haveNsec = true
		}
	}
	if haveSec && haveNsec && sec > 0 {
		state.handshake = time.Unix(sec, nsec)
	}
	return state, nil
}
