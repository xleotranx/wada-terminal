// Package control is the protocol between the window and the programs that
// drive it, and between this app and its own command line.
//
// It is JSON, one request and one response per connection, over a Unix socket
// in the app's data folder. The window is one client of it (the app's own
// command line); an agent in a pane is another. It imports nothing from the
// window, so a client can be simple.
package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SocketName is the socket's name in the app's data folder.
const SocketName = "terminal.sock"

// SocketPath is where a window with this data folder listens.
func SocketPath(dataDir string) string {
	return filepath.Join(dataDir, SocketName)
}

// Request is one call: a method (`status`, `ls`, `do`, `commands`) and, for
// `do`, the action and its named arguments.
type Request struct {
	Method string         `json:"method"`
	Action string         `json:"action,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
}

// Response is what the window answers. Exactly one of Result and Error is set.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Call sends one request to a window and returns its result, or its error.
func Call(socket string, req Request) (json.RawMessage, error) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("no window is listening at %s: %w", socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("the window closed the connection: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("the window answered with something that is not JSON: %w", err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Result, nil
}

// Listen opens the window's socket. A leftover socket from a window that did
// not exit cleanly is removed; a live one is left alone and its error
// returned, so a second window never takes another's socket.
func Listen(dataDir string) (net.Listener, error) {
	path := SocketPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		return nil, fmt.Errorf("another window is already listening at %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// ReadRequest reads one JSON request. The caller owns the deadline and the
// connection.
func ReadRequest(conn net.Conn) (Request, error) {
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, fmt.Errorf("not a request: %w", err)
	}
	return req, nil
}

// WriteResponse writes one response line, ignoring a client that left.
func WriteResponse(conn net.Conn, resp Response) {
	body, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(body, '\n'))
}

// Serve answers one connection: one JSON line in, one JSON line out. It is
// the window's side, called from a worker with a callback that runs on the UI
// thread; the callback is where the window's state is read and changed.
func Serve(conn net.Conn, handle func(Request) Response) {
	defer conn.Close()
	req, err := ReadRequest(conn)
	if err != nil {
		WriteResponse(conn, Response{Error: err.Error()})
		return
	}
	WriteResponse(conn, handle(req))
}
