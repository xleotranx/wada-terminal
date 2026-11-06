package terminal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
	"wada-terminal/internal/pty"
	"wada-terminal/internal/status"

	"github.com/xleotranx/wada-kit/kernel"

	mterm "github.com/egoist/mygo/plugins/terminal"
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

// session is one program on a pseudo-terminal, and the terminal that shows it:
// MyGo's terminal view, which draws, selects, scrolls, searches and takes the
// keyboard. Its screen is the core's (remote), shown as frames of the core's
// terminal, or else the window's own, reading the program.
//
// Everything in a session belongs to the UI thread; the terminal's callbacks
// and the readers reach it through its scope's posts.
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

	scope *kernel.Scope
	term  *mterm.Terminal
	proc  *pty.Session // the program, when the window runs it rather than the core
	alive bool

	// remote is a session whose program runs in the core: its terminal shows
	// the core's frames, input goes over the attach stream, and closing the
	// window detaches rather than ends it. coreID is its address there.
	remote  bool
	coreID  int
	stream  *core.Stream
	closing atomic.Bool
	status  string // what the pane says instead of a screen, once the program stopped
	exitErr error  // why a program the window ran ended, from its reader
	cols    int
	rows    int

	// progTitle and progCwd are what a remote program set (OSC 0, 2 and 7),
	// which the core tells; a local terminal is asked directly.
	progTitle string
	progCwd   string
	// bracketed tells that the program asked for bracketed paste; links are
	// the targets of the hyperlinks it printed (OSC 8), newest last.
	bracketed bool
	links     []string
	// outputPosted holds while word of output is on its way to the UI
	// thread, so a flood costs one post at a time.
	outputPosted atomic.Bool

	// The agent state the window shows: busy while output or a key just
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
	if w == nil || (w.task != nil && w.task.Dir == "") {
		return
	}
	cols, rows := t.defaultSize()
	s := t.startProgram(def, id, w.machine, w.client, w.dir, cols, rows)
	w.addTab(s, true)
	t.saveWorkspaces()
}

// startProgram starts an agent's own argv, local or on a machine.
func (t *Plugin) startProgram(def config.Agent, id, machine string, client *core.Client, dir string, cols, rows int) *session {
	detail := ""
	if len(def.Command) > 0 {
		detail = filepath.Base(def.Command[0])
	}
	s := &session{t: t, id: t.nextID, kind: kindShell, dir: dir, cols: cols, rows: rows,
		alive: true, name: def.Name,
		detail: detail, agent: true, agentID: id,
		agentGlyph: def.Glyph, agentColor: def.Color, argv: append([]string(nil), def.Command...),
		machine: machine, client: client, seen: true, wordProgress: -1}
	t.nextID++
	if len(def.Command) == 0 {
		s.alive = false
		s.status = "No command is configured for this agent."
		return s
	}
	if client != nil && t.startRemote(s, def.Command) {
		return s
	}
	if machine != "" {
		s.alive = false
		if client == nil {
			s.status = "Machine " + machine + " is connecting."
			t.waitMachineProgram(s, s.argv)
		} else {
			s.status = "Machine " + machine + " could not start this program."
		}
		return s
	}
	t.startLocal(s, def.Command)
	return s
}

// startAt starts a program on the local core or on a machine's core. A machine
// that cannot be reached keeps the pane with its reason instead of quietly
// running the program on this computer.
func (t *Plugin) startAt(machine string, client *core.Client, k kind, dir string, cols, rows int) *session {
	sp := k.spec()
	s := &session{t: t, id: t.nextID, kind: k, dir: dir, cols: cols, rows: rows,
		alive: true, name: sp.name, detail: sp.detail,
		seen: true, wordProgress: -1, machine: machine, client: client}
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

	s.argv = append([]string(nil), argv...)
	if client != nil && t.startRemote(s, argv) {
		return s
	}
	if machine != "" {
		s.alive = false
		if client == nil {
			s.status = "Machine " + machine + " is connecting."
			t.waitMachineProgram(s, s.argv)
		} else {
			s.status = "Machine " + machine + " could not start this program."
		}
		return s
	}
	t.startLocal(s, argv)
	return s
}

// startLocal runs the program on a pseudo-terminal of the window's own, which
// the session's terminal reads: the window has no core, and the program ends
// with it.
func (t *Plugin) startLocal(s *session, argv []string) {
	proc, err := pty.Start(pty.Options{Argv: argv, Env: terminalEnv(s.dir, t.host.DataDir(), t.dark()), Dir: s.dir,
		Cols: max(s.cols, 2), Rows: max(s.rows, 2)})
	if err != nil {
		s.alive = false
		s.status = err.Error()
		return
	}
	s.proc = proc
	s.scope = t.ctx.NewScope()
	conn := &localConn{s: s, proc: proc}
	term, err := mterm.New(t.terminalOptions(s, conn))
	if err != nil {
		_ = proc.Close()
		s.scope.Dispose()
		s.alive = false
		s.status = "The terminal could not start: " + err.Error()
		return
	}
	term.Resize(max(s.cols, 2), max(s.rows, 2))
	s.term = term
	s.scope.OnDispose(func() {
		_ = term.Close()
		_ = proc.Close()
	})
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
func terminalEnv(dir, dataDir string, dark bool) []string {
	fg, bg := "15", "0"
	if !dark {
		fg, bg = "0", "15"
	}
	env := appdir.TerminalEnv(os.Environ(), dataDir)
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=wada-terminal",
		"COLORFGBG="+fg+";"+bg)
	if dir != "" {
		env = append(env, "PWD="+dir)
	}
	return env
}

// maxLinks is how many hyperlink targets a session keeps for the link picker.
const maxLinks = 200

// localConn is the Conn of a terminal whose program the window runs: what
// the program writes goes to the terminal, after the status reports and the
// bracketed paste mode are read out of it, which the emulator does not
// report. Its Read runs on the terminal's goroutine.
type localConn struct {
	s    *session
	proc *pty.Session
	scan status.Stream // the reader's alone
	mode bool          // bracketed paste as last posted
}

func (c *localConn) Read(p []byte) (int, error) {
	n, err := c.proc.Read(p)
	if n > 0 {
		c.scan.Write(p[:n])
		for _, reply := range c.scan.TakeReplies() {
			_, _ = c.proc.Write([]byte(reply))
		}
		reports, links, bracketed := c.scan.TakeReports(), c.scan.TakeLinks(), c.scan.Bracketed()
		if len(reports) > 0 || len(links) > 0 || bracketed != c.mode {
			c.mode = bracketed
			s := c.s
			s.scope.Post(func() {
				s.bracketed = bracketed
				s.links = status.AppendLinks(s.links, links, maxLinks)
				s.applyStatusReports(reports)
				s.t.ctx.Invalidate()
			})
		}
		c.s.outputSoon()
	}
	if err != nil {
		// The program closed its terminal: why it ended is the session's,
		// before the terminal says it did.
		waitErr := c.proc.Wait()
		s := c.s
		s.scope.Post(func() { s.exitErr = waitErr })
		return n, io.EOF
	}
	return n, nil
}

func (c *localConn) Write(p []byte) (int, error) { return c.proc.Write(p) }

// Resize gives the program the size the pane fits.
func (c *localConn) Resize(cols, rows int) error {
	s := c.s
	s.scope.Post(func() { s.cols, s.rows = cols, rows })
	return c.proc.Resize(cols, rows)
}

func (c *localConn) Close() error { return c.proc.Close() }

// terminalOptions is how a session's terminal looks and what it tells the
// session: the window's font and colors, and its program's title, output,
// keys and end.
func (t *Plugin) terminalOptions(s *session, conn io.ReadWriteCloser) mterm.Options {
	light, dark := t.terminalThemes()
	return mterm.Options{
		Conn:  conn,
		Font:  t.terminalFont(),
		Theme: light, DarkTheme: dark,
		Cursor:  mterm.CursorBar,
		Padding: mterm.Padding{X: bodyPadX, Y: bodyPadY},
		OnTitle: func(string) {
			s.scope.Post(func() {
				t.detectAgent(s)
				t.ctx.Invalidate()
			})
		},
		OnExit: func(int) { s.scope.Post(s.ended) },
		// A key typed into an agent's pane is what gives it work to finish.
		OnKey: func(ui.Modifiers, ui.Key) bool {
			t.prompted(s)
			return false
		},
		// The window's own keys and its View menu zoom every pane together.
		OnZoom: func(z mterm.Zoom) bool {
			if z.Reset {
				t.setFont(defaultFontSize)
			} else {
				t.zoomFont(z.Steps)
			}
			return true
		},
		OnSearch: func(st mterm.SearchStatus) {
			if t.search.session == s {
				t.search.status = st
				t.ctx.Invalidate()
			}
		},
		// Clearing a core's terminal here alone would leave its next
		// frames drawn from a screen still full: the core clears.
		OnClear: func(mterm.Clear) bool {
			if s.remote && s.stream != nil {
				_ = s.stream.Clear()
				return true
			}
			return false
		},
	}
}

// outputSoon tells the UI thread that the program drew, once per post: an
// agent that draws is working until it is quiet. It runs on a reader.
func (s *session) outputSoon() {
	if !s.outputPosted.CompareAndSwap(false, true) {
		return
	}
	s.scope.PostQuiet(func() {
		s.outputPosted.Store(false)
		s.t.agentOutput(s)
	})
}

// ended records that the terminal's program ended, or that its stream to
// the core did; nothing when the window closed it.
func (s *session) ended() {
	if !s.alive || s.closing.Load() {
		return
	}
	if s.remote {
		exit := s.stream.Exit()
		if exit == nil {
			return // closed here
		}
		var err error
		if exit.Error != "" {
			err = errors.New(exit.Error)
		}
		s.t.closed(s, err)
		return
	}
	s.t.closed(s, s.exitErr)
}

// write gives the program input, as keys typed into the pane.
func (s *session) write(b []byte) {
	if len(b) == 0 || !s.writable() {
		return
	}
	s.term.Send(b)
}

// writable reports whether a program is there to take input: its terminal has
// started and the program has not ended.
func (s *session) writable() bool { return s.term != nil && s.alive }

// kill ends the program and its terminal, and forgets the session.
func (s *session) kill() {
	s.closing.Store(true)
	if s.remote && s.client != nil && s.coreID > 0 {
		client, id := s.client, s.coreID
		s.t.ctx.Spawn(func(context.Context) { _ = client.Kill(id) })
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
	name := strings.TrimSpace(s.programTitle())
	if s.alive && name != "" && name != s.name {
		return name
	}
	return s.detail
}

// cwd is where the program says it is, or where it started.
func (s *session) cwd() string {
	if dir := s.programCwd(); dir != "" {
		return dir
	}
	return s.dir
}

// programTitle is the title the program set, if any.
func (s *session) programTitle() string {
	if s.remote || s.term == nil {
		return s.progTitle
	}
	return s.term.Title()
}

// programCwd is the directory the program reported, if any.
func (s *session) programCwd() string {
	if s.remote || s.term == nil {
		return s.progCwd
	}
	return s.term.Dir()
}

// screenText is what the terminal shows, with its scrollback, a line per
// row; empty before the terminal exists.
func (s *session) screenText() string {
	if s.term == nil {
		return ""
	}
	return s.term.Text()
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

// statusText is what the pane's header adds beside its title: how far a
// reporting program says its work is.
func (s *session) statusText() string {
	if s.reported && s.wordProgress >= 0 && (s.word == "working" || s.word == "blocked") {
		return strconv.Itoa(s.wordProgress) + "%"
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

// waitMachineProgram keeps a pane as a placeholder while SSH connects.
func (t *Plugin) waitMachineProgram(s *session, argv []string) {
	s.scope = t.ctx.NewScope()
	scope := s.scope
	argv = append([]string(nil), argv...)
	t.whenMachine(s.machine, func(client *core.Client, err error) {
		if !scope.Alive() || s.closing.Load() {
			return
		}
		if err != nil {
			s.status = err.Error()
			return
		}
		scope.Dispose()
		s.client = client
		s.alive = true
		s.status = ""
		for _, w := range t.workspaces {
			if w.machine == s.machine {
				w.client = client
			}
		}
		t.startRemote(s, argv)
	})
}
