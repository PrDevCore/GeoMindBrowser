package policy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureCase struct {
	Host   string `json:"host"`
	Action Action `json:"action"`
	Exit   string `json:"exit"`
	Rule   string `json:"rule"`
}

type validFixture struct {
	Name       string          `json:"name"`
	KnownExits []string        `json:"knownExits"`
	Policy     json.RawMessage `json:"policy"`
	Cases      []fixtureCase   `json:"cases"`
}

type invalidFixture struct {
	Name       string   `json:"name"`
	KnownExits []string `json:"knownExits"`
	Cases      []struct {
		Why    string          `json:"why"`
		Policy json.RawMessage `json:"policy"`
	} `json:"cases"`
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "shared", "fixtures", "policy")
}

func readValidFixture(t *testing.T, name string) validFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureRoot(t), "valid", name+".json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var fx validFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return fx
}

func storeFor(t *testing.T, raw json.RawMessage, exits []string) *Store {
	t.Helper()
	doc, err := decodeDoc(raw)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	s := New(exits)
	rules, err := compile(doc, s.knownExits)
	if err != nil {
		t.Fatalf("compile policy: %v", err)
	}
	s.doc = doc
	s.rules = rules
	s.loaded = true
	return s
}

func TestValidFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureRoot(t), "valid", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no valid fixtures found: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			fx := readValidFixture(t, name)
			s := storeFor(t, fx.Policy, fx.KnownExits)
			for _, c := range fx.Cases {
				got, err := s.Match(c.Host)
				if err != nil {
					t.Errorf("host %q: unexpected error: %v", c.Host, err)
					continue
				}
				want := Decision{Action: c.Action, Exit: c.Exit, RuleID: c.Rule}
				if got != want {
					t.Errorf("host %q: got %+v, want %+v", c.Host, got, want)
				}
			}
		})
	}
}

func TestInvalidFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixtureRoot(t), "invalid", "cases.json"))
	if err != nil {
		t.Fatalf("read invalid fixtures: %v", err)
	}
	var fx invalidFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("decode invalid fixtures: %v", err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("invalid fixture file is empty")
	}
	for _, c := range fx.Cases {
		t.Run(c.Why, func(t *testing.T) {
			doc, err := decodeDoc(c.Policy)
			if err == nil {
				_, err = compile(doc, New(fx.KnownExits).knownExits)
			}
			if err == nil {
				t.Fatalf("policy was accepted but should have been rejected")
			}
			if !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("error %v does not wrap ErrInvalidPolicy", err)
			}
		})
	}
}

func TestMatchRequiresLoadedPolicy(t *testing.T) {
	s := New(nil)
	if _, err := s.Match("example.com"); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("Match error = %v, want ErrNotLoaded", err)
	}
	if _, err := s.PAC("127.0.0.1:1080"); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("PAC error = %v, want ErrNotLoaded", err)
	}
}
