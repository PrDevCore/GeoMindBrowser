package policy

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden PAC files")

const testProxy = "127.0.0.1:1080"

func TestPACGolden(t *testing.T) {
	fx := readValidFixture(t, "basic")
	s := storeFor(t, fx.Policy, fx.KnownExits)

	got, err := s.PAC(testProxy)
	if err != nil {
		t.Fatalf("PAC: %v", err)
	}

	path := filepath.Join(fixtureRoot(t), "golden", "basic.pac")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run: go test ./internal/policy -update): %v", err)
	}
	if got != string(want) {
		t.Fatalf("PAC differs from golden %s\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestPACHasNoDirectFallbackForMatchedHosts(t *testing.T) {
	fx := readValidFixture(t, "precedence")
	s := storeFor(t, fx.Policy, fx.KnownExits)

	pac, err := s.PAC(testProxy)
	if err != nil {
		t.Fatalf("PAC: %v", err)
	}
	if err := ValidatePAC(pac, testProxy); err != nil {
		t.Fatalf("ValidatePAC: %v", err)
	}
	if got := strings.Count(pac, `return "DIRECT";`); got != 1 {
		t.Fatalf("DIRECT returns = %d, want exactly 1 (the default)", got)
	}
	if !strings.HasSuffix(pac, pacBody) {
		t.Fatal("DIRECT return is not the final statement")
	}
	if !strings.Contains(pac, blockProxy) {
		t.Fatalf("blocked host does not map to %q", blockProxy)
	}
	if strings.Contains(pac, `"proxy":"DIRECT"`) {
		t.Fatal("a rule maps to DIRECT")
	}
}

func TestValidatePACRejectsTampering(t *testing.T) {
	fx := readValidFixture(t, "basic")
	s := storeFor(t, fx.Policy, fx.KnownExits)
	pac, err := s.PAC(testProxy)
	if err != nil {
		t.Fatalf("PAC: %v", err)
	}

	tests := map[string]string{
		"proxy replaced with DIRECT": strings.Replace(pac, "SOCKS5 "+testProxy, "DIRECT", 1),
		"function removed":           strings.Replace(pac, "function FindProxyForURL(url, host) {", "", 1),
		"extra DIRECT return":        strings.Replace(pac, "return r.proxy;", `return "DIRECT";`, 1),
		"body changed":               strings.Replace(pac, "host = String(host).toLowerCase();", "", 1),
	}
	for name, tampered := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePAC(tampered, testProxy); err == nil {
				t.Fatal("tampered PAC was accepted")
			}
		})
	}
}

func TestPACProxyAddressValidation(t *testing.T) {
	fx := readValidFixture(t, "basic")
	s := storeFor(t, fx.Policy, fx.KnownExits)

	bad := []string{
		"",
		"127.0.0.1",
		"localhost:1080",
		"0.0.0.0:1080",
		"127.0.0.1:0",
		"127.0.0.1:9",
		"127.0.0.1:70000",
		"127.0.0.1:10a",
		"[::1]:1080",
	}
	for _, addr := range bad {
		if _, err := s.PAC(addr); err == nil {
			t.Errorf("PAC(%q): expected error", addr)
		} else if !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("PAC(%q) error %v does not wrap ErrInvalidPolicy", addr, err)
		}
	}

	good := []string{"127.0.0.1:1080", "127.0.0.1:65535"}
	for _, addr := range good {
		if _, err := s.PAC(addr); err != nil {
			t.Errorf("PAC(%q): %v", addr, err)
		}
	}
}
