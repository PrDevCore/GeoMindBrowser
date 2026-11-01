package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	valid := map[string]string{
		"Example.COM.":    "example.com",
		"example.com":     "example.com",
		"münchen.example": "xn--mnchen-3ya.example",
		"BÜCHER.EXAMPLE":  "xn--bcher-kva.example",
		"a.b.c.test":      "a.b.c.test",
	}
	for in, want := range valid {
		got, err := normalizeHost(in)
		if err != nil {
			t.Errorf("normalizeHost(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{
		"",
		" example.com",
		"example.com ",
		"*",
		"a.*.com",
		"127.0.0.1",
		"010.0.0.1",
		"2130706433",
		"0x7f000001",
		"0x7f.0.0.1",
		"999.999.999.999",
		"example.123",
		"example.com:443",
		"http://example.com",
		"a..b",
		"-a.com",
		"a-.com",
		"a_b.com",
		"example.com/path",
		"user@example.com",
	}
	for _, in := range invalid {
		if got, err := normalizeHost(in); err == nil {
			t.Errorf("normalizeHost(%q) = %q, want error", in, got)
		} else if !errors.Is(err, ErrInvalidHost) {
			t.Errorf("normalizeHost(%q) error %v does not wrap ErrInvalidHost", in, err)
		}
	}
}

func TestCompileMarksUnknownExitAsBlock(t *testing.T) {
	doc := Doc{
		Version:       1,
		DefaultAction: ActionDirect,
		Rules: []Rule{
			{ID: "gone", Host: "*.gone.test", Action: ActionRoute, Exit: "missing"},
		},
	}
	s := New([]string{"de-test"})
	rules, err := compile(doc, s.knownExits)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	s.rules, s.doc, s.loaded = rules, doc, true

	got, err := s.Match("a.gone.test")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	want := Decision{Action: ActionBlock, RuleID: "gone"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadKeepsPreviousPolicyOnError(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(good, []byte(`{
		"version": 1,
		"defaultAction": "direct",
		"rules": [{"id": "de", "host": "*.example.com", "action": "route", "exit": "de-test"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{"version": 2, "defaultAction": "direct", "rules": []}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New([]string{"de-test"})
	if _, err := s.Load(good); err != nil {
		t.Fatalf("load good: %v", err)
	}
	if _, err := s.Load(bad); err == nil {
		t.Fatal("load bad: expected error")
	}
	got, err := s.Match("a.example.com")
	if err != nil {
		t.Fatalf("match after failed load: %v", err)
	}
	if got.Action != ActionRoute || got.Exit != "de-test" {
		t.Fatalf("previous policy was not kept: %+v", got)
	}
}

func TestLoadRejectsTrailingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"defaultAction":"direct","rules":[]} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil).Load(path); err == nil {
		t.Fatal("expected error for trailing JSON data")
	}
}

func TestLoadRejectsMissingRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"defaultAction":"direct"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil).Load(path); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Load error = %v, want ErrInvalidPolicy", err)
	}
}

func TestStoreConcurrentMatchAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"defaultAction": "direct",
		"rules": [{"id": "de", "host": "*.example.com", "action": "route", "exit": "de-test"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New([]string{"de-test"})
	if _, err := s.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				got, err := s.Match("a.example.com")
				if err != nil {
					t.Errorf("match: %v", err)
					return
				}
				if got.Action != ActionRoute || got.Exit != "de-test" {
					t.Errorf("match = %+v", got)
					return
				}
				if _, err := s.PAC("127.0.0.1:1080"); err != nil {
					t.Errorf("pac: %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			if _, err := s.Load(path); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

// SnapshotWithPAC must never pair a document with a PAC from another reload.
func TestSnapshotWithPACIsAtomic(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.json")
	large := filepath.Join(dir, "large.json")
	if err := os.WriteFile(small, []byte(`{
		"version": 1,
		"defaultAction": "direct",
		"rules": [{"id": "one", "host": "one.test", "action": "route", "exit": "de-test"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, []byte(`{
		"version": 1,
		"defaultAction": "direct",
		"rules": [
			{"id": "one", "host": "one.test", "action": "route", "exit": "de-test"},
			{"id": "two", "host": "two.test", "action": "route", "exit": "de-test"},
			{"id": "three", "host": "three.test", "action": "route", "exit": "de-test"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New([]string{"de-test"})
	if _, err := s.Load(small); err != nil {
		t.Fatalf("load: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				doc, pac, ok, err := s.SnapshotWithPAC("127.0.0.1:1080")
				if err != nil || !ok {
					t.Errorf("snapshot: ok=%v err=%v", ok, err)
					return
				}
				if got := strings.Count(pac, `"proxy"`); got != len(doc.Rules) {
					t.Errorf("document has %d rules but PAC has %d", len(doc.Rules), got)
					return
				}
			}
		}()
	}
	for j := 0; j < 200; j++ {
		path := small
		if j%2 == 0 {
			path = large
		}
		if _, err := s.Load(path); err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}
