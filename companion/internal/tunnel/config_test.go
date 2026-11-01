package tunnel

import (
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func validEnv(t *testing.T) map[string]string {
	t.Helper()
	local := newKeyPair(t)
	peer := newKeyPair(t)
	return map[string]string{
		"GEOMIND_WG_PRIVATE_KEY":     hex.EncodeToString(local.private[:]),
		"GEOMIND_WG_PEER_PUBLIC_KEY": hex.EncodeToString(peer.public[:]),
		"GEOMIND_WG_ENDPOINT":        "192.0.2.10:51820",
		"GEOMIND_WG_LOCAL_ADDRS":     "10.8.0.2",
		"GEOMIND_WG_ALLOWED_IPS":     "0.0.0.0/0, ::/0",
		"GEOMIND_WG_DOH":             "https://192.0.2.53/dns-query",
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := validEnv(t)
	cfg, err := FromEnv(envFrom(env))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.MTU != DefaultMTU {
		t.Errorf("MTU = %d, want %d", cfg.MTU, DefaultMTU)
	}
	if cfg.Keepalive != DefaultKeepalive {
		t.Errorf("Keepalive = %v, want %v", cfg.Keepalive, DefaultKeepalive)
	}
	if cfg.Endpoint.String() != "192.0.2.10:51820" {
		t.Errorf("Endpoint = %v", cfg.Endpoint)
	}
	if len(cfg.LocalAddresses) != 1 || cfg.LocalAddresses[0].String() != "10.8.0.2" {
		t.Errorf("LocalAddresses = %v", cfg.LocalAddresses)
	}
	if len(cfg.AllowedIPs) != 2 {
		t.Errorf("AllowedIPs = %v", cfg.AllowedIPs)
	}
	if err := cfg.validate(); err != nil {
		t.Errorf("validated config rejected: %v", err)
	}
}

func TestConfigFromEnvRejects(t *testing.T) {
	cases := map[string]func(map[string]string){
		"missing private key": func(env map[string]string) { delete(env, "GEOMIND_WG_PRIVATE_KEY") },
		"short private key":   func(env map[string]string) { env["GEOMIND_WG_PRIVATE_KEY"] = "abcd" },
		"zero private key": func(env map[string]string) {
			env["GEOMIND_WG_PRIVATE_KEY"] = strings.Repeat("0", 64)
		},
		"missing peer key": func(env map[string]string) { delete(env, "GEOMIND_WG_PEER_PUBLIC_KEY") },
		"hostname endpoint": func(env map[string]string) {
			env["GEOMIND_WG_ENDPOINT"] = "vpn.example.com:51820"
		},
		"zero endpoint port": func(env map[string]string) {
			env["GEOMIND_WG_ENDPOINT"] = "192.0.2.10:0"
		},
		"missing local addresses": func(env map[string]string) {
			delete(env, "GEOMIND_WG_LOCAL_ADDRS")
		},
		"hostname local address": func(env map[string]string) {
			env["GEOMIND_WG_LOCAL_ADDRS"] = "client.example.com"
		},
		"missing allowed ips": func(env map[string]string) {
			delete(env, "GEOMIND_WG_ALLOWED_IPS")
		},
		"invalid allowed ip": func(env map[string]string) {
			env["GEOMIND_WG_ALLOWED_IPS"] = "0.0.0.0/0, not-a-cidr"
		},
		"mtu too small": func(env map[string]string) { env["GEOMIND_WG_MTU"] = "1000" },
		"mtu too large": func(env map[string]string) { env["GEOMIND_WG_MTU"] = "10000" },
		"mtu not a number": func(env map[string]string) {
			env["GEOMIND_WG_MTU"] = "big"
		},
		"keepalive out of range": func(env map[string]string) {
			env["GEOMIND_WG_KEEPALIVE"] = "70000"
		},
		"missing doh": func(env map[string]string) { delete(env, "GEOMIND_WG_DOH") },
		"doh not https": func(env map[string]string) {
			env["GEOMIND_WG_DOH"] = "http://192.0.2.53/dns-query"
		},
		"doh hostname": func(env map[string]string) {
			env["GEOMIND_WG_DOH"] = "https://dns.example.com/dns-query"
		},
		"doh with query": func(env map[string]string) {
			env["GEOMIND_WG_DOH"] = "https://192.0.2.53/dns-query?x=1"
		},
		"doh without path": func(env map[string]string) {
			env["GEOMIND_WG_DOH"] = "https://192.0.2.53"
		},
		"doh with userinfo": func(env map[string]string) {
			env["GEOMIND_WG_DOH"] = "https://user@192.0.2.53/dns-query"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv(t)
			mutate(env)
			if _, err := FromEnv(envFrom(env)); err == nil {
				t.Fatal("config was accepted")
			}
		})
	}
}

func TestConfigErrorsDoNotEchoKeyMaterial(t *testing.T) {
	env := validEnv(t)
	secret := env["GEOMIND_WG_PRIVATE_KEY"]
	env["GEOMIND_WG_ENDPOINT"] = "not-an-endpoint"
	_, err := FromEnv(envFrom(env))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error message contains the private key")
	}
}

func TestConfigDefaultsApplied(t *testing.T) {
	cfg := Config{
		PrivateKey:     newKeyPair(t).private,
		PeerPublicKey:  newKeyPair(t).public,
		Endpoint:       mustAddrPort(t, "198.51.100.1:51820"),
		AllowedIPs:     []netip.Prefix{mustPrefix(t, "0.0.0.0/0")},
		LocalAddresses: []netip.Addr{mustAddr(t, "10.9.0.2")},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.MTU != DefaultMTU || cfg.Keepalive != DefaultKeepalive || cfg.StaleAfter != DefaultStaleAfter {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func mustAddrPort(t *testing.T, raw string) netip.AddrPort {
	t.Helper()
	addr, err := netip.ParseAddrPort(raw)
	if err != nil {
		t.Fatalf("parse addr port: %v", err)
	}
	return addr
}

func TestKeepaliveMustBeWholeSeconds(t *testing.T) {
	cfg := Config{
		PrivateKey:     newKeyPair(t).private,
		PeerPublicKey:  newKeyPair(t).public,
		Endpoint:       mustAddrPort(t, "198.51.100.1:51820"),
		AllowedIPs:     []netip.Prefix{mustPrefix(t, "0.0.0.0/0")},
		LocalAddresses: []netip.Addr{mustAddr(t, "10.9.0.2")},
		Keepalive:      1500 * time.Millisecond,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("fractional keepalive accepted")
	}
}
