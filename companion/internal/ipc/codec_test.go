package ipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func encodeFrame(payload []byte) []byte {
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	return append(header[:], payload...)
}

func decodeResponse(t *testing.T, raw []byte) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env
}

func TestReadWriteMessageRoundTrip(t *testing.T) {
	payload := []byte(`{"id":"1","type":"hello"}`)
	var buf bytes.Buffer
	if err := WriteMessage(&buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

func TestReadMessageUsesLittleEndianLength(t *testing.T) {
	raw := []byte{2, 0, 0, 0, 'o', 'k'}
	got, err := ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestReadMessageRejectsEmptyFrame(t *testing.T) {
	raw := []byte{0, 0, 0, 0}
	if _, err := ReadMessage(bytes.NewReader(raw)); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("error = %v, want ErrMalformedMessage", err)
	}
}

func TestReadMessageRejectsOversizedFrame(t *testing.T) {
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], MaxMessageBytes+1)
	if _, err := ReadMessage(bytes.NewReader(header[:])); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("error = %v, want ErrMessageTooLarge", err)
	}
}

func TestReadMessageTruncatedPayload(t *testing.T) {
	raw := []byte{4, 0, 0, 0, 'a', 'b'}
	if _, err := ReadMessage(bytes.NewReader(raw)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want ErrUnexpectedEOF", err)
	}
}

func TestReadMessageEOFBetweenFrames(t *testing.T) {
	if _, err := ReadMessage(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want EOF", err)
	}
}

func TestReadMessageLeavesNextFrame(t *testing.T) {
	stream := append(encodeFrame([]byte("one")), encodeFrame([]byte("two"))...)
	reader := bytes.NewReader(stream)
	first, err := ReadMessage(reader)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ReadMessage(reader)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if string(first) != "one" || string(second) != "two" {
		t.Fatalf("got %q and %q", first, second)
	}
}

func TestWriteMessageRejectsEmptyAndOversized(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, nil); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("empty error = %v", err)
	}
	if err := WriteMessage(&buf, make([]byte, MaxMessageBytes+1)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("oversized error = %v", err)
	}
}

type shortWriter struct {
	w io.Writer
}

func (s shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return s.w.Write(p)
}

func TestWriteMessageHandlesShortWrites(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(shortWriter{w: &buf}, []byte("abc")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "abc" {
		t.Fatalf("got %q", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestWriteMessagePropagatesError(t *testing.T) {
	if err := WriteMessage(failingWriter{}, []byte("abc")); err == nil {
		t.Fatal("expected error")
	}
}
