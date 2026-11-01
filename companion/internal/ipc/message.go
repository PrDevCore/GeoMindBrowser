// Package ipc implements the typed, size-bounded native messaging protocol
// between the extension and the companion.
package ipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	// ProtocolVersion is the native messaging protocol version.
	ProtocolVersion = 1
	// MaxMessageBytes caps a single frame in either direction.
	MaxMessageBytes = 1 << 20
	maxIDBytes      = 128
	maxTypeBytes    = 64
)

// ErrMalformedMessage reports a frame or envelope that cannot be accepted.
var ErrMalformedMessage = errors.New("ipc: malformed message")

// ErrMessageTooLarge reports a frame that exceeds MaxMessageBytes.
var ErrMessageTooLarge = errors.New("ipc: message too large")

// Envelope is the outer native messaging frame.
type Envelope struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is what a handler returns. Serve copies ID from the request.
type Response struct {
	Type    string
	Payload any
}

// ErrorPayload is the body of an "error" response.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	typePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9]+)*$`)
	codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Validate checks the envelope shape. Payload, when present, must be a JSON
// object; individual handlers decode it strictly.
func (e Envelope) Validate() error {
	if !idPattern.MatchString(e.ID) || len(e.ID) > maxIDBytes {
		return fmt.Errorf("%w: invalid id", ErrMalformedMessage)
	}
	if len(e.Type) > maxTypeBytes || !typePattern.MatchString(e.Type) {
		return fmt.Errorf("%w: invalid type", ErrMalformedMessage)
	}
	if len(e.Payload) > 0 {
		trimmed := bytes.TrimSpace(e.Payload)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return fmt.Errorf("%w: payload must be an object", ErrMalformedMessage)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return fmt.Errorf("%w: payload: %w", ErrMalformedMessage, err)
		}
	}
	return nil
}

// DecodePayload decodes a request payload into out, rejecting unknown fields
// and trailing data. A missing payload is treated as an empty object.
func DecodePayload(payload json.RawMessage, out any) error {
	raw := payload
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: payload: %w", ErrMalformedMessage, err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("%w: payload has trailing data", ErrMalformedMessage)
	}
	return nil
}
