package core

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server is one core: its terminals, the socket a window connects to, and the
// instance id a saved layout is tied to.
type Server struct {
	dir      string
	socket   string
	instance string
	ln       net.Listener

	mu        sync.Mutex
	terminals map[int]*terminal
	nextID    int
	closed    bool

	layoutMu  sync.Mutex
	layout    json.RawMessage
	layoutRev int64

	eventMu   sync.Mutex
	events    map[int]chan Event
	nextEvent int
	notified  map[int]string

	questionMu   sync.Mutex
	questions    map[string]*Question
	nextQuestion int
	questionWake chan struct{}
}

// Listen opens the core's socket and starts serving. It refuses a second core
// on the same data folder, and removes a socket a crashed core left behind.
func Listen(dir string) (*Server, error) {
	if dir == "" {
		return nil, fmt.Errorf("core: no data folder")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	socket := SocketPath(dir)
	if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
		conn.Close()
		return nil, fmt.Errorf("core: another core is already listening at %s", socket)
	}
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{dir: dir, socket: socket, instance: newInstance(), ln: ln,
		terminals: map[int]*terminal{}, nextID: 0,
		events: map[int]chan Event{}, notified: map[int]string{},
		questions: map[string]*Question{}, questionWake: make(chan struct{})}
	if body, err := os.ReadFile(filepath.Join(dir, "layout.json")); err == nil && json.Valid(body) {
		s.layout = body
		s.layoutRev = 1
		if raw, err := os.ReadFile(filepath.Join(dir, "layout.rev")); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64); err == nil && n > 0 {
				s.layoutRev = n
			}
		}
	}
	if err := s.writeInstance(); err != nil {
		ln.Close()
		return nil, err
	}
	go s.accept()
	return s, nil
}

// Serve runs a core until the process is asked to stop. A window may come
// and go; the core and its terminals stay.
func Serve(dir string) error {
	s, err := Listen(dir)
	if err != nil {
		return err
	}
	defer s.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	return nil
}

// Close stops accepting, ends every terminal, and removes the socket and the
// instance file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	terminals := make([]*terminal, 0, len(s.terminals))
	for _, t := range s.terminals {
		terminals = append(terminals, t)
	}
	s.mu.Unlock()

	err := s.ln.Close()
	for _, t := range terminals {
		t.kill()
	}
	s.eventMu.Lock()
	for id, ch := range s.events {
		delete(s.events, id)
		close(ch)
	}
	s.eventMu.Unlock()
	_ = os.Remove(s.socket)
	_ = os.Remove(filepath.Join(s.dir, InstanceName))
	return err
}

// accept takes connections until the listener closes.
func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

// handle reads one request. Attach switches the connection to the framed
// stream; every other method answers one JSON line and closes.
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	var req Request
	if err := jsonUnmarshal(line, &req); err != nil {
		s.answer(conn, Response{Error: "not a request: " + err.Error()})
		return
	}
	if req.Method == "attach" {
		_ = conn.SetDeadline(time.Time{})
		s.attach(conn, reader, req)
		return
	}
	if req.Method == "events" {
		_ = conn.SetDeadline(time.Time{})
		s.streamEvents(conn)
		return
	}
	if req.Method == "question-wait" {
		_ = conn.SetDeadline(time.Now().Add(time.Duration(req.Wait)*time.Millisecond + 5*time.Second))
	}
	s.answer(conn, s.dispatch(req))
}

// dispatch runs one control method.
func (s *Server) dispatch(req Request) Response {
	switch req.Method {
	case "ping":
		return result(Ping{Instance: s.instance, Version: protocolVersion})

	case "list":
		s.mu.Lock()
		terminals := make([]*terminal, 0, len(s.terminals))
		for _, t := range s.terminals {
			terminals = append(terminals, t)
		}
		s.mu.Unlock()
		sort.Slice(terminals, func(i, j int) bool { return terminals[i].id < terminals[j].id })
		list := List{Instance: s.instance, Terminals: make([]Record, 0, len(terminals))}
		for _, t := range terminals {
			list.Terminals = append(list.Terminals, t.record())
		}
		return result(list)

	case "start":
		if req.Start == nil {
			return Response{Error: "start needs a program"}
		}
		s.mu.Lock()
		s.nextID++
		id := s.nextID
		s.mu.Unlock()
		t, err := startTerminal(id, *req.Start, s.dir, s.exitNote, s.terminalEvent)
		if err != nil {
			return Response{Error: err.Error()}
		}
		s.mu.Lock()
		s.terminals[id] = t
		s.mu.Unlock()
		s.terminalEvent(t, "created")
		return result(t.record())

	case "layout.get":
		s.layoutMu.Lock()
		layout := append(json.RawMessage(nil), s.layout...)
		s.layoutMu.Unlock()
		return result(map[string]any{"layout": layout, "revision": s.layoutRev})

	case "layout.set":
		if len(req.Layout) == 0 || !json.Valid(req.Layout) {
			return Response{Error: "layout is not valid JSON"}
		}
		s.layoutMu.Lock()
		if req.BaseRevision != 0 && req.BaseRevision != s.layoutRev {
			current := s.layoutRev
			s.layoutMu.Unlock()
			return Response{Error: fmt.Sprintf("layout changed (revision %d)", current)}
		}
		s.layout = append(json.RawMessage(nil), req.Layout...)
		s.layoutRev++
		s.layoutMu.Unlock()
		path := filepath.Join(s.dir, "layout.json")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, req.Layout, 0o600); err != nil {
			return Response{Error: err.Error()}
		}
		if err := os.Rename(tmp, path); err != nil {
			return Response{Error: err.Error()}
		}
		_ = os.WriteFile(filepath.Join(s.dir, "layout.rev"), []byte(strconv.FormatInt(s.layoutRev, 10)), 0o600)
		return result(map[string]any{"saved": true, "revision": s.layoutRev})

	case "kill":
		t := s.terminal(req.Terminal)
		if t == nil {
			return Response{Error: fmt.Sprintf("no terminal %d", req.Terminal)}
		}
		t.kill()
		s.closeQuestionsFor(req.Terminal)
		return result(map[string]any{"killed": req.Terminal})

	case "send":
		t := s.terminal(req.Terminal)
		if t == nil {
			return Response{Error: fmt.Sprintf("no terminal %d", req.Terminal)}
		}
		if err := t.send(req.Data); err != nil {
			return Response{Error: err.Error()}
		}
		return result(map[string]any{"sent": len(req.Data)})

	case "read":
		t := s.terminal(req.Terminal)
		if t == nil {
			return Response{Error: fmt.Sprintf("no terminal %d", req.Terminal)}
		}
		return result(t.read(req.Lines))

	case "ask":
		question, err := s.ask(req.Child, req.Parent, req.Text)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return result(question)

	case "questions":
		return result(s.questionList())

	case "question":
		question, err := s.questionByID(req.Question)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return result(question)

	case "question-wait":
		timeout := time.Duration(req.Wait) * time.Millisecond
		question, err := s.waitQuestion(req.Question, timeout)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return result(question)

	case "answer":
		question, err := s.answerQuestion(req.Question, req.Answer)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return result(question)

	case "question-cancel":
		if err := s.cancelQuestion(req.Question); err != nil {
			return Response{Error: err.Error()}
		}
		return result(map[string]any{"closed": req.Question})

	default:
		return Response{Error: fmt.Sprintf("no method %q", req.Method)}
	}
}

// attach joins one viewer to a terminal. The first frames start the viewer's
// terminal over at the size the terminal takes; frames of the screen follow
// as the program draws, and the viewer's input, size and colors come back on
// the same connection.
func (s *Server) attach(conn net.Conn, reader *bufio.Reader, req Request) {
	t := s.terminal(req.Terminal)
	if t == nil {
		_ = writeFrame(conn, frameExit, frameExitPayload(Exit{Error: fmt.Sprintf("no terminal %d", req.Terminal)}))
		return
	}
	v := newViewer(conn)
	exit, err := t.attachViewer(v, req.Cols, req.Rows, req.Theme)
	if err != nil {
		_ = writeFrame(conn, frameExit, frameExitPayload(Exit{Error: err.Error()}))
		return
	}
	if exit != nil {
		_ = writeFrame(conn, frameExit, frameExitPayload(*exit))
		return
	}
	go func() {
		t.readViewer(reader)
		t.detach(v)
		v.finish()
	}()
	v.write()
	t.detach(v)
}

// exitNote tells a parent that a child it started ended, when the child asked
// for that. The note lands in the parent's input as ordinary text; the parent
// is an agent reading its terminal, not a caller of this core.
func (s *Server) exitNote(child *terminal, exit Exit) {
	s.closeQuestionsFor(child.id)
	if !child.start.Notify || child.start.Parent == 0 {
		return
	}
	parent := s.terminal(child.start.Parent)
	if parent == nil {
		return
	}
	from := address(child.id)
	if name := child.start.Name; name != "" {
		from += " " + name
	}
	text := fmt.Sprintf("\n<wada from=\"%s\" exit=\"%d\">finished</wada>\n", escapeNote(from), exit.Code)
	if exit.Error != "" && exit.Code <= 0 {
		text = fmt.Sprintf("\n<wada from=\"%s\" error=\"%s\">finished</wada>\n", escapeNote(from), escapeNote(exit.Error))
	}
	_ = parent.send([]byte(text + "\r"))
}

// escapeNote keeps a name or an error from closing the note's attribute early.
func escapeNote(text string) string {
	return strings.NewReplacer("\"", "'", "\n", " ", "\r", " ").Replace(text)
}

// subscribe adds an events client and returns its channel.
func (s *Server) subscribe() (int, chan Event) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	s.nextEvent++
	ch := make(chan Event, 256)
	s.events[s.nextEvent] = ch
	return s.nextEvent, ch
}

// unsubscribe forgets an events client.
func (s *Server) unsubscribe(id int) {
	s.eventMu.Lock()
	if ch, ok := s.events[id]; ok {
		delete(s.events, id)
		close(ch)
	}
	s.eventMu.Unlock()
}

// publish sends one event to every watcher. A watcher that stopped reading is
// dropped rather than holding the core back; it can connect again and list.
func (s *Server) publish(event Event) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for id, ch := range s.events {
		select {
		case ch <- event:
		default:
			delete(s.events, id)
			close(ch)
		}
	}
}

// streamEvents keeps one client's event connection open. It writes a ready
// event first, then every change; a read on the connection notices a client
// that went away while nothing was happening.
func (s *Server) streamEvents(conn net.Conn) {
	id, events := s.subscribe()
	defer s.unsubscribe(id)
	if err := writeEvent(conn, Event{Type: "ready"}); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		close(done)
	}()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeEvent(conn, event); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// writeEvent writes one JSON line.
func writeEvent(w io.Writer, event Event) error {
	body, err := jsonMarshal(event)
	if err != nil {
		return err
	}
	_, err = w.Write(append(body, '\n'))
	return err
}

// terminalEvent publishes a terminal change and, when a child that asked for
// notices becomes blocked, types a note into its parent's input.
func (s *Server) terminalEvent(t *terminal, kind string) {
	record := t.record()
	s.publish(Event{Type: kind, Terminal: record.ID, Record: &record})
	if kind != "changed" || !t.start.Notify || record.Parent == 0 {
		return
	}
	blocked := record.Word == "blocked" || record.Word == "error"
	s.eventMu.Lock()
	previous := s.notified[record.ID]
	if !blocked {
		delete(s.notified, record.ID)
		s.eventMu.Unlock()
		return
	}
	if previous == record.Word {
		s.eventMu.Unlock()
		return
	}
	s.notified[record.ID] = record.Word
	s.eventMu.Unlock()
	parent := s.terminal(record.Parent)
	if parent == nil {
		return
	}
	from := address(record.ID)
	if record.Name != "" {
		from += " " + record.Name
	}
	text := record.Message
	if text == "" {
		text = "needs you"
	}
	note := fmt.Sprintf("\n<wada from=\"%s\" state=\"%s\">%s</wada>\n",
		escapeNote(from), escapeNote(record.Word), escapeNote(text))
	_ = parent.send([]byte(note + "\r"))
}

// terminal finds a terminal by id.
func (s *Server) terminal(id int) *terminal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminals[id]
}

// answer writes one JSON response.
func (s *Server) answer(conn net.Conn, resp Response) {
	body, err := jsonMarshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(body, '\n'))
}

// result wraps a value as a successful response.
func result(v any) Response {
	body, err := jsonMarshal(v)
	if err != nil {
		return Response{Error: err.Error()}
	}
	return Response{Result: body}
}

// writeInstance records which core is on this data folder.
func (s *Server) writeInstance() error {
	body, err := jsonMarshal(struct {
		Instance string `json:"instance"`
		PID      int    `json:"pid"`
		Version  int    `json:"version"`
	}{s.instance, os.Getpid(), protocolVersion})
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, InstanceName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// newInstance makes a random id for this core run, so a saved layout can tell
// whether the terminals it names are still the ones it saw.
func newInstance() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
