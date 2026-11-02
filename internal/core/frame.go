package core

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The attach stream is framed, one message at a time:
//
//	[type:1][length:4 little endian][payload]
//
// The core sends a snapshot first, then the program's output and finally why
// it ended. A viewer sends input, a resize, or nothing.
const (
	frameSnapshot byte = iota // a gob-encoded vt.Snapshot
	frameData                 // raw bytes from the program
	frameExit                 // a JSON Exit
	frameInput                // raw bytes to the program
	frameResize               // cols u16, rows u16
)

// maxFrame bounds one frame: a snapshot of a large scrollback fits.
const maxFrame = 32 << 20

// writeFrame writes one frame to a buffered writer.
func writeFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("core: frame of %d bytes is too large", len(payload))
	}
	var header [5]byte
	header[0] = kind
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame reads one frame and returns its payload. The payload is reused by
// the next call only if the caller passes it back, so callers copy what they
// keep.
func readFrame(r *bufio.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	kind := header[0]
	n := binary.LittleEndian.Uint32(header[1:])
	if n > maxFrame {
		return 0, nil, fmt.Errorf("core: frame of %d bytes is too large", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return kind, payload, nil
}

// frameExitPayload marshals why a terminal ended.
func frameExitPayload(exit Exit) []byte {
	body, err := jsonMarshal(exit)
	if err != nil {
		return []byte(`{"error":"the program ended"}`)
	}
	return body
}

// decodeExit reads a frameExit payload.
func decodeExit(payload []byte) (Exit, error) {
	var exit Exit
	if err := jsonUnmarshal(payload, &exit); err != nil {
		return Exit{}, errors.New("core: unreadable exit frame")
	}
	return exit, nil
}
