package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"
)

// SocketName is the core's socket in the app's data folder.
const SocketName = "core.sock"

// InstanceName is the file that names the running core, for a person looking
// at the folder and for a client that wants to know whether the core it talks
// to is the one its layout was saved from.
const InstanceName = "core.json"

// SocketPath is where a core with this data folder listens.
func SocketPath(dataDir string) string { return filepath.Join(dataDir, SocketName) }

// Request is one control call. Start carries a new terminal; Terminal names an
// existing one for kill. Attach uses its own connection.
type Request struct {
	Method       string          `json:"method"`
	Terminal     int             `json:"terminal,omitempty"`
	Start        *Start          `json:"start,omitempty"`
	Cols         int             `json:"cols,omitempty"`
	Rows         int             `json:"rows,omitempty"`
	FgRGB        [3]uint8        `json:"fg,omitempty"`
	BgRGB        [3]uint8        `json:"bg,omitempty"`
	Data         []byte          `json:"data,omitempty"`  // input for send
	Lines        int             `json:"lines,omitempty"` // how much of the screen for read
	Layout       json.RawMessage `json:"layout,omitempty"`
	BaseRevision int64           `json:"baseRevision,omitempty"`

	// Questions: a child's ask to its parent, and the parent's answer.
	Child    int    `json:"child,omitempty"`
	Parent   int    `json:"parent,omitempty"`
	Question string `json:"question,omitempty"`
	Text     string `json:"text,omitempty"`
	Answer   string `json:"answer,omitempty"`
	Wait     int64  `json:"wait,omitempty"` // milliseconds to wait for an answer
}

// Start describes a program for the core to run.
type Start struct {
	Argv   []string          `json:"argv"`
	Kind   string            `json:"kind,omitempty"`
	Name   string            `json:"name,omitempty"`
	Parent int               `json:"parent,omitempty"` // the terminal that started it
	Notify bool              `json:"notify,omitempty"` // tell the parent when it ends
	Dir    string            `json:"dir,omitempty"`
	Cols   int               `json:"cols,omitempty"`
	Rows   int               `json:"rows,omitempty"`
	Dark   bool              `json:"dark,omitempty"`
	FgRGB  [3]uint8          `json:"fg,omitempty"`
	BgRGB  [3]uint8          `json:"bg,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Exit is why a terminal's program ended.
type Exit struct {
	Error string `json:"error,omitempty"`
	Code  int    `json:"code,omitempty"`
}

// Record is one terminal as a client sees it.
type Record struct {
	ID         int               `json:"id"`
	Kind       string            `json:"kind"`
	Argv       []string          `json:"argv"`
	Dir        string            `json:"dir"`
	Name       string            `json:"name,omitempty"`
	Parent     int               `json:"parent,omitempty"`
	Cols       int               `json:"cols"`
	Rows       int               `json:"rows"`
	Alive      bool              `json:"alive"`
	Title      string            `json:"title,omitempty"`
	Cwd        string            `json:"cwd,omitempty"`
	App        string            `json:"app,omitempty"`
	Word       string            `json:"word,omitempty"`
	Message    string            `json:"message,omitempty"`
	Progress   int               `json:"progress,omitempty"`
	Reported   bool              `json:"reported,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Foreground string            `json:"foreground,omitempty"`
	Started    int64             `json:"started,omitempty"`
	Exit       *Exit             `json:"exit,omitempty"`
}

// ReadResult is what a terminal last printed, and whether it is still running.
type ReadResult struct {
	Terminal int      `json:"terminal"`
	Lines    []string `json:"lines"`
	Alive    bool     `json:"alive"`
	Exit     *Exit    `json:"exit,omitempty"`
}

// Question is a helper's question to its parent. The helper's ask waits until
// Answer is set or Closed is true.
type Question struct {
	ID       string `json:"id"`
	Child    int    `json:"child"`
	Parent   int    `json:"parent"`
	Text     string `json:"text"`
	Answer   string `json:"answer,omitempty"`
	Asked    int64  `json:"asked"`
	Answered int64  `json:"answered,omitempty"`
	Closed   bool   `json:"closed,omitempty"`
}

// Event is one change in the core, sent to a client that asked to watch.
type Event struct {
	Type     string  `json:"type"` // ready, created, changed, exit
	Terminal int     `json:"terminal,omitempty"`
	Record   *Record `json:"record,omitempty"`
}

// EventStream is the core's events, line by line, until the connection ends.
type EventStream struct {
	conn net.Conn
	r    *bufio.Reader
	once sync.Once
}

// Ask records a child's question to its parent, which is typed into the
// parent's input as a tagged note. The answer arrives through Question.
func (c *Client) Ask(child, parent int, text string) (Question, error) {
	result, err := c.call(Request{Method: "ask", Child: child, Parent: parent, Text: text})
	if err != nil {
		return Question{}, err
	}
	var question Question
	if err := jsonUnmarshal(result, &question); err != nil {
		return Question{}, err
	}
	return question, nil
}

// Questions lists every question the core still knows.
func (c *Client) Questions() ([]Question, error) {
	result, err := c.call(Request{Method: "questions"})
	if err != nil {
		return nil, err
	}
	var questions []Question
	if err := jsonUnmarshal(result, &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

// WaitQuestion waits up to timeout for an answer or a close, so a local or
// remote ask does not poll the core.
func (c *Client) WaitQuestion(id string, timeout time.Duration) (Question, error) {
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	result, err := c.callWithin(Request{Method: "question-wait", Question: id, Wait: timeout.Milliseconds()}, timeout+5*time.Second)
	if err != nil {
		return Question{}, err
	}
	var question Question
	if err := jsonUnmarshal(result, &question); err != nil {
		return Question{}, err
	}
	return question, nil
}

// Question reads one question, including its answer.
func (c *Client) Question(id string) (Question, error) {
	result, err := c.call(Request{Method: "question", Question: id})
	if err != nil {
		return Question{}, err
	}
	var question Question
	if err := jsonUnmarshal(result, &question); err != nil {
		return Question{}, err
	}
	return question, nil
}

// AnswerQuestion answers an open question.
func (c *Client) AnswerQuestion(id, answer string) (Question, error) {
	result, err := c.call(Request{Method: "answer", Question: id, Answer: answer})
	if err != nil {
		return Question{}, err
	}
	var question Question
	if err := jsonUnmarshal(result, &question); err != nil {
		return Question{}, err
	}
	return question, nil
}

// CancelQuestion closes a question whose asker stopped waiting.
func (c *Client) CancelQuestion(id string) error {
	_, err := c.call(Request{Method: "question-cancel", Question: id})
	return err
}

// Events subscribes to the core's changes. The first event is ready; after
// that every created terminal, status change and exit arrives.
func (c *Client) Events() (*EventStream, error) {
	conn, err := net.DialTimeout("unix", c.socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("core: events: %w", err)
	}
	body, err := jsonMarshal(Request{Method: "events"})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		conn.Close()
		return nil, err
	}
	return &EventStream{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Next reads the next event.
func (e *EventStream) Next() (Event, error) {
	var event Event
	if err := json.NewDecoder(e.r).Decode(&event); err != nil {
		return Event{}, err
	}
	return event, nil
}

// Close ends the subscription.
func (e *EventStream) Close() error {
	e.once.Do(func() { _ = e.conn.Close() })
	return nil
}

// List is the answer to `list`.
type List struct {
	Instance  string   `json:"instance"`
	Terminals []Record `json:"terminals"`
}

// Ping is the answer to `ping`.
type Ping struct {
	Instance string `json:"instance"`
	Version  int    `json:"version"`
}

// protocolVersion changes when an old client can no longer talk to a core.
const protocolVersion = 1

// Response is a control answer. Exactly one of Result and Error is set.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Client talks to a running core. It keeps no connection: a call is one
// connection with one request and one answer, which is cheap on a local
// socket and leaves nothing to leak if the core restarts.
type Client struct {
	dir      string
	socket   string
	instance string
	dial     func() (net.Conn, error)
}

// Connect finds a running core and learns its instance.
func Connect(dir string) (*Client, error) {
	if dir == "" {
		return nil, errors.New("core: no data folder")
	}
	client := &Client{dir: dir, socket: SocketPath(dir), dial: func() (net.Conn, error) {
		return net.DialTimeout("unix", SocketPath(dir), 2*time.Second)
	}}
	result, err := client.call(Request{Method: "ping"})
	if err != nil {
		return nil, err
	}
	var ping Ping
	if err := jsonUnmarshal(result, &ping); err != nil {
		return nil, err
	}
	if ping.Version != protocolVersion {
		return nil, fmt.Errorf("core: speaks protocol %d; this build speaks %d", ping.Version, protocolVersion)
	}
	client.instance = ping.Instance
	return client, nil
}

// Transport opens one connection to a core. A local core is a Unix socket; a
// core on another machine is an SSH command that carries the same bytes over
// its standard input and output.
type Transport interface {
	Dial() (net.Conn, error)
}

// ConnectTransport finds a core over a transport and learns its instance.
func ConnectTransport(transport Transport) (*Client, error) {
	if transport == nil {
		return nil, errors.New("core: no transport")
	}
	client := &Client{dial: transport.Dial}
	result, err := client.call(Request{Method: "ping"})
	if err != nil {
		return nil, err
	}
	var ping Ping
	if err := jsonUnmarshal(result, &ping); err != nil {
		return nil, err
	}
	if ping.Version != protocolVersion {
		return nil, fmt.Errorf("core: speaks protocol %d; this build speaks %d", ping.Version, protocolVersion)
	}
	client.instance = ping.Instance
	return client, nil
}

// Instance is the id of the running core.
func (c *Client) Instance() string { return c.instance }

// Dir is the data folder the core uses.
func (c *Client) Dir() string { return c.dir }

// Start runs a program in a new terminal and returns its record.
func (c *Client) Start(start Start) (Record, error) {
	result, err := c.call(Request{Method: "start", Start: &start})
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := jsonUnmarshal(result, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// List returns every terminal the core holds, live or finished.
func (c *Client) List() (List, error) {
	return c.ListWithin(10 * time.Second)
}

// ListWithin is List with a shorter deadline, for a background refresh that
// must not hold up closing the window.
func (c *Client) ListWithin(timeout time.Duration) (List, error) {
	result, err := c.callWithin(Request{Method: "list"}, timeout)
	if err != nil {
		return List{}, err
	}
	var list List
	if err := jsonUnmarshal(result, &list); err != nil {
		return List{}, err
	}
	return list, nil
}

// Kill ends a terminal and its program.
func (c *Client) Kill(id int) error {
	_, err := c.call(Request{Method: "kill", Terminal: id})
	return err
}

// Send gives a terminal's program input. It is the text a person would type,
// with any control bytes the program understands.
func (c *Client) Send(id int, data []byte) error {
	_, err := c.call(Request{Method: "send", Terminal: id, Data: data})
	return err
}

// Read returns the last lines of a terminal's screen. It works after the
// program ended too, while the core still holds the terminal.
func (c *Client) Read(id, lines int) (ReadResult, error) {
	result, err := c.call(Request{Method: "read", Terminal: id, Lines: lines})
	if err != nil {
		return ReadResult{}, err
	}
	var read ReadResult
	if err := jsonUnmarshal(result, &read); err != nil {
		return ReadResult{}, err
	}
	return read, nil
}

// call sends one request.
func (c *Client) call(req Request) (json.RawMessage, error) {
	return c.callWithin(req, 10*time.Second)
}

// callWithin is call with a chosen I/O deadline.
func (c *Client) callWithin(req Request, timeout time.Duration) (json.RawMessage, error) {
	if c.dial == nil {
		return nil, errors.New("core: the client has no transport")
	}
	conn, err := c.dial()
	if err != nil {
		if c.socket != "" {
			return nil, fmt.Errorf("core: no core at %s: %w", c.socket, err)
		}
		return nil, fmt.Errorf("core: no core: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	body, err := jsonMarshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return nil, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("core: unreadable answer: %w", err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Result, nil
}

// Layout reads the core's saved spaces document.
func (c *Client) Layout() (json.RawMessage, error) {
	layout, _, err := c.LayoutVersioned()
	return layout, err
}

// LayoutVersioned reads the document and the revision it is at.
func (c *Client) LayoutVersioned() (json.RawMessage, int64, error) {
	result, err := c.call(Request{Method: "layout.get"})
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		Layout   json.RawMessage `json:"layout"`
		Revision int64           `json:"revision"`
	}
	if err := jsonUnmarshal(result, &out); err != nil {
		return nil, 0, err
	}
	return out.Layout, out.Revision, nil
}

// LayoutWithin is Layout with a shorter deadline for background sync.
func (c *Client) LayoutWithin(timeout time.Duration) (json.RawMessage, int64, error) {
	result, err := c.callWithin(Request{Method: "layout.get"}, timeout)
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		Layout   json.RawMessage `json:"layout"`
		Revision int64           `json:"revision"`
	}
	if err := jsonUnmarshal(result, &out); err != nil {
		return nil, 0, err
	}
	return out.Layout, out.Revision, nil
}

// SetLayout replaces the core's saved spaces document. The core does not
// interpret it; a window owns the shape.
func (c *Client) SetLayout(layout json.RawMessage) error {
	return c.SetLayoutAt(layout, 0)
}

// SetLayoutAt replaces the document when base is 0 or still the current
// revision; otherwise the write loses to a newer one.
func (c *Client) SetLayoutAt(layout json.RawMessage, base int64) error {
	_, err := c.call(Request{Method: "layout.set", Layout: layout, BaseRevision: base})
	return err
}

// SetLayoutAtWithin is SetLayoutAt with a deadline for background saves.
func (c *Client) SetLayoutAtWithin(layout json.RawMessage, base int64, timeout time.Duration) error {
	_, err := c.callWithin(Request{Method: "layout.set", Layout: layout, BaseRevision: base}, timeout)
	return err
}
