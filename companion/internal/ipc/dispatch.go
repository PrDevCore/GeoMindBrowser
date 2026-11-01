package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// EventID is the envelope id reserved for unsolicited events. Requests from the
// extension never use it, so responses and events cannot be confused.
const EventID = "event"

// Handler processes one request payload and returns one response.
type Handler func(ctx context.Context, payload json.RawMessage) (Response, error)

// Sender writes envelopes to a native messaging stream. Responses and
// unsolicited events can arrive from different goroutines, so each frame is
// written under a mutex and a failed stream stays failed.
type Sender struct {
	mu     sync.Mutex
	w      io.Writer
	failed error
}

// NewSender wraps w.
func NewSender(w io.Writer) *Sender {
	return &Sender{w: w}
}

// Send writes a response envelope with the given id.
func (s *Sender) Send(id, typ string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	return s.write(Envelope{ID: id, Type: typ, Payload: data})
}

// Notify writes an unsolicited event. Events carry EventID, never a request id.
func (s *Sender) Notify(typ string, payload any) error {
	return s.Send(EventID, typ, payload)
}

func (s *Sender) write(env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return s.failed
	}
	if err := WriteMessage(s.w, data); err != nil {
		s.failed = err
		return err
	}
	return nil
}

// Serve reads length-prefixed requests from r and writes responses to w until
// EOF or a framing error. Malformed JSON inside a valid frame is answered with
// an error response and does not stop the stream.
func Serve(ctx context.Context, r io.Reader, w io.Writer, handlers map[string]Handler) error {
	return ServeWithSender(ctx, r, NewSender(w), handlers)
}

// ServeWithSender is Serve with a caller-owned Sender, so the same stream can
// also carry events from other goroutines.
func ServeWithSender(ctx context.Context, r io.Reader, sender *Sender, handlers map[string]Handler) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := ReadMessage(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := handleFrame(ctx, raw, sender, handlers); err != nil {
			return err
		}
	}
}

func handleFrame(ctx context.Context, raw []byte, sender *Sender, handlers map[string]Handler) error {
	var env Envelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return writeError(sender, recoverID(raw), "malformed", "malformed message")
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return writeError(sender, safeID(env.ID), "malformed", "malformed message")
	}
	if err := env.Validate(); err != nil {
		return writeError(sender, safeID(env.ID), "malformed", "malformed message")
	}
	if env.ID == EventID {
		return writeError(sender, "", "malformed", "reserved id")
	}

	handler, ok := handlers[env.Type]
	if !ok {
		return writeError(sender, env.ID, "unknown_type", "unknown message type")
	}
	resp, err := handler(ctx, env.Payload)
	if err != nil {
		var handlerErr *HandlerError
		if errors.As(err, &handlerErr) {
			return writeError(sender, env.ID, handlerErr.Code, handlerErr.Message)
		}
		return writeError(sender, env.ID, "internal", "request failed")
	}
	if err := validateResponse(resp); err != nil {
		return writeError(sender, env.ID, "internal", "request failed")
	}
	return sender.Send(env.ID, resp.Type, resp.Payload)
}

func validateResponse(resp Response) error {
	if !typePattern.MatchString(resp.Type) || len(resp.Type) > maxTypeBytes {
		return fmt.Errorf("%w: invalid response type", ErrMalformedMessage)
	}
	return nil
}

func writeError(sender *Sender, id, code, message string) error {
	if !codePattern.MatchString(code) {
		code = "internal"
	}
	return sender.Send(id, "error", ErrorPayload{Code: code, Message: safeMessage(message)})
}

func recoverID(raw []byte) string {
	var loose struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		return ""
	}
	return safeID(loose.ID)
}

func safeID(id string) string {
	if idPattern.MatchString(id) {
		return id
	}
	return ""
}

func safeMessage(message string) string {
	const maxRunes = 200
	var b strings.Builder
	count := 0
	for _, r := range message {
		if count >= maxRunes {
			break
		}
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
