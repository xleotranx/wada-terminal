package core

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/pty"
	"wada-terminal/internal/status"

	mterm "github.com/egoist/mygo/plugins/terminal"
)

// viewer is one attached window: its own Mirror of the terminal, and the
// frames queued for it, which a writer sends in order so that a slow window
// never holds the program back.
type viewer struct {
	conn   net.Conn
	mirror *mterm.Mirror
	out    chan []byte
	done   chan struct{}
	once   sync.Once
}

// viewerQueue is how many frames a viewer may be behind by before it is
// dropped; it can attach again, from a first frame.
const viewerQueue = 4096

func newViewer(conn net.Conn) *viewer {
	return &viewer{conn: conn, out: make(chan []byte, viewerQueue), done: make(chan struct{})}
}

// send queues one frame. It reports false when the viewer is too far behind,
// which the caller then drops.
func (v *viewer) send(kind byte, payload []byte) bool {
	body := make([]byte, 5+len(payload))
	body[0] = kind
	binary.LittleEndian.PutUint32(body[1:], uint32(len(payload)))
	copy(body[5:], payload)
	select {
	case <-v.done:
		return false
	default:
	}
	select {
	case v.out <- body:
		return true
	default:
		return false
	}
}

// finish ends the viewer's stream, once; its writer sends what is queued
// first.
func (v *viewer) finish() {
	v.once.Do(func() { close(v.done) })
}

// write sends the queued frames until the viewer is finished, then what is
// still queued, as the frames of a program that just ended.
func (v *viewer) write() {
	for {
		select {
		case f := <-v.out:
			if _, err := v.conn.Write(f); err != nil {
				v.finish()
				return
			}
		case <-v.done:
			_ = v.conn.SetWriteDeadline(time.Now().Add(time.Second))
			for {
				select {
				case f := <-v.out:
					if _, err := v.conn.Write(f); err != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// headless connects the core's terminal emulator to the program: the core
// feeds it what the program writes, so it reads nothing, and what it writes
// (its answers to the program's queries) goes to the program.
type headless struct {
	proc   *pty.Session
	closed chan struct{}
	once   sync.Once
}

func (h *headless) Read([]byte) (int, error) {
	<-h.closed
	return 0, io.EOF
}

func (h *headless) Write(p []byte) (int, error) {
	_, _ = h.proc.Write(p)
	return len(p), nil
}

func (h *headless) Close() error {
	h.once.Do(func() { close(h.closed) })
	return nil
}

// terminal is one program on a pseudo-terminal, the terminal emulator that
// reads its output (Ghostty's, through MyGo), and the viewers watching it. It
// belongs to the core and outlives any window: each window shows frames of
// this terminal and lays out none of the program's output itself.
type terminal struct {
	id      int
	start   Start
	proc    *pty.Session
	term    *mterm.Terminal
	started time.Time
	onExit  func(*terminal, Exit)   // called once, from the read loop
	onEvent func(*terminal, string) // created, changed or exit
	titled  atomic.Bool             // the program set its title since the last apply

	mu        sync.Mutex
	alive     bool
	exit      *Exit
	viewers   map[*viewer]bool
	cols      int
	rows      int
	cellW     int
	cellH     int
	cwd       string
	scan      status.Stream // OSC 7501 and bracketed paste, which the emulator does not report
	bracketed bool          // what the viewers were last told
	links     []string      // the hyperlinks' targets, the newest maxLinks
	framing   bool          // frames are due shortly
}

// startTerminal runs a program and returns the terminal that holds it.
func startTerminal(id int, start Start, dataDir string, onExit func(*terminal, Exit), onEvent func(*terminal, string)) (*terminal, error) {
	if len(start.Argv) == 0 {
		return nil, errors.New("no program to run")
	}
	if start.Cols < 2 {
		start.Cols = 80
	}
	if start.Rows < 2 {
		start.Rows = 24
	}
	proc, err := pty.Start(pty.Options{
		Argv: start.Argv, Env: terminalEnv(start.Dir, dataDir, start.Dark, id, start.Parent), Dir: start.Dir,
		Cols: start.Cols, Rows: start.Rows,
	})
	if err != nil {
		return nil, err
	}
	t := &terminal{
		id: id, start: start, proc: proc, started: time.Now(),
		alive: true, viewers: map[*viewer]bool{}, cols: start.Cols, rows: start.Rows,
		onExit: onExit, onEvent: onEvent,
	}
	term, err := mterm.New(mterm.Options{
		Conn:  &headless{proc: proc, closed: make(chan struct{})},
		Theme: startTheme(start),
		// Programs' reads of the clipboard go on to the windows, whose
		// terminals answer them as their user allows.
		ClipboardRead: mterm.ClipboardForward,
		OnTitle:       func(string) { t.titled.Store(true) },
	})
	if err != nil {
		_ = proc.Close()
		_ = proc.Wait()
		return nil, err
	}
	term.Resize(start.Cols, start.Rows)
	t.term = term
	go t.run()
	return t, nil
}

// terminalEnv is what a program reads to know it draws into a terminal: the
// app's environment with TERM, truecolor and the light or dark background.
func terminalEnv(dir, dataDir string, dark bool, id, parent int) []string {
	fg, bg := "15", "0"
	if !dark {
		fg, bg = "0", "15"
	}
	env := appdir.TerminalEnv(os.Environ(), dataDir)
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=wada-terminal",
		"COLORFGBG="+fg+";"+bg, "WADA_TERMINAL_ID="+address(id))
	if parent > 0 {
		env = append(env, "WADA_PARENT_ID="+address(parent))
	}
	if dir != "" {
		env = append(env, "PWD="+dir)
	}
	return env
}

// address is how this app names a terminal to people and scripts.
func address(id int) string { return "t" + strconv.Itoa(id) }

// run reads the program's output until it ends.
func (t *terminal) run() {
	buf := make([]byte, 64*1024)
	for {
		n, err := t.proc.Read(buf)
		if n > 0 {
			t.apply(buf[:n])
		}
		if err != nil {
			break
		}
	}
	t.finish(t.proc.Wait())
}

// apply gives the program's bytes to the terminal, tells the viewers what the
// screen does not carry (status reports, bracketed paste) at once, and their
// screens shortly; and answers the OSC 7501 query, which the emulator does
// not know.
func (t *terminal) apply(b []byte) {
	t.mu.Lock()
	if !t.alive {
		t.mu.Unlock()
		return
	}
	t.scan.Write(b)
	reports, replies, links := t.scan.TakeReports(), t.scan.TakeReplies(), t.scan.TakeLinks()
	t.links = status.AppendLinks(t.links, links, maxLinks)
	t.term.Feed(b)
	cwd := t.term.Dir()
	if cwd == "" {
		cwd = t.cwd
	}
	moved := t.titled.Swap(false) || cwd != t.cwd
	changed := len(reports) > 0 || moved
	t.cwd = cwd
	if bracketed := t.scan.Bracketed(); changed || bracketed != t.bracketed || len(links) > 0 {
		t.bracketed = bracketed
		info := t.info()
		info.Reports, info.Links = reports, links
		t.sendInfo(info)
	}
	t.frameSoon()
	t.mu.Unlock()
	for _, reply := range replies {
		_, _ = t.proc.Write([]byte(reply))
	}
	if changed && t.onEvent != nil {
		t.onEvent(t, "changed")
	}
}

// info is what the viewers are told beside the screen, without reports;
// t.mu is held.
func (t *terminal) info() Info {
	return Info{Title: t.term.Title(), Cwd: t.cwd, Reported: t.scan.Reported(), Bracketed: t.scan.Bracketed()}
}

// sendInfo tells every viewer; t.mu is held.
func (t *terminal) sendInfo(info Info) {
	payload, err := jsonMarshal(info)
	if err != nil {
		return
	}
	for v := range t.viewers {
		t.sendTo(v, frameInfo, payload)
	}
}

// sendTo queues a frame for a viewer, dropping one that fell too far behind;
// t.mu is held.
func (t *terminal) sendTo(v *viewer, kind byte, payload []byte) {
	if v.send(kind, payload) {
		return
	}
	delete(t.viewers, v)
	v.mirror.Close()
	v.finish()
}

// maxLinks is how many hyperlink targets a terminal keeps for a window that
// attaches.
const maxLinks = 200

// frameGap is how long frames wait after output, for more of it: a program's
// redraw comes in pieces, and a flood then costs a frame every few
// milliseconds rather than one per read.
const frameGap = 4 * time.Millisecond

// frameSoon sends the viewers a frame shortly; t.mu is held.
func (t *terminal) frameSoon() {
	if t.framing || len(t.viewers) == 0 {
		return
	}
	t.framing = true
	time.AfterFunc(frameGap, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.framing = false
		t.frame()
	})
}

// frame sends each viewer what brings its terminal to what this one shows,
// and tries again a little later while a program's synchronized update holds
// the frame back; t.mu is held.
func (t *terminal) frame() {
	due := false
	for v := range t.viewers {
		f, later := v.mirror.Frame()
		if len(f) > 0 {
			t.sendTo(v, frameData, f)
		}
		due = due || later
	}
	if due && !t.framing {
		t.framing = true
		time.AfterFunc(50*time.Millisecond, func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.framing = false
			t.frame()
		})
	}
}

// finish records why the program ended, sends the viewers the last frames
// and the end, and releases them. The terminal stays, for reading.
func (t *terminal) finish(err error) {
	exit := Exit{}
	if err != nil {
		exit.Error = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit.Code = exitErr.ExitCode()
		}
	}
	t.mu.Lock()
	if !t.alive {
		t.mu.Unlock()
		return
	}
	t.alive = false
	t.exit = &exit
	t.frame()
	payload := frameExitPayload(exit)
	for v := range t.viewers {
		v.send(frameExit, payload)
		v.mirror.Close()
		v.finish()
		delete(t.viewers, v)
	}
	t.mu.Unlock()
	if t.onEvent != nil {
		t.onEvent(t, "exit")
	}
	if t.onExit != nil {
		t.onExit(t, exit)
	}
}

// attachViewer adds a viewer at the size it fits, and queues the frames that
// start its terminal over: the size, what the screen does not carry, and the
// screen. Output after them comes as frames of its Mirror, so nothing is lost
// between the two. It returns why the program ended instead, when it did.
func (t *terminal) attachViewer(v *viewer, cols, rows int, theme *Theme) (*Exit, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.alive {
		if t.exit != nil {
			exit := *t.exit
			return &exit, nil
		}
		return &Exit{Error: "the program has ended"}, nil
	}
	if th := theme.terminalTheme(); th != nil {
		t.term.UseTheme(th)
	}
	t.resizeLocked(cols, rows, 0, 0)
	m, err := t.term.NewMirror()
	if err != nil {
		return nil, err
	}
	v.mirror = m
	first, _ := m.Frame()
	info := t.info()
	if st, ok := t.scan.Status(); ok {
		info.Status = &st
	}
	info.Links = t.links
	payload, err := jsonMarshal(info)
	if err != nil {
		m.Close()
		return nil, err
	}
	v.send(frameResized, sizePayload(t.cols, t.rows))
	v.send(frameInfo, payload)
	v.send(frameData, first)
	t.viewers[v] = true
	return nil, nil
}

// detach forgets a viewer.
func (t *terminal) detach(v *viewer) {
	t.mu.Lock()
	if t.viewers[v] {
		delete(t.viewers, v)
		v.mirror.Close()
	}
	t.mu.Unlock()
}

// readViewer applies what a window sends: input, its size and colors, and
// the user's clears. It returns when the viewer goes away.
func (t *terminal) readViewer(r *bufio.Reader) {
	for {
		kind, payload, err := readFrame(r)
		if err != nil {
			return
		}
		switch kind {
		case frameInput:
			_, _ = t.proc.Write(payload)
		case frameResize:
			if len(payload) >= 4 {
				cols := int(binary.LittleEndian.Uint16(payload[0:]))
				rows := int(binary.LittleEndian.Uint16(payload[2:]))
				cellW, cellH := 0, 0
				if len(payload) >= 8 {
					cellW = int(binary.LittleEndian.Uint16(payload[4:]))
					cellH = int(binary.LittleEndian.Uint16(payload[6:]))
				}
				t.resize(cols, rows, cellW, cellH)
			}
		case frameTheme:
			var theme Theme
			if jsonUnmarshal(payload, &theme) == nil {
				t.term.UseTheme(theme.terminalTheme())
			}
		case frameClear:
			t.clear()
		}
	}
}

// sizePayload is a size as frameResized carries it.
func sizePayload(cols, rows int) []byte {
	var payload [4]byte
	binary.LittleEndian.PutUint16(payload[0:], uint16(cols))
	binary.LittleEndian.PutUint16(payload[2:], uint16(rows))
	return payload[:]
}

// resize changes the terminal and the program's size together.
func (t *terminal) resize(cols, rows, cellW, cellH int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resizeLocked(cols, rows, cellW, cellH)
}

// resizeLocked takes a size a viewer fits. The viewers first get the screen
// at the old size, then the new size (frameResized), which their terminals
// take there too, then the screen at it; t.mu is held.
func (t *terminal) resizeLocked(cols, rows, cellW, cellH int) {
	if !t.alive || cols < 2 || rows < 2 {
		return
	}
	if cellW > 0 && cellH > 0 && (cellW != t.cellW || cellH != t.cellH) {
		// The cells' size in pixels: what a program asking for the size in
		// pixels gets.
		t.cellW, t.cellH = cellW, cellH
		t.term.ResizePixels(t.cols, t.rows, cellW, cellH)
	}
	if cols == t.cols && rows == t.rows {
		return
	}
	t.frame()
	t.cols, t.rows = cols, rows
	if t.cellW > 0 && t.cellH > 0 {
		t.term.ResizePixels(cols, rows, t.cellW, t.cellH)
	} else {
		t.term.Resize(cols, rows)
	}
	_ = t.proc.Resize(cols, rows)
	for v := range t.viewers {
		t.sendTo(v, frameResized, sizePayload(cols, rows))
	}
	t.frame()
}

// clear clears the terminal as a window's user asked: the scrollback and the
// screen above the cursor's line, but for a full-screen program's screen.
// The viewers see it with the next frame.
func (t *terminal) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.alive {
		return
	}
	if t.term.ClearScreen() {
		t.frameSoon()
	}
}

// kill hangs up the program. The read loop records the end.
func (t *terminal) kill() {
	t.mu.Lock()
	alive := t.alive
	t.mu.Unlock()
	if alive {
		_ = t.proc.Close()
	}
}

// send gives the program input, if it is still running.
func (t *terminal) send(data []byte) error {
	t.mu.Lock()
	alive := t.alive
	t.mu.Unlock()
	if !alive {
		return fmt.Errorf("terminal %s has ended", address(t.id))
	}
	if len(data) == 0 {
		return nil
	}
	_, err := t.proc.Write(data)
	return err
}

// read returns the last lines the terminal shows, and whether the program
// still runs. It keeps working after the program ended: the screen is still
// here.
func (t *terminal) read(lines int) ReadResult {
	if lines <= 0 {
		lines = 40
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := ReadResult{Terminal: t.id, Lines: tailLines(t.term.Text(), lines), Alive: t.alive}
	if t.exit != nil {
		exit := *t.exit
		out.Exit = &exit
	}
	return out
}

// tailLines is the last n lines of a terminal's text that hold something,
// up to the last of them, each without its trailing blanks.
func tailLines(text string, n int) []string {
	all := strings.Split(text, "\n")
	for i, line := range all {
		all[i] = strings.TrimRight(line, " ")
	}
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// record is the terminal as a client sees it.
func (t *terminal) record() Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := Record{
		ID: t.id, Kind: t.start.Kind, Name: t.start.Name, Parent: t.start.Parent,
		Argv: append([]string(nil), t.start.Argv...),
		Dir:  t.start.Dir, Cols: t.cols, Rows: t.rows, Alive: t.alive,
		Cwd: t.term.Dir(), Reported: t.scan.Reported(),
		Started: t.started.UnixMilli(), Labels: cloneLabels(t.start.Labels),
	}
	if r.Cwd == "" {
		r.Cwd = t.start.Dir
	}
	if t.start.Name != "" {
		r.Title = t.start.Name
	} else if title := t.term.Title(); title != "" {
		r.Title = title
	} else if len(t.start.Argv) > 0 {
		r.Title = filepath.Base(t.start.Argv[0])
	}
	if status, ok := t.scan.Status(); ok {
		r.App, r.Word, r.Message, r.Progress = status.App, status.State, status.Message, status.Progress
	}
	if t.proc != nil {
		if name, ok := t.proc.ForegroundName(); ok {
			r.Foreground = name
		}
	}
	if t.exit != nil {
		exit := *t.exit
		r.Exit = &exit
	}
	return r
}

// cloneLabels copies the labels a terminal was started with.
func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}
