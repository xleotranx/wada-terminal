package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
	"wada-terminal/internal/pty"
	"wada-terminal/internal/vt"

	"github.com/xleotranx/wada-kit/kernel"

	"github.com/egoist/mygo/ui"
)

// kind is the program a session runs: an agent's own TUI, or a shell.
type kind int

const (
	kindClaude kind = iota
	kindCodex
	kindOpenCode
	kindShell
)

// spec names a kind of session as the window shows it.
type spec struct {
	name       string   // the pane's title
	detail     string   // the second line, until the program names itself
	glyph      string   // the icon's mark
	candidates []string // the programs to look for, in order
}

func (k kind) spec() spec {
	switch k {
	case kindClaude:
		return spec{name: "Claude Code", detail: "Claude Code", glyph: "✳", candidates: []string{"claude"}}
	case kindCodex:
		return spec{name: "Codex", detail: "OpenAI Codex", glyph: "◆", candidates: []string{"codex"}}
	case kindOpenCode:
		return spec{name: "OpenCode", detail: "OpenCode", glyph: "◈", candidates: []string{"opencode"}}
	default:
		return spec{name: "Shell", detail: "Shell", glyph: "›", candidates: nil}
	}
}

// session is one program on a pseudo-terminal, with the screen it draws.
//
// The program's bytes come to the UI thread through push, which the reader
// worker calls; feed and everything else in a session belongs to the UI
// thread alone.
type session struct {
	t       *Plugin
	id      int
	kind    kind
	name    string // the pane's title, as the spec or the program named it
	detail  string
	dir     string
	machine string       // the SSH machine whose core runs it, empty on this one
	client  *core.Client // the core it attached to, local or remote

	// A custom agent from agents.json: its argv, its menu id and glyph, and
	// whether this session is one even before a 7501 report.
	agent      bool
	detected   bool
	agentID    string
	agentGlyph string
	agentColor string
	foreground string
	argv       []string

	scope  *kernel.Scope
	proc   *pty.Session
	screen *vt.Screen
	alive  bool

	// remote is a session whose program runs in the core: its screen is a
	// mirror of the core's, input goes over the attach stream, and closing
	// the window detaches rather than ends it. coreID is its address there.
	remote   bool
	coreID   int
	stream   *core.Stream
	streamMu sync.Mutex
	closing  atomic.Bool
	status   string // what the pane says instead of a screen, once the program stopped
	cols     int
	rows     int

	// The Sessions's view of an agent: busy while output or a key just
	// reached it, blocked while its screen asks for an approval, done when
	// it finished in a pane the user was not looking at, and asked while a
	// prompt of the user's has not been answered and seen.
	//
	// A program that speaks OSC 7501 says all of this itself. Then
	// reported is true and word is its own state; the fields above are
	// unused, so the protocol wins over anything its screen seems to say.
	reported     bool
	word         string // idle, working, done, blocked or error
	wordKind     string // permission, question or auth, for a blocked word
	wordProgress int    // 0 to 100, or -1 when the program sent none
	app          string // the program's own name for itself, from 7501
	statusTitle  string // the program's label for its work, from 7501
	statusMsg    string // one line saying what it is doing, from 7501
	seen         bool   // the user has looked since it stopped working
	busy         bool
	blocked      bool
	done         bool
	asked        bool
	lastWork     time.Time

	// inbox is the worker's way in: bytes appended under mu, with one post
	// to drain them while posted holds.
	mu      sync.Mutex
	inbox   []byte
	posted  bool
	body    ui.Handle
	started bool

	// fg and bg are the colors last reported to the program, so a frame
	// tells the screen only when the appearance changed.
	fg, bg [3]uint8

	// sel is what a drag picked in the pane, in the screen's viewport
	// coordinates: rows and columns of cells, not bytes.
	sel selection
	// mouseBtn is the button a mouse report holds, -1 when none, and
	// hoverLink whether the pointer rests on a hyperlink.
	mouseBtn int
	hoverURL string

	// scrollMode keeps typed keys for moving through the scrollback instead
	// of sending them to the program; viVisual selects whole lines.
	scrollMode  bool
	viVisual    bool
	viLinewise  bool
	viAnchor    int
	viCursor    int
	viAnchorCol int
	viCursorCol int
}

// start runs the program of kind k in dir on a new terminal of the given size.
func (t *Plugin) start(k kind, dir string, cols, rows int) *session {
	return t.startAt("", t.core, k, dir, cols, rows)
}

// newAgent starts a custom agent from agents.json in the shown workspace.
func (t *Plugin) newAgent(id string) {
	def, ok := t.agentDefs[id]
	if !ok || len(def.Command) == 0 {
		return
	}
	w := t.current
	if w == nil {
		return
	}
	cols, rows := t.defaultSize()
	s := t.startProgram(def, id, w.machine, w.client, w.dir, cols, rows)
	w.addTab(s, true)
	t.saveWorkspaces()
}

// startProgram starts an agent's own argv, local or on a machine.
func (t *Plugin) startProgram(def config.Agent, id, machine string, client *core.Client, dir string, cols, rows int) *session {
	s := &session{t: t, id: t.nextID, kind: kindShell, dir: dir, cols: cols, rows: rows,
		screen: vt.New(max(cols, 2), max(rows, 2)), alive: true, name: def.Name,
		detail: filepath.Base(def.Command[0]), agent: true, agentID: id,
		agentGlyph: def.Glyph, agentColor: def.Color, argv: append([]string(nil), def.Command...),
		machine: machine, client: client, mouseBtn: -1, seen: true, wordProgress: -1}
	t.nextID++
	s.reportColors(t.surface())
	if client != nil && t.startRemote(s, def.Command) {
		return s
	}
	if machine != "" {
		s.alive = false
		if client == nil {
			s.status = "Machine " + machine + " is not connected."
		} else {
			s.status = "Machine " + machine + " could not start this program."
		}
		return s
	}
	proc, err := pty.Start(pty.Options{Argv: def.Command, Env: terminalEnv(dir, t.dark()), Dir: dir,
		Cols: max(cols, 2), Rows: max(rows, 2)})
	if err != nil {
		s.alive = false
		s.status = err.Error()
		return s
	}
	s.proc = proc
	s.scope = t.ctx.NewScope()
	s.scope.OnDispose(func() { _ = proc.Close() })
	s.readLoop()
	return s
}

// startAt starts a program on the local core or on a machine's core. A machine
// that cannot be reached keeps the pane with its reason instead of quietly
// running the program on this computer.
func (t *Plugin) startAt(machine string, client *core.Client, k kind, dir string, cols, rows int) *session {
	sp := k.spec()
	s := &session{t: t, id: t.nextID, kind: k, dir: dir, cols: cols, rows: rows,
		screen: vt.New(max(cols, 2), max(rows, 2)), alive: true, name: sp.name, detail: sp.detail,
		mouseBtn: -1, seen: true, wordProgress: -1, machine: machine, client: client}
	t.nextID++

	var argv []string
	if machine != "" {
		argv = machineCommand(k, sp)
		if len(argv) > 0 {
			s.detail = filepath.Base(argv[0])
		}
	} else {
		argv = commandFor(sp)
	}
	if len(argv) == 0 {
		// The agent is not installed: the pane runs a shell instead, and
		// says so under its title.
		s.kind = kindShell
		s.name = "Shell"
		s.detail = sp.name + " isn't installed"
		if machine != "" {
			argv = []string{"/bin/sh", "-l"}
		} else {
			argv = shellCommand()
		}
	} else if s.detail == sp.detail {
		s.detail = filepath.Base(argv[0])
	}

	// A program asks for the terminal's colors as it starts, before the
	// first frame knows the appearance: tell the screen now, so the answer
	// it gets is the one the pane will paint.
	s.reportColors(t.surface())

	if client != nil && t.startRemote(s, argv) {
		return s
	}
	if machine != "" {
		s.alive = false
		if client == nil {
			s.status = "Machine " + machine + " is not connected."
		} else {
			s.status = "Machine " + machine + " could not start this program."
		}
		return s
	}
	proc, err := pty.Start(pty.Options{Argv: argv, Env: terminalEnv(dir, t.dark()), Dir: dir, Cols: max(cols, 2), Rows: max(rows, 2)})
	if err != nil {
		s.alive = false
		s.status = err.Error()
		return s
	}
	s.proc = proc
	s.scope = t.ctx.NewScope()
	s.scope.OnDispose(func() { _ = proc.Close() })
	s.readLoop()
	return s
}

// commandFor resolves the program of a spec, preferring the first candidate
// that is installed.
func commandFor(sp spec) []string {
	for _, name := range sp.candidates {
		if path, err := exec.LookPath(name); err == nil {
			return []string{path}
		}
	}
	if len(sp.candidates) == 0 {
		return shellCommand()
	}
	return nil
}

// shellCommand is the user's login shell.
func shellCommand() []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	if _, err := os.Stat(sh); err != nil {
		sh = "/bin/sh"
	}
	return []string{sh, "-l"}
}

// terminalEnv is the environment of every session: the app's own, with what a
// program reads to know it draws into a terminal, and whether that terminal is
// dark, which COLORFGBG spells as foreground and background indexes.
func terminalEnv(dir string, dark bool) []string {
	fg, bg := "15", "0"
	if !dark {
		fg, bg = "0", "15"
	}
	env := os.Environ()
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=wada-terminal",
		"COLORFGBG="+fg+";"+bg)
	if dir != "" {
		env = append(env, "PWD="+dir)
	}
	return env
}

// readLoop reads the program's output until it ends, and reports its end on
// the UI thread. It runs in a scope worker.
func (s *session) readLoop() {
	proc := s.proc
	s.scope.Spawn(func(ctx context.Context) {
		buf := make([]byte, 32*1024)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				s.push(buf[:n])
			}
			if err != nil {
				break
			}
			if ctx.Err() != nil {
				break
			}
		}
		err := proc.Wait()
		s.scope.Post(func() { s.t.closed(s, err) })
	})
}

// push appends what the program wrote and asks the UI thread to take it,
// once, however many reads come before it runs.
func (s *session) push(b []byte) {
	s.mu.Lock()
	s.inbox = append(s.inbox, b...)
	post := !s.posted
	if post {
		s.posted = true
	}
	s.mu.Unlock()
	if !post {
		return
	}
	s.scope.Post(func() {
		s.mu.Lock()
		b := s.inbox
		s.inbox = nil
		s.posted = false
		s.mu.Unlock()
		if len(b) > 0 {
			s.feed(b)
		}
	})
}

// feed applies what the program wrote to the screen, and answers what it
// asked — its attributes, the cursor's position, the colors — so a program
// that waits for a reply gets one.
func (s *session) feed(b []byte) {
	s.screen.Write(b)
	s.t.detectAgent(s)
	s.applyStatusReports()
	s.t.agentOutput(s)
	replies := s.screen.TakeResponses()
	if s.remote {
		return // the core answers the program's queries, once
	}
	for _, reply := range replies {
		s.write([]byte(reply))
	}
}

// reportColors tells the screen the colors of the surface it shows on, which
// it reports to a program that asks (OSC 10 and 11).
func (s *session) reportColors(sf surface) {
	fg, bg := [3]uint8{sf.fg.R, sf.fg.G, sf.fg.B}, [3]uint8{sf.bg.R, sf.bg.G, sf.bg.B}
	if fg == s.fg && bg == s.bg {
		return
	}
	s.fg, s.bg = fg, bg
	s.screen.SetColors(fg, bg)
}

// write gives the program input, as keys typed into the pane.
func (s *session) write(b []byte) {
	if len(b) == 0 {
		return
	}
	if s.remote {
		s.sendInput(b)
		return
	}
	if s.proc == nil || !s.alive {
		return
	}
	_, _ = s.proc.Write(b)
}

// resize keeps the screen and the program's terminal the same size.
func (s *session) resize(cols, rows int) {
	if cols < 2 || rows < 2 || (cols == s.cols && rows == s.rows) {
		return
	}
	s.cols, s.rows = cols, rows
	s.screen.Resize(cols, rows)
	if s.remote {
		s.sendResize(cols, rows)
		return
	}
	if s.proc != nil {
		// A failed resize leaves the program drawing at its old size, which
		// the screen crops; nothing else to do about it.
		_ = s.proc.Resize(cols, rows)
	}
}

// kill ends the program and its workers, and forgets the session.
func (s *session) kill() {
	if s.remote {
		s.closing.Store(true)
		s.closeStream()
		if s.client != nil && s.coreID > 0 {
			_ = s.client.Kill(s.coreID)
		}
		if s.scope != nil {
			s.scope.Dispose()
		}
		s.alive = false
		return
	}
	if s.scope != nil {
		s.scope.Dispose()
	}
	s.alive = false
}

// title is what the pane shows as its title: the session's own name, which
// stays put as the program renames its window. A program that reports over
// OSC 7501 can name its current work there, and names itself when the session
// started as a plain shell.
func (s *session) title() string {
	if s.reported {
		if s.statusTitle != "" {
			return s.statusTitle
		}
		if s.kind == kindShell && s.app != "" {
			return s.agentName()
		}
	}
	return s.name
}

// subtitle is the pane header's second line: the program's title while it runs
// (an agent names its model there), else its spec.
func (s *session) subtitle() string {
	if s.reported {
		if s.statusMsg != "" {
			return s.statusMsg
		}
		if s.app != "" && s.kind == kindShell {
			return "" // the title already carries the program's name
		}
	}
	name := strings.TrimSpace(s.screen.Title())
	if s.alive && name != "" && name != s.name {
		return name
	}
	return s.detail
}

// cwd is where the program says it is, or where it started.
func (s *session) cwd() string {
	if dir := s.screen.Cwd(); dir != "" {
		return dir
	}
	return s.dir
}

// state is what the pane's status dot reports: "running", "stopped" or "failed".
func (s *session) state() string {
	switch {
	case s.alive:
		return "running"
	case s.remote:
		return "stopped"
	case s.proc == nil:
		return "failed"
	default:
		return "stopped"
	}
}

// selection is the cells a drag picked, from its anchor to its focus.
type selection struct {
	on             bool
	dragging       bool
	ax, ay, fx, fy int
}

// begin starts a selection at a cell, or clears it when the press is a click.
func (s *session) begin(x, y int) {
	s.sel.on = true
	s.sel.dragging = true
	s.sel.ax, s.sel.ay = x, y
	s.sel.fx, s.sel.fy = x, y
}

// extend moves the focus of a drag to a cell.
func (s *session) extend(x, y int) {
	s.sel.fx, s.sel.fy = x, y
}

// finish ends a drag, keeping a selection only if it covers a cell other than
// the one pressed.
func (s *session) finish() {
	s.sel.dragging = false
	if s.sel.ax == s.sel.fx && s.sel.ay == s.sel.fy {
		s.sel.on = false
	}
}

// clearSelection drops what was selected, as typing does.
func (s *session) clearSelection() { s.sel = selection{} }

// range is the selection from its first cell to its last, row by row, and
// whether there is one.
func (s *session) selRange() (x0, y0, x1, y1 int, ok bool) {
	if !s.sel.on {
		return 0, 0, 0, 0, false
	}
	x0, y0, x1, y1 = s.sel.ax, s.sel.ay, s.sel.fx, s.sel.fy
	if y0 > y1 || (y0 == y1 && x0 > x1) {
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	cols, rows := s.screen.Size()
	x0, x1 = min(max(x0, 0), max(cols-1, 0)), min(max(x1, 0), max(cols-1, 0))
	y0, y1 = min(max(y0, 0), max(rows-1, 0)), min(max(y1, 0), max(rows-1, 0))
	return x0, y0, x1, y1, true
}

// selected reports whether a cell is inside the selection.
func (s *session) selected(x, y int) bool {
	x0, y0, x1, y1, ok := s.selRange()
	if !ok || y < y0 || y > y1 {
		return false
	}
	if y == y0 && x < x0 {
		return false
	}
	if y == y1 && x > x1 {
		return false
	}
	return true
}

// linkAt is the URL of the cell at x, y, empty when it links to nothing.
func (s *session) linkAt(x, y int) string {
	row := s.screen.Row(y)
	if x < 0 || x >= len(row) {
		return ""
	}
	return s.screen.LinkOf(row[x])
}

// wordRune reports whether a rune belongs to a word a double click selects:
// letters, digits, and the punctuation of paths, URLs and flags.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-./~:@%+=?&#$", r)
}

// selectWordAt selects the word around a cell, as a double click does.
func (s *session) selectWordAt(x, y int) {
	row := s.screen.Row(y)
	if y < 0 || y >= len(row) {
		return
	}
	x = min(max(x, 0), len(row)-1)
	at := func(i int) rune {
		for _, r := range row[i].Text {
			return r
		}
		return ' '
	}
	if !wordRune(at(x)) {
		s.sel = selection{on: true, ax: x, ay: y, fx: x, fy: y}
		return
	}
	start, end := x, x
	for start > 0 && wordRune(at(start-1)) {
		start--
	}
	for end < len(row)-1 && wordRune(at(end+1)) {
		end++
	}
	s.sel = selection{on: true, ax: start, ay: y, fx: end, fy: y}
}

// selectLineAt selects the line of a cell, as a triple click does.
func (s *session) selectLineAt(y int) {
	cols, rows := s.screen.Size()
	if y < 0 || y >= rows {
		return
	}
	s.sel = selection{on: true, ax: 0, ay: y, fx: cols - 1, fy: y}
}

// mouseReporting is how the program at the pane wants the mouse, and whether
// this event goes to it: Shift always selects text here instead.
func (s *session) mouseReporting(ev ui.InputEvent) (vt.MouseState, bool) {
	m := s.screen.Mouse()
	if m.Mode == 0 || ev.Mods&ui.Shift != 0 {
		return m, false
	}
	return m, true
}

// selectedText is what the selection holds, a line per row, its blanks at the
// end left out, as copying a screen gives text.
func (s *session) selectedText() string {
	x0, y0, x1, y1, ok := s.selRange()
	if !ok {
		return ""
	}
	var out strings.Builder
	for y := y0; y <= y1; y++ {
		row := s.screen.Row(y)
		start, end := 0, len(row)-1
		if y == y0 {
			start = x0
		}
		if y == y1 {
			end = min(x1, len(row)-1)
		}
		var line strings.Builder
		for x := start; x >= 0 && x <= end; x++ {
			cell := row[x]
			if cell.Width == 0 {
				continue // the second half of a wide character
			}
			if cell.Text == "" {
				line.WriteByte(' ')
			} else {
				line.WriteString(cell.Text)
			}
		}
		out.WriteString(strings.TrimRight(line.String(), " "))
		if y < y1 {
			out.WriteByte('\n')
		}
	}
	// A screen's last rows are usually empty; they do not end the copied
	// text with newlines nobody wrote.
	return strings.TrimRight(out.String(), "\n")
}

// selectAll selects the whole screen.
func (s *session) selectAll() {
	cols, rows := s.screen.Size()
	s.sel = selection{on: true, ax: 0, ay: 0, fx: cols - 1, fy: rows - 1}
}

// statusText is what the pane's header adds beside its title: how far the view
// is above the bottom of the scrollback.
func (s *session) statusText() string {
	if s.reported && s.wordProgress >= 0 && (s.word == "working" || s.word == "blocked") {
		return strconv.Itoa(s.wordProgress) + "%"
	}
	if n := s.screen.Scrolled(); n > 0 {
		return "↑ " + strconv.Itoa(n)
	}
	return ""
}

// machineCommand is the program a machine runs. A shell is the remote /bin/sh,
// not this computer's shell path; an agent is its command name so the remote
// PATH decides whether it is installed.
func machineCommand(k kind, sp spec) []string {
	if k == kindShell {
		return []string{"/bin/sh", "-l"}
	}
	if len(sp.candidates) > 0 {
		return []string{sp.candidates[0]}
	}
	return []string{"/bin/sh", "-l"}
}
