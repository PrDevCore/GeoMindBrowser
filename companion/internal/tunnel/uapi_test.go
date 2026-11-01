package tunnel

import (
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestUAPIRendering(t *testing.T) {
	key := newKeyPair(t)
	cfg := Config{
		PrivateKey:    key.private,
		PeerPublicKey: key.public,
		Endpoint:      mustAddrPort(t, "198.51.100.7:51820"),
		AllowedIPs:    []netip.Prefix{mustPrefix(t, "0.0.0.0/0")},
		Keepalive:     DefaultKeepalive,
		ListenPort:    51820,
	}
	rendered := cfg.uapi()
	for _, want := range []string{
		"private_key=" + hex.EncodeToString(key.private[:]),
		"listen_port=51820",
		"public_key=" + hex.EncodeToString(key.public[:]),
		"endpoint=198.51.100.7:51820",
		"allowed_ip=0.0.0.0/0",
		"persistent_keepalive_interval=25",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("UAPI output missing %q", want)
		}
	}
}

func TestParseUAPIState(t *testing.T) {
	text := strings.Join([]string{
		"private_key=0000000000000000000000000000000000000000000000000000000000000001",
		"listen_port=51820",
		"public_key=0000000000000000000000000000000000000000000000000000000000000002",
		"protocol_version=1",
		"last_handshake_time_sec=1700000000",
		"last_handshake_time_nsec=250",
		"tx_bytes=10",
		"rx_bytes=20",
		"",
	}, "\n")
	state, err := parseUAPIState(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if state.listenPort != 51820 {
		t.Errorf("listen port = %d", state.listenPort)
	}
	want := time.Unix(1700000000, 250)
	if !state.handshake.Equal(want) {
		t.Errorf("handshake = %v, want %v", state.handshake, want)
	}
}

func TestParseUAPIStateNoHandshake(t *testing.T) {
	text := "private_key=0000000000000000000000000000000000000000000000000000000000000001\n" +
		"last_handshake_time_sec=0\nlast_handshake_time_nsec=0\n"
	state, err := parseUAPIState(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !state.handshake.IsZero() {
		t.Fatalf("handshake = %v, want zero", state.handshake)
	}
}

func TestParseUAPIStateRejectsGarbage(t *testing.T) {
	for _, text := range []string{
		"listen_port=not-a-port",
		"last_handshake_time_sec=not-a-time",
		"last_handshake_time_nsec=not-a-time",
	} {
		if _, err := parseUAPIState(text); err == nil {
			t.Errorf("parseUAPIState(%q) accepted garbage", text)
		}
	}
}
