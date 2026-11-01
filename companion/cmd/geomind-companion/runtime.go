package main

import (
	"context"
	"net"
	"strings"
	"time"

	"geomind/companion/internal/ipc"
	"geomind/companion/internal/proxy"
	"geomind/companion/internal/tunnel"
)

// closedTunnel is the fail-closed path used when no WireGuard configuration is
// present. It refuses every dial, so the proxy is still a valid PAC target that
// blocks matched hosts instead of falling back to the real network.
type closedTunnel struct{}

func (closedTunnel) Healthy() bool { return false }

func (closedTunnel) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, tunnel.ErrUnhealthy
}

type runtimeOptions struct {
	ProxyAddr string
	ExitID    string
	Region    string
	Locale    string
	Getenv    func(string) string
}

// runtime owns the data plane: the WireGuard device (when configured) and the
// loopback SOCKS5 proxy that the PAC points at.
type runtime struct {
	device  *tunnel.Device
	proxy   *proxy.Server
	started time.Time
	exitID  string
	region  string
	locale  string
}

func startRuntime(ctx context.Context, opts runtimeOptions) (*runtime, error) {
	r := &runtime{started: time.Now(), exitID: opts.ExitID, region: opts.Region, locale: opts.Locale}
	var path proxy.Tunnel = closedTunnel{}

	if tunnelConfigured(opts.Getenv) {
		cfg, err := tunnel.FromEnv(opts.Getenv)
		if err != nil {
			return nil, err
		}
		device, err := tunnel.Start(ctx, cfg)
		if err != nil {
			return nil, err
		}
		r.device = device
		path = device
	}

	server, err := proxy.ListenAt(ctx, path, opts.ProxyAddr)
	if err != nil {
		if r.device != nil {
			_ = r.device.Close()
		}
		return nil, err
	}
	r.proxy = server
	return r, nil
}

// Serve runs the proxy until ctx is canceled or the listener fails. A failed
// listener closes the device so status cannot claim a working data plane while
// nothing is accepting connections.
func (r *runtime) Serve(ctx context.Context) {
	if err := r.proxy.Serve(ctx); err != nil && r.device != nil {
		_ = r.device.Close()
	}
}

func (r *runtime) ProxyAddr() string {
	return r.proxy.Addr()
}

func (r *runtime) Status() ipc.TunnelStatus {
	state := ipc.TunnelDown
	since := r.started
	if r.device != nil {
		snapshot := r.device.Status()
		if !snapshot.Since.IsZero() {
			since = snapshot.Since
		}
		switch snapshot.State {
		case tunnel.StateUp:
			state = ipc.TunnelUp
		case tunnel.StateStarting:
			state = ipc.TunnelStarting
		}
	}
	status := ipc.TunnelStatus{State: state, Since: since.UTC().Format(time.RFC3339)}
	if r.exitID != "" {
		status.Exit = &r.exitID
	}
	if r.region != "" {
		status.Region = &r.region
	}
	if r.locale != "" {
		status.Locale = &r.locale
	}
	return status
}

func (r *runtime) Close() {
	if r.proxy != nil {
		_ = r.proxy.Close()
	}
	if r.device != nil {
		_ = r.device.Close()
	}
}

// tunnelConfigured reports whether any required WireGuard value is present. A
// partial configuration is an error rather than a silent fallback to closed.
func tunnelConfigured(getenv func(string) string) bool {
	names := []string{
		"GEOMIND_WG_PRIVATE_KEY",
		"GEOMIND_WG_PEER_PUBLIC_KEY",
		"GEOMIND_WG_ENDPOINT",
		"GEOMIND_WG_LOCAL_ADDRS",
		"GEOMIND_WG_ALLOWED_IPS",
		"GEOMIND_WG_DOH",
	}
	for _, name := range names {
		if strings.TrimSpace(getenv(name)) != "" {
			return true
		}
	}
	return false
}
