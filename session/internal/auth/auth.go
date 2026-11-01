// Package auth verifies bearer tokens presented to the session API. The
// development verifier is for local runs only; hosted deployments use an OIDC
// verifier.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
)

// ErrUnauthenticated reports a missing, malformed, or unknown token.
var ErrUnauthenticated = errors.New("unauthenticated")

// Principal is the authenticated caller.
type Principal struct {
	ID string
}

// Verifier turns a bearer token into a Principal.
type Verifier interface {
	Verify(ctx context.Context, token string) (Principal, error)
}

type devEntry struct {
	hash [sha256.Size]byte
	user string
}

// DevVerifier authenticates a fixed set of tokens without any external
// dependency. It must not be used in a hosted deployment.
type DevVerifier struct {
	entries []devEntry
}

// NewDevVerifier builds a verifier from a token -> user mapping.
func NewDevVerifier(tokens map[string]string) (*DevVerifier, error) {
	if len(tokens) == 0 {
		return nil, errors.New("auth: at least one development token is required")
	}
	entries := make([]devEntry, 0, len(tokens))
	for token, user := range tokens {
		if token == "" || user == "" {
			return nil, errors.New("auth: development tokens and users must be non-empty")
		}
		entries = append(entries, devEntry{hash: sha256.Sum256([]byte(token)), user: user})
	}
	return &DevVerifier{entries: entries}, nil
}

// Verify compares the token hash against every configured token in constant
// time and returns the matching principal.
func (v *DevVerifier) Verify(ctx context.Context, token string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	sum := sha256.Sum256([]byte(token))
	matched := ""
	for _, entry := range v.entries {
		if subtle.ConstantTimeCompare(sum[:], entry.hash[:]) == 1 {
			matched = entry.user
		}
	}
	if matched == "" {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{ID: matched}, nil
}
