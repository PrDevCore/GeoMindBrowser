package auth

import (
	"context"
	"errors"
	"testing"
)

func TestDevVerifierAcceptsKnownTokens(t *testing.T) {
	verifier, err := NewDevVerifier(map[string]string{
		"token-alice": "alice",
		"token-bob":   "bob",
	})
	if err != nil {
		t.Fatalf("NewDevVerifier: %v", err)
	}

	for token, want := range map[string]string{"token-alice": "alice", "token-bob": "bob"} {
		principal, err := verifier.Verify(context.Background(), token)
		if err != nil {
			t.Fatalf("Verify(%q): %v", token, err)
		}
		if principal.ID != want {
			t.Fatalf("Verify(%q).ID = %q, want %q", token, principal.ID, want)
		}
	}
}

func TestDevVerifierRejectsUnknownTokens(t *testing.T) {
	verifier, err := NewDevVerifier(map[string]string{"token-alice": "alice"})
	if err != nil {
		t.Fatalf("NewDevVerifier: %v", err)
	}

	for _, token := range []string{"", "token", "token-alice ", "TOKEN-ALICE", "token-alice-extra"} {
		if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Verify(%q) error = %v, want ErrUnauthenticated", token, err)
		}
	}
}

func TestDevVerifierRequiresEntries(t *testing.T) {
	if _, err := NewDevVerifier(nil); err == nil {
		t.Fatal("NewDevVerifier accepted no tokens")
	}
	if _, err := NewDevVerifier(map[string]string{"": "alice"}); err == nil {
		t.Fatal("NewDevVerifier accepted an empty token")
	}
	if _, err := NewDevVerifier(map[string]string{"token": ""}); err == nil {
		t.Fatal("NewDevVerifier accepted an empty user")
	}
}

func TestDevVerifierHonorsContext(t *testing.T) {
	verifier, err := NewDevVerifier(map[string]string{"token-alice": "alice"})
	if err != nil {
		t.Fatalf("NewDevVerifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.Verify(ctx, "token-alice"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify on canceled context error = %v, want context.Canceled", err)
	}
}
