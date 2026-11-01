// Package policy loads per-site routing rules, matches hosts against them, and
// generates the proxy auto-config script the extension installs.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/idna"
)

// Action is what happens to a host: go direct, route through an exit, or block.
type Action string

// Actions a policy can assign to a host.
const (
	ActionDirect Action = "direct"
	ActionRoute  Action = "route"
	ActionBlock  Action = "block"
)

var (
	// ErrInvalidPolicy reports a policy document that failed validation.
	ErrInvalidPolicy = errors.New("invalid policy")
	// ErrInvalidHost reports a host or host pattern that cannot be normalized.
	ErrInvalidHost = errors.New("invalid host")
	// ErrNotLoaded reports use of a store before a policy was loaded.
	ErrNotLoaded = errors.New("policy not loaded")
)

// Rule is one per-site routing rule.
type Rule struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Action   Action `json:"action"`
	Exit     string `json:"exit,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// Doc is a policy document as stored on disk and sent over IPC.
type Doc struct {
	Version       int    `json:"version"`
	DefaultAction Action `json:"defaultAction"`
	Rules         []Rule `json:"rules"`
}

// Decision is the outcome of matching a host.
type Decision struct {
	Action Action
	Exit   string
	RuleID string
}

type patternKind int

const (
	patternExact patternKind = iota
	patternWildcard
)

type compiledRule struct {
	id       string
	host     string
	kind     patternKind
	labels   int
	action   Action
	exit     string
	priority int
	index    int
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// maxPriority bounds rule priority, matching the policy schema and the
// extension's validator.
const maxPriority = 1_000_000

// Store holds a validated policy. It is safe for concurrent use: readers hold a
// read lock while Load swaps the policy under the write lock. knownExits is
// read-only after New.
type Store struct {
	mu         sync.RWMutex
	doc        Doc
	rules      []compiledRule
	knownExits map[string]struct{}
	loaded     bool
}

// New returns an empty store that treats only the given exits as known.
func New(knownExits []string) *Store {
	exits := make(map[string]struct{}, len(knownExits))
	for _, e := range knownExits {
		exits[e] = struct{}{}
	}
	return &Store{knownExits: exits}
}

// Load reads, validates, and compiles a policy file. On error the previous
// policy stays in effect.
func (s *Store) Load(path string) (Doc, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: local policy file path from the companion config.
	if err != nil {
		return Doc{}, fmt.Errorf("%w: read: %w", ErrInvalidPolicy, err)
	}
	doc, err := decodeDoc(data)
	if err != nil {
		return Doc{}, err
	}
	rules, err := compile(doc, s.knownExits)
	if err != nil {
		return Doc{}, err
	}
	s.mu.Lock()
	s.doc = doc
	s.rules = rules
	s.loaded = true
	s.mu.Unlock()
	return doc, nil
}

// Snapshot returns the current policy and whether one is loaded. The returned
// Doc shares its rule slice with the store, which never mutates a published
// document in place.
func (s *Store) Snapshot() (Doc, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.doc, s.loaded
}

// SnapshotWithPAC returns the current policy and its PAC under one read lock,
// so a concurrent reload cannot pair an old document with a new PAC.
func (s *Store) SnapshotWithPAC(proxyAddr string) (Doc, string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		return Doc{}, "", false, nil
	}
	pac, err := s.pacLocked(proxyAddr)
	if err != nil {
		return Doc{}, "", false, err
	}
	return s.doc, pac, true, nil
}

func decodeDoc(data []byte) (Doc, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc Doc
	if err := dec.Decode(&doc); err != nil {
		return Doc{}, fmt.Errorf("%w: decode: %w", ErrInvalidPolicy, err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return Doc{}, fmt.Errorf("%w: trailing data", ErrInvalidPolicy)
	}
	if doc.Rules == nil {
		return Doc{}, fmt.Errorf("%w: rules must be an array", ErrInvalidPolicy)
	}
	return doc, nil
}

func compile(doc Doc, knownExits map[string]struct{}) ([]compiledRule, error) {
	if doc.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidPolicy, doc.Version)
	}
	if doc.DefaultAction != ActionDirect {
		return nil, fmt.Errorf("%w: defaultAction must be %q", ErrInvalidPolicy, ActionDirect)
	}

	seen := make(map[string]struct{}, len(doc.Rules))
	rules := make([]compiledRule, 0, len(doc.Rules))
	for i, r := range doc.Rules {
		if !idPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("%w: rule %d has an invalid id", ErrInvalidPolicy, i)
		}
		if _, dup := seen[r.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate rule id %q", ErrInvalidPolicy, r.ID)
		}
		seen[r.ID] = struct{}{}

		if r.Priority < -maxPriority || r.Priority > maxPriority {
			return nil, fmt.Errorf("%w: rule %q has priority %d out of range", ErrInvalidPolicy, r.ID, r.Priority)
		}

		kind, host, labels, err := compilePattern(r.Host)
		if err != nil {
			return nil, fmt.Errorf("%w: rule %q: %w", ErrInvalidPolicy, r.ID, err)
		}

		action := r.Action
		exit := r.Exit
		switch r.Action {
		case ActionRoute:
			if r.Exit == "" {
				return nil, fmt.Errorf("%w: rule %q routes without an exit", ErrInvalidPolicy, r.ID)
			}
			if _, ok := knownExits[r.Exit]; !ok {
				// Unknown exits fail closed, and never fall back to direct.
				action = ActionBlock
				exit = ""
			}
		case ActionBlock:
			if r.Exit != "" {
				return nil, fmt.Errorf("%w: rule %q blocks but names an exit", ErrInvalidPolicy, r.ID)
			}
		default:
			return nil, fmt.Errorf("%w: rule %q has action %q", ErrInvalidPolicy, r.ID, r.Action)
		}

		rules = append(rules, compiledRule{
			id:       r.ID,
			host:     host,
			kind:     kind,
			labels:   labels,
			action:   action,
			exit:     exit,
			priority: r.Priority,
			index:    i,
		})
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return moreSpecific(rules[i], rules[j])
	})
	return rules, nil
}

func moreSpecific(a, b compiledRule) bool {
	if a.kind != b.kind {
		return a.kind == patternExact
	}
	if a.kind == patternWildcard {
		if a.labels != b.labels {
			return a.labels > b.labels
		}
		if len(a.host) != len(b.host) {
			return len(a.host) > len(b.host)
		}
	}
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	return a.index < b.index
}

func compilePattern(raw string) (patternKind, string, int, error) {
	if raw == "" {
		return 0, "", 0, fmt.Errorf("%w: empty pattern", ErrInvalidHost)
	}
	if strings.HasPrefix(raw, "*.") {
		suffix, err := normalizeHost(raw[2:])
		if err != nil {
			return 0, "", 0, err
		}
		return patternWildcard, suffix, len(strings.Split(suffix, ".")), nil
	}
	if strings.Contains(raw, "*") {
		return 0, "", 0, fmt.Errorf("%w: wildcard must be the leading label", ErrInvalidHost)
	}
	host, err := normalizeHost(raw)
	if err != nil {
		return 0, "", 0, err
	}
	return patternExact, host, 0, nil
}

// normalizeHost lowercases, strips one trailing dot, converts IDNs to punycode,
// and validates labels. IP literals are rejected in Phase 1.
func normalizeHost(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidHost)
	}
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: surrounding whitespace", ErrInvalidHost)
	}
	h := strings.ToLower(raw)
	h = strings.TrimSuffix(h, ".")
	if h == "" || strings.Contains(h, "*") {
		return "", fmt.Errorf("%w: %q", ErrInvalidHost, raw)
	}
	if strings.ContainsAny(h, "/:@[]%\\ \t\r\n") {
		return "", fmt.Errorf("%w: %q", ErrInvalidHost, raw)
	}
	if len(h) > 253 {
		return "", fmt.Errorf("%w: too long", ErrInvalidHost)
	}

	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidHost, raw, err)
	}
	if len(ascii) > 253 {
		return "", fmt.Errorf("%w: too long", ErrInvalidHost)
	}
	if net.ParseIP(ascii) != nil {
		return "", fmt.Errorf("%w: IP literals are not supported", ErrInvalidHost)
	}
	labels := strings.Split(ascii, ".")
	for _, label := range labels {
		if !validLabel(label) {
			return "", fmt.Errorf("%w: bad label %q", ErrInvalidHost, label)
		}
	}
	// WHATWG URL parsing treats a host as IPv4 when its last label is numeric.
	// Forms such as 010.0.0.1, 0x7f000001, 2130706433, and example.123 therefore
	// resolve to an address, not a name; reject them so Go and the extension
	// agree on which patterns are hosts.
	if isNumericLabel(labels[len(labels)-1]) {
		return "", fmt.Errorf("%w: IP literals are not supported", ErrInvalidHost)
	}
	return ascii, nil
}

// isNumericLabel reports whether a label is a decimal or 0x-prefixed hex
// number, the two forms a WHATWG IPv4 parser accepts.
func isNumericLabel(label string) bool {
	if rest, ok := strings.CutPrefix(label, "0x"); ok {
		if rest == "" {
			return false
		}
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
		return true
	}
	if label == "" {
		return false
	}
	for i := 0; i < len(label); i++ {
		if label[i] < '0' || label[i] > '9' {
			return false
		}
	}
	return true
}

func validLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

// Match returns the decision for a host. It never returns a route decision for
// an unknown exit; those compile to block.
func (s *Store) Match(rawHost string) (Decision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		return Decision{}, ErrNotLoaded
	}
	host, err := normalizeHost(rawHost)
	if err != nil {
		return Decision{}, err
	}
	for _, r := range s.rules {
		switch r.kind {
		case patternExact:
			if host == r.host {
				return decisionFor(r), nil
			}
		case patternWildcard:
			suffix := "." + r.host
			if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
				return decisionFor(r), nil
			}
		}
	}
	return Decision{Action: ActionDirect}, nil
}

func decisionFor(r compiledRule) Decision {
	if r.action == ActionBlock {
		return Decision{Action: ActionBlock, RuleID: r.id}
	}
	return Decision{Action: r.action, Exit: r.exit, RuleID: r.id}
}
