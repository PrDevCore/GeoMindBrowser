package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ReadMessage reads one length-prefixed frame. A clean EOF between frames is
// returned as io.EOF.
func ReadMessage(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 {
		return nil, fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	if size > MaxMessageBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrMessageTooLarge, size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteMessage writes one length-prefixed frame.
func WriteMessage(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty frame", ErrMalformedMessage)
	}
	if len(payload) > MaxMessageBytes {
		return fmt.Errorf("%w: %d bytes", ErrMessageTooLarge, len(payload))
	}
	var header [4]byte
	// Bounded by MaxMessageBytes above, so the conversion cannot overflow.
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload))) //nolint:gosec // G115: bounded length.
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
