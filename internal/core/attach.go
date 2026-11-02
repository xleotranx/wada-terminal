package core

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"wada-terminal/internal/vt"
)

// Stream is one attached view of a terminal: the core sends the screen and
// then what the program writes; the viewer sends input and resizes. It is
// safe to read on a worker and write from the UI thread.
type Stream struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	gone bool
}

// Attach opens a stream to a terminal, giving it the viewer's size and colors,
// and returns the screen as it was when the viewer joined.
func (c *Client) Attach(id, cols, rows int, fg, bg [3]uint8) (*Stream, vt.Snapshot, error) {
	if c.dial == nil {
		return nil, vt.Snapshot{}, fmt.Errorf("core: the client has no transport")
	}
	conn, err := c.dial()
	if err != nil {
		return nil, vt.Snapshot{}, fmt.Errorf("core: attach: %w", err)
	}
	req := struct {
		Method   string   `json:"method"`
		Terminal int      `json:"terminal"`
		Cols     int      `json:"cols"`
		Rows     int      `json:"rows"`
		FgRGB    [3]uint8 `json:"fg"`
		BgRGB    [3]uint8 `json:"bg"`
	}{Method: "attach", Terminal: id, Cols: cols, Rows: rows, FgRGB: fg, BgRGB: bg}
	body, err := jsonMarshal(req)
	if err != nil {
		conn.Close()
		return nil, vt.Snapshot{}, err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(append(body, '\n')); err != nil {
		conn.Close()
		return nil, vt.Snapshot{}, err
	}
	_ = conn.SetWriteDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	stream := &Stream{conn: conn, r: bufio.NewReader(conn)}
	kind, payload, err := readFrame(stream.r)
	if err != nil {
		conn.Close()
		return nil, vt.Snapshot{}, fmt.Errorf("core: attach: %w", err)
	}
	if kind == frameExit {
		exit, _ := decodeExit(payload)
		conn.Close()
		if exit.Error == "" {
			exit.Error = "the program has ended"
		}
		return nil, vt.Snapshot{}, fmt.Errorf("core: %s", exit.Error)
	}
	if kind != frameSnapshot {
		conn.Close()
		return nil, vt.Snapshot{}, fmt.Errorf("core: attach did not send a screen")
	}
	var snap vt.Snapshot
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&snap); err != nil {
		conn.Close()
		return nil, vt.Snapshot{}, fmt.Errorf("core: unreadable screen: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return stream, snap, nil
}

// Next reads the next thing the core sent. It returns data, or why the
// program ended, or an error when the stream is gone.
func (s *Stream) Next() (data []byte, exit *Exit, err error) {
	for {
		kind, payload, err := readFrame(s.r)
		if err != nil {
			if err == io.EOF {
				return nil, nil, io.ErrUnexpectedEOF
			}
			return nil, nil, err
		}
		switch kind {
		case frameData:
			return payload, nil, nil
		case frameExit:
			ended, err := decodeExit(payload)
			if err != nil {
				return nil, nil, err
			}
			return nil, &ended, nil
		}
	}
}

// Input sends bytes to the program.
func (s *Stream) Input(b []byte) error { return s.send(frameInput, b) }

// Resize tells the program its terminal changed size.
func (s *Stream) Resize(cols, rows int) error {
	var payload [4]byte
	binary.LittleEndian.PutUint16(payload[0:], uint16(cols))
	binary.LittleEndian.PutUint16(payload[2:], uint16(rows))
	return s.send(frameResize, payload[:])
}

// Close detaches this viewer. The terminal keeps running in the core.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.gone {
		s.mu.Unlock()
		return nil
	}
	s.gone = true
	s.mu.Unlock()
	return s.conn.Close()
}

// send writes one frame, whole, under the stream's write lock.
func (s *Stream) send(kind byte, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gone {
		return net.ErrClosed
	}
	body := make([]byte, 5+len(payload))
	body[0] = kind
	binary.LittleEndian.PutUint32(body[1:], uint32(len(payload)))
	copy(body[5:], payload)
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := s.conn.Write(body)
	_ = s.conn.SetWriteDeadline(time.Time{})
	return err
}
