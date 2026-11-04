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
// The core sends the size its terminal has and what the window needs beside
// the screen, then frames of the screen (terminal.Mirror) as the program
// draws, and finally why it ended. Each first frame of a stream starts the
// window's terminal over, so a window that attaches again shows the screen as
// it is. A viewer sends input, the size and colors it shows the terminal at,
// and the user's clears.
const (
	frameResized byte = iota // cols u16, rows u16: the size the next frames are drawn at
	frameData                // VT sequences that bring the window's terminal to the core's
	frameExit                // a JSON Exit
	frameInput               // raw bytes to the program
	frameResize              // cols u16, rows u16, cell width u16, cell height u16
	frameInfo                // a JSON Info: status reports and modes the screen does not carry
	frameTheme               // a JSON Theme: the colors the window shows
	frameClear               // the user cleared the screen
)

// maxFrame bounds one frame: a first frame with a large scrollback fits.
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
