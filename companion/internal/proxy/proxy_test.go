package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTunnel struct {
	mu       sync.Mutex
	healthFn func() bool
	dialFn   func(context.Context, string) (net.Conn, error)
	recorded []string
}

func newFakeTunnel() *fakeTunnel {
	return &fakeTunnel{healthFn: func() bool { return true }}
}

func (f *fakeTunnel) Healthy() bool {
	f.mu.Lock()
	fn := f.healthFn
	f.mu.Unlock()
	if fn == nil {
		return true
	}
	return fn()
}

func (f *fakeTunnel) DialContext(ctx context.Context, _ string, addr string) (net.Conn, error) {
	f.mu.Lock()
	f.recorded = append(f.recorded, addr)
	fn := f.dialFn
	f.mu.Unlock()
	if fn == nil {
		return nil, errors.New("no dialer configured")
	}
	return fn(ctx, addr)
}

func (f *fakeTunnel) setHealthy(healthy bool) {
	f.mu.Lock()
	f.healthFn = func() bool { return healthy }
	f.mu.Unlock()
}

func (f *fakeTunnel) dials() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.recorded...)
}

func startServer(t *testing.T, tunnel Tunnel) *Server {
	t.Helper()
	server, err := Listen(context.Background(), tunnel)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		_ = server.Serve(context.Background())
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})
	return server
}

func dialProxy(t *testing.T, server *Server) net.Conn {
	t.Helper()
	var dialer net.Dialer
	conn, err := dialer.DialContext(context.Background(), "tcp", server.Addr())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

func sendGreeting(t *testing.T, conn net.Conn, methods ...byte) {
	t.Helper()
	request := append([]byte{socksVersion, byte(len(methods))}, methods...)
	mustWrite(t, conn, request)
}

func readExact(t *testing.T, conn net.Conn, size int) []byte {
	t.Helper()
	buf := make([]byte, size)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	return buf
}

func mustWrite(t *testing.T, conn net.Conn, data []byte) {
	t.Helper()
	if err := writeAll(conn, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func greetingOK(t *testing.T, conn net.Conn) {
	t.Helper()
	sendGreeting(t, conn, authNone)
	reply := readExact(t, conn, 2)
	if reply[0] != socksVersion || reply[1] != authNone {
		t.Fatalf("method reply = %v", reply)
	}
}

func domainRequest(host string, port uint16) []byte {
	request := []byte{socksVersion, cmdConnect, 0, atypDomain, byte(len(host))}
	request = append(request, []byte(host)...)
	return append(request, byte(port>>8), byte(port))
}

func ipv4Request(ip net.IP, port uint16) []byte {
	request := []byte{socksVersion, cmdConnect, 0, atypIPv4}
	request = append(request, ip.To4()...)
	return append(request, byte(port>>8), byte(port))
}

func ipv6Request(ip net.IP, port uint16) []byte {
	request := []byte{socksVersion, cmdConnect, 0, atypIPv6}
	request = append(request, ip.To16()...)
	return append(request, byte(port>>8), byte(port))
}

func readReplyCode(t *testing.T, conn net.Conn) byte {
	t.Helper()
	return readExact(t, conn, 10)[1]
}

func TestRefusesWhenUnhealthyAtAccept(t *testing.T) {
	fake := newFakeTunnel()
	fake.setHealthy(false)
	server := startServer(t, fake)

	conn := dialProxy(t, server)
	_, _ = conn.Write([]byte{socksVersion, 1, authNone})
	_, err := io.ReadFull(conn, make([]byte, 2))
	if err == nil {
		t.Fatal("expected the proxy to close an unhealthy connection")
	}
	if got := fake.dials(); len(got) != 0 {
		t.Fatalf("dialed %v with an unhealthy tunnel", got)
	}
}

func TestRefusesAfterHandshakeWhenTunnelDrops(t *testing.T) {
	fake := newFakeTunnel()
	var calls int32
	fake.healthFn = func() bool {
		return atomic.AddInt32(&calls, 1) == 1
	}
	server := startServer(t, fake)

	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("example.com", 443))
	if code := readReplyCode(t, conn); code != replyGeneralFailure {
		t.Fatalf("reply code = %d, want %d", code, replyGeneralFailure)
	}
	if got := fake.dials(); len(got) != 0 {
		t.Fatalf("dialed %v after the tunnel dropped", got)
	}
}

func TestForwardsDomainUnresolvedAndRelays(t *testing.T) {
	fake := newFakeTunnel()
	upstreamCh := make(chan net.Conn, 1)
	fake.dialFn = func(context.Context, string) (net.Conn, error) {
		client, upstream := net.Pipe()
		upstreamCh <- upstream
		return client, nil
	}
	server := startServer(t, fake)

	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("example.com", 443))
	if code := readReplyCode(t, conn); code != replySuccess {
		t.Fatalf("reply code = %d", code)
	}
	if got := fake.dials(); len(got) != 1 || got[0] != "example.com:443" {
		t.Fatalf("dialed %v, want the unresolved hostname", got)
	}

	upstream := <-upstreamCh
	t.Cleanup(func() {
		_ = upstream.Close()
	})
	go func() {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(upstream, buf); err != nil {
			return
		}
		_, _ = upstream.Write([]byte("pong"))
	}()
	mustWrite(t, conn, []byte("ping"))
	if got := string(readExact(t, conn, 4)); got != "pong" {
		t.Fatalf("echo = %q", got)
	}
}

func TestTargetAddresses(t *testing.T) {
	cases := map[string]struct {
		request []byte
		want    string
	}{
		"ipv4": {request: ipv4Request(net.IPv4(192, 0, 2, 10), 80), want: "192.0.2.10:80"},
		"ipv6": {request: ipv6Request(net.ParseIP("2001:db8::1"), 8443), want: "[2001:db8::1]:8443"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newFakeTunnel()
			fake.dialFn = func(context.Context, string) (net.Conn, error) {
				return nil, errors.New("stop after recording")
			}
			server := startServer(t, fake)
			conn := dialProxy(t, server)
			greetingOK(t, conn)
			mustWrite(t, conn, tc.request)
			if code := readReplyCode(t, conn); code != replyGeneralFailure {
				t.Fatalf("reply code = %d", code)
			}
			if got := fake.dials(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("dialed %v, want %q", got, tc.want)
			}
		})
	}
}

func TestRejectsUnsupportedAuth(t *testing.T) {
	fake := newFakeTunnel()
	server := startServer(t, fake)
	conn := dialProxy(t, server)
	sendGreeting(t, conn, 0x02)
	reply := readExact(t, conn, 2)
	if reply[0] != socksVersion || reply[1] != authReject {
		t.Fatalf("method reply = %v", reply)
	}
	if len(fake.dials()) != 0 {
		t.Fatal("must not dial without a supported auth method")
	}
}

func TestRejectsNonConnectCommand(t *testing.T) {
	fake := newFakeTunnel()
	server := startServer(t, fake)
	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, []byte{socksVersion, 0x02, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
	if code := readReplyCode(t, conn); code != replyCommandNotSupported {
		t.Fatalf("reply code = %d", code)
	}
	if len(fake.dials()) != 0 {
		t.Fatal("must not dial for a non-CONNECT command")
	}
}

func TestRejectsZeroPort(t *testing.T) {
	fake := newFakeTunnel()
	server := startServer(t, fake)
	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("example.com", 0))
	if code := readReplyCode(t, conn); code != replyGeneralFailure {
		t.Fatalf("reply code = %d", code)
	}
	if len(fake.dials()) != 0 {
		t.Fatal("must not dial port zero")
	}
}

func TestRejectsAmbiguousDomain(t *testing.T) {
	fake := newFakeTunnel()
	server := startServer(t, fake)
	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("bad host", 443))
	if code := readReplyCode(t, conn); code != replyAddressNotSupported {
		t.Fatalf("reply code = %d", code)
	}
	if len(fake.dials()) != 0 {
		t.Fatal("must not dial an ambiguous host")
	}
}

func TestKillMidTransferStopsTraffic(t *testing.T) {
	fake := newFakeTunnel()
	upstreamCh := make(chan net.Conn, 1)
	fake.dialFn = func(context.Context, string) (net.Conn, error) {
		client, upstream := net.Pipe()
		upstreamCh <- upstream
		return client, nil
	}
	server := startServer(t, fake)

	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("example.com", 443))
	if code := readReplyCode(t, conn); code != replySuccess {
		t.Fatalf("reply code = %d", code)
	}
	upstream := <-upstreamCh
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	mustWrite(t, conn, []byte("payload"))
	if got := string(readExact(t, upstream, 7)); got != "payload" {
		t.Fatalf("upstream got %q", got)
	}

	fake.setHealthy(false)
	_ = upstream.Close()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("client must see the cut transfer fail")
	}

	dialsBefore := len(fake.dials())
	retry := dialProxy(t, server)
	_, _ = retry.Write([]byte{socksVersion, 1, authNone})
	if _, err := io.ReadFull(retry, make([]byte, 2)); err == nil {
		t.Fatal("an unhealthy tunnel must refuse new connections")
	}
	if got := len(fake.dials()); got != dialsBefore {
		t.Fatalf("new dials after kill: %d -> %d", dialsBefore, got)
	}
}

func TestCloseFailsInFlightConnections(t *testing.T) {
	fake := newFakeTunnel()
	upstreamCh := make(chan net.Conn, 1)
	fake.dialFn = func(context.Context, string) (net.Conn, error) {
		client, upstream := net.Pipe()
		upstreamCh <- upstream
		return client, nil
	}
	server := startServer(t, fake)
	conn := dialProxy(t, server)
	greetingOK(t, conn)
	mustWrite(t, conn, domainRequest("example.com", 443))
	if code := readReplyCode(t, conn); code != replySuccess {
		t.Fatalf("reply code = %d", code)
	}
	upstream := <-upstreamCh
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	if err := server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("closing the server must close in-flight connections")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func listenOn(t *testing.T, addr string) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp4", addr)
	if err != nil {
		t.Skipf("cannot bind %s: %v", addr, err)
	}
	return listener
}

func TestNewServerRejectsNonLoopbackListener(t *testing.T) {
	listener := listenOn(t, "0.0.0.0:0")
	if _, err := NewServer(newFakeTunnel(), listener); !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("error = %v, want ErrNotLoopback", err)
	}
	if _, err := listener.Accept(); err == nil {
		t.Fatal("rejected listener must be closed")
	}
}

func TestNewServerRejectsNilTunnelAndClosesListener(t *testing.T) {
	listener := listenOn(t, "127.0.0.1:0")
	if _, err := NewServer(nil, listener); !errors.Is(err, ErrNilTunnel) {
		t.Fatalf("error = %v, want ErrNilTunnel", err)
	}
	if _, err := listener.Accept(); err == nil {
		t.Fatal("rejected listener must be closed")
	}
}

func TestListenBindsLoopbackEphemeralPort(t *testing.T) {
	server := startServer(t, newFakeTunnel())
	host, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("host = %q", host)
	}
	if port == "0" {
		t.Fatal("expected an ephemeral port")
	}
}
