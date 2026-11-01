package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
)

func serveFrames(t *testing.T, handlers map[string]Handler, payloads ...[]byte) []Envelope {
	t.Helper()
	var stream bytes.Buffer
	for _, payload := range payloads {
		stream.Write(encodeFrame(payload))
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), &stream, &out, handlers); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var responses []Envelope
	for {
		raw, err := ReadMessage(&out)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		responses = append(responses, decodeResponse(t, raw))
	}
	return responses
}

func okHandler() map[string]Handler {
	return map[string]Handler{
		"hello": func(context.Context, json.RawMessage) (Response, error) {
			return Response{Type: "hello.result", Payload: map[string]int{"protocolVersion": 1}}, nil
		},
	}
}

func TestServeCopiesRequestIDAndType(t *testing.T) {
	responses := serveFrames(t, okHandler(), []byte(`{"id":"req-1","type":"hello"}`))
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	if responses[0].ID != "req-1" || responses[0].Type != "hello.result" {
		t.Fatalf("got %+v", responses[0])
	}
}

func TestServeRejectsUnknownType(t *testing.T) {
	responses := serveFrames(t, okHandler(), []byte(`{"id":"req-1","type":"nope"}`))
	if len(responses) != 1 || responses[0].Type != "error" {
		t.Fatalf("got %+v", responses)
	}
	assertErrorCode(t, responses[0], "unknown_type")
}

func TestServeRejectsMalformedEnvelopes(t *testing.T) {
	cases := map[string]string{
		"not json":        `not json`,
		"missing id":      `{"type":"hello"}`,
		"invalid id":      `{"id":"bad id","type":"hello"}`,
		"missing type":    `{"id":"req-1"}`,
		"unknown field":   `{"id":"req-1","type":"hello","extra":1}`,
		"trailing json":   `{"id":"req-1","type":"hello"}{}`,
		"payload not obj": `{"id":"req-1","type":"hello","payload":"x"}`,
		"payload array":   `{"id":"req-1","type":"hello","payload":[]}`,
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			responses := serveFrames(t, okHandler(), []byte(frame))
			if len(responses) != 1 || responses[0].Type != "error" {
				t.Fatalf("got %+v", responses)
			}
			assertErrorCode(t, responses[0], "malformed")
		})
	}
}

func TestServeKeepsStreamAfterBadRequest(t *testing.T) {
	responses := serveFrames(t, okHandler(),
		[]byte(`not json`),
		[]byte(`{"id":"req-2","type":"hello"}`),
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d", len(responses))
	}
	if responses[0].Type != "error" || responses[1].Type != "hello.result" {
		t.Fatalf("got %+v", responses)
	}
}

func TestServeMapsHandlerErrors(t *testing.T) {
	handlers := map[string]Handler{
		"hello": func(context.Context, json.RawMessage) (Response, error) {
			return Response{}, &HandlerError{Code: "policy_unavailable", Message: "no policy"}
		},
		"tunnel.status": func(context.Context, json.RawMessage) (Response, error) {
			return Response{}, errors.New("secret internal detail")
		},
	}
	responses := serveFrames(t, handlers,
		[]byte(`{"id":"a","type":"hello"}`),
		[]byte(`{"id":"b","type":"tunnel.status"}`),
	)
	if len(responses) != 2 {
		t.Fatalf("responses = %d", len(responses))
	}
	assertErrorCode(t, responses[0], "policy_unavailable")
	assertErrorCode(t, responses[1], "internal")
	var payload ErrorPayload
	if err := json.Unmarshal(responses[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Message != "request failed" {
		t.Fatalf("internal error leaked: %q", payload.Message)
	}
}

func TestServeRejectsOversizedFrame(t *testing.T) {
	var stream bytes.Buffer
	header := make([]byte, 4)
	header[0] = 1
	header[2] = 0x10
	stream.Write(header)
	var out bytes.Buffer
	err := Serve(context.Background(), &stream, &out, okHandler())
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestServeHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, bytes.NewReader(nil), &bytes.Buffer{}, okHandler()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestServeRejectsInvalidResponseType(t *testing.T) {
	handlers := map[string]Handler{
		"hello": func(context.Context, json.RawMessage) (Response, error) {
			return Response{Type: "Bad Type"}, nil
		},
	}
	responses := serveFrames(t, handlers, []byte(`{"id":"a","type":"hello"}`))
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	assertErrorCode(t, responses[0], "internal")
}

func TestServeRejectsReservedEventID(t *testing.T) {
	responses := serveFrames(t, okHandler(), []byte(`{"id":"event","type":"hello"}`))
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	assertErrorCode(t, responses[0], "malformed")
	if responses[0].ID != "" {
		t.Fatalf("error echoed reserved id %q", responses[0].ID)
	}
}

func TestSenderSerializesConcurrentEvents(t *testing.T) {
	var out bytes.Buffer
	sender := NewSender(&out)
	const writers = 8
	const each = 25

	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				if err := sender.Notify("policy.changed", struct{}{}); err != nil {
					t.Errorf("notify: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	count := 0
	for {
		raw, err := ReadMessage(&out)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		env := decodeResponse(t, raw)
		if env.ID != EventID || env.Type != "policy.changed" {
			t.Fatalf("event = %+v", env)
		}
		count++
	}
	if count != writers*each {
		t.Fatalf("events = %d, want %d", count, writers*each)
	}
}

func TestSenderStaysFailedAfterWriteError(t *testing.T) {
	sender := NewSender(errWriter{})
	if err := sender.Notify("policy.changed", struct{}{}); err == nil {
		t.Fatal("expected write error")
	}
	if err := sender.Notify("policy.changed", struct{}{}); err == nil {
		t.Fatal("expected the failed stream to stay failed")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func assertErrorCode(t *testing.T, env Envelope, want string) {
	t.Helper()
	if env.Type != "error" {
		t.Fatalf("type = %q, want error", env.Type)
	}
	var payload ErrorPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Code != want {
		t.Fatalf("code = %q, want %q", payload.Code, want)
	}
}
