// Package proxy implements the loopback SOCKS5 proxy that carries routed
// traffic. It has no direct dial path: every connection goes through Tunnel.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	defaultHandshakeTimeout = 10 * time.Second
	defaultDialTimeout      = 15 * time.Second
)

var (
	// ErrNilTunnel reports a missing tunnel dependency.
	ErrNilTunnel = errors.New("proxy: nil tunnel")
	// ErrNotLoopback reports a listener that is not bound to loopback.
	ErrNotLoopback = errors.New("proxy: listener must bind loopback")
)

// Tunnel is the only outbound path available to the proxy. Implementations
// dial through the WireGuard netstack; the proxy never resolves or dials
// locally.
type Tunnel interface {
	Healthy() bool
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Server is a loopback SOCKS5 CONNECT proxy with a fail-closed health gate.
type Server struct {
	tunnel           Tunnel
	listener         net.Listener
	handshakeTimeout time.Duration
	dialTimeout      time.Duration

	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	closed    bool
	closeOnce sync.Once
}

// Listen starts a proxy on an ephemeral loopback port.
func Listen(ctx context.Context, tunnel Tunnel) (*Server, error) {
	return ListenAt(ctx, tunnel, "127.0.0.1:0")
}

// ListenAt starts a proxy on addr. The address must resolve to loopback;
// anything else is rejected after binding and the listener is closed.
func ListenAt(ctx context.Context, tunnel Tunnel, addr string) (*Server, error) {
	if tunnel == nil {
		return nil, ErrNilTunnel
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("proxy: listen: %w", err)
	}
	return NewServer(tunnel, listener)
}

// NewServer wraps an existing listener. The listener must already be bound to
// loopback; anything else is rejected so the proxy cannot be exposed.
func NewServer(tunnel Tunnel, listener net.Listener) (*Server, error) {
	if listener == nil {
		return nil, errors.New("proxy: nil listener")
	}
	if tunnel == nil {
		_ = listener.Close()
		return nil, ErrNilTunnel
	}
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("proxy: listener address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		_ = listener.Close()
		return nil, ErrNotLoopback
	}
	return &Server{
		tunnel:           tunnel,
		listener:         listener,
		handshakeTimeout: defaultHandshakeTimeout,
		dialTimeout:      defaultDialTimeout,
		conns:            make(map[net.Conn]struct{}),
	}, nil
}

// Addr is the loopback address the proxy is listening on.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Serve accepts connections until the listener is closed or ctx is canceled.
func (s *Server) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		_ = s.Close()
	})
	defer stop()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.isClosed() || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("proxy: accept: %w", err)
		}
		// Fail closed before reading a greeting: an unhealthy tunnel must not
		// receive any routed bytes, even a SOCKS handshake.
		if !s.tunnel.Healthy() {
			_ = conn.Close()
			continue
		}
		if !s.track(conn) {
			continue
		}
		go func() {
			defer s.untrack(conn)
			s.handle(ctx, conn)
		}()
	}
}

// Close stops accepting, closes in-flight connections, and is idempotent.
func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		active := make([]net.Conn, 0, len(s.conns))
		for conn := range s.conns {
			active = append(active, conn)
		}
		s.mu.Unlock()

		closeErr = s.listener.Close()
		for _, conn := range active {
			_ = conn.Close()
		}
	})
	return closeErr
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
	_ = conn.Close()
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	if err := conn.SetDeadline(time.Now().Add(s.handshakeTimeout)); err != nil {
		return
	}
	target, err := readConnect(conn)
	if err != nil {
		return
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return
	}

	// Re-check after the handshake: the tunnel may have dropped while the
	// client was talking to us.
	if !s.tunnel.Healthy() {
		_ = writeReply(conn, replyGeneralFailure)
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx, s.dialTimeout)
	defer cancel()
	upstream, err := s.tunnel.DialContext(dialCtx, "tcp", target)
	if err != nil {
		_ = writeReply(conn, replyGeneralFailure)
		return
	}
	defer func() {
		_ = upstream.Close()
	}()

	if err := writeReply(conn, replySuccess); err != nil {
		return
	}
	relay(conn, upstream)
}

func relay(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
	_ = client.Close()
	_ = upstream.Close()
	<-done
}
