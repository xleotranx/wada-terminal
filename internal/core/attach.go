package core

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	mterm "github.com/egoist/mygo/plugins/terminal"
)

// Stream is one attached view of a terminal. Read gives the frames of the
// core's terminal as VT sequences, which a window's terminal shows cell for
// cell (terminal.Mirror on the core's side): it is that terminal's Conn, with
// ResizePixels and UseTheme, which it calls with its size and colors. The
// sizes the core's terminal takes, and what the screen does not carry, come
// through OnResized and OnInfo, within Read, before the frames after them.
//
// A stream whose connection breaks while the program runs attaches again
// within Read, a few times; the first frame of the new attachment starts the
// window's terminal over. It is safe to read on one goroutine and write from
// others.
type Stream struct {
	client *Client
	id     int

	// Cols and Rows are the size of the first frames.
	Cols, Rows int
	// OnResized, OnInfo and OnOutput, when set before the first Read, get
	// the size the core's terminal took, what the screen does not carry,
	// and word that a frame of output came.
	OnResized func(cols, rows int)
	OnInfo    func(Info)
	OnOutput  func()

	mu     sync.Mutex // guards the fields below and orders writes
	conn   net.Conn
	r      *bufio.Reader
	size   [4]int // the size last asked, with the cells' in pixels
	theme  *Theme // the colors last told
	exit   *Exit
	closed bool

	buf []byte // the rest of a frame Read has not returned
}

// Attach opens a stream to a terminal, giving it the size the viewer fits
// and its colors. The program's end comes as an error when it already ended.
func (c *Client) Attach(id, cols, rows int, theme *Theme) (*Stream, error) {
	s := &Stream{client: c, id: id, size: [4]int{cols, rows}, theme: theme}
	if err := s.attach(); err != nil {
		return nil, err
	}
	return s, nil
}

// attach dials the core and reads the size of the first frames.
func (s *Stream) attach() error {
	if s.client == nil || s.client.dial == nil {
		return fmt.Errorf("core: the client has no transport")
	}
	conn, err := s.client.dial()
	if err != nil {
		return fmt.Errorf("core: attach: %w", err)
	}
	s.mu.Lock()
	req := Request{Method: "attach", Terminal: s.id, Cols: s.size[0], Rows: s.size[1], Theme: s.theme}
	s.mu.Unlock()
	body, err := jsonMarshal(req)
	if err != nil {
		conn.Close()
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(append(body, '\n')); err != nil {
		conn.Close()
		return err
	}
	_ = conn.SetWriteDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	kind, payload, err := readFrame(r)
	if err != nil {
		conn.Close()
		return fmt.Errorf("core: attach: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	switch kind {
	case frameExit:
		exit, _ := decodeExit(payload)
		conn.Close()
		if exit.Error == "" {
			exit.Error = "the program has ended"
		}
		return fmt.Errorf("core: %s", exit.Error)
	case frameResized:
	default:
		conn.Close()
		return fmt.Errorf("core: attach did not send the terminal's size")
	}
	cols, rows, ok := parseSize(payload)
	if !ok {
		conn.Close()
		return fmt.Errorf("core: attach sent an unreadable size")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		conn.Close()
		return net.ErrClosed
	}
	old := s.conn
	s.conn, s.r, s.Cols, s.Rows = conn, r, cols, rows
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// parseSize reads a frameResized payload.
func parseSize(payload []byte) (cols, rows int, ok bool) {
	if len(payload) < 4 {
		return 0, 0, false
	}
	return int(binary.LittleEndian.Uint16(payload[0:])), int(binary.LittleEndian.Uint16(payload[2:])), true
}

// Read returns the next frames of the screen. It returns io.EOF once the
// program ended (Exit says how) or the stream was closed.
func (s *Stream) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		s.mu.Lock()
		r := s.r
		s.mu.Unlock()
		kind, payload, err := readFrame(r)
		if err != nil {
			if s.reattach() {
				if s.OnResized != nil {
					s.OnResized(s.currentSize())
				}
				continue
			}
			s.mu.Lock()
			if s.exit == nil && !s.closed {
				s.exit = &Exit{Error: "the core's connection was lost: " + err.Error()}
			}
			s.mu.Unlock()
			return 0, io.EOF
		}
		switch kind {
		case frameData:
			s.buf = payload
			if s.OnOutput != nil {
				s.OnOutput()
			}
		case frameResized:
			if cols, rows, ok := parseSize(payload); ok {
				s.mu.Lock()
				s.Cols, s.Rows = cols, rows
				s.mu.Unlock()
				if s.OnResized != nil {
					s.OnResized(cols, rows)
				}
			}
		case frameInfo:
			var info Info
			if jsonUnmarshal(payload, &info) == nil && s.OnInfo != nil {
				s.OnInfo(info)
			}
		case frameExit:
			exit, _ := decodeExit(payload)
			s.mu.Lock()
			s.exit = &exit
			s.mu.Unlock()
			return 0, io.EOF
		}
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// currentSize is the size of the frames the stream reads now.
func (s *Stream) currentSize() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cols, s.Rows
}

// reattach joins the same terminal again after the connection broke, unless
// the stream was closed; the core kept everything that happened in between.
func (s *Stream) reattach() bool {
	for attempt := 0; attempt < 3; attempt++ {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return false
		}
		time.Sleep(200 * time.Millisecond)
		err := s.attach()
		if err == nil {
			return true
		}
		if errors.Is(err, net.ErrClosed) {
			return false
		}
	}
	return false
}

// Next reads the next thing the core sent, for a viewer that is not a
// terminal: data, or why the program ended, or an error when the stream is
// gone. It does not attach again.
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

// Exit is why the program ended, or why the stream did, once Read returned
// io.EOF; nil when the stream was closed.
func (s *Stream) Exit() *Exit {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exit == nil {
		return nil
	}
	exit := *s.exit
	return &exit
}

// Write sends bytes to the program, as if typed.
func (s *Stream) Write(p []byte) (int, error) {
	if err := s.send(frameInput, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ResizePixels tells the core the size the window's terminal fits, and its
// cells' in pixels, which a program asking for the size in pixels gets.
func (s *Stream) ResizePixels(cols, rows, cellWidth, cellHeight int) error {
	s.mu.Lock()
	s.size = [4]int{cols, rows, cellWidth, cellHeight}
	s.mu.Unlock()
	var payload [8]byte
	binary.LittleEndian.PutUint16(payload[0:], uint16(cols))
	binary.LittleEndian.PutUint16(payload[2:], uint16(rows))
	binary.LittleEndian.PutUint16(payload[4:], uint16(cellWidth))
	binary.LittleEndian.PutUint16(payload[6:], uint16(cellHeight))
	return s.send(frameResize, payload[:])
}

// Resize tells the core the size the window's terminal fits.
func (s *Stream) Resize(cols, rows int) error {
	s.mu.Lock()
	cellW, cellH := s.size[2], s.size[3]
	s.mu.Unlock()
	return s.ResizePixels(cols, rows, cellW, cellH)
}

// UseTheme tells the core the colors the window shows, which programs asking
// about them get.
func (s *Stream) UseTheme(theme *mterm.Theme) error {
	th := ThemeOf(theme)
	if th == nil {
		return nil
	}
	s.mu.Lock()
	s.theme = th
	s.mu.Unlock()
	payload, err := jsonMarshal(th)
	if err != nil {
		return err
	}
	return s.send(frameTheme, payload)
}

// Clear asks the core's terminal to clear, in order with what is typed.
func (s *Stream) Clear() error { return s.send(frameClear, nil) }

// Close detaches this viewer. The terminal keeps running in the core.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// send writes one frame, whole, under the stream's lock.
func (s *Stream) send(kind byte, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.conn == nil {
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
