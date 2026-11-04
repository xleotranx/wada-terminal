package terminal

import (
	"os"

	"wada-terminal/internal/core"
	"wada-terminal/internal/status"

	mterm "github.com/egoist/mygo/plugins/terminal"
)

// connectCore finds the persistent terminal server, if one is running on this
// app's data folder. Without one the window opens as it did before the core:
// panes belong to it and end with it. With one, a pane attaches to a program
// the core keeps, and closing the window leaves it running.
func (t *Plugin) connectCore() {
	if os.Getenv("WADATERM_CORE") == "off" {
		return
	}
	dir := t.host.DataDir()
	if dir == "" {
		return
	}
	client, err := core.Connect(dir)
	if err != nil {
		return
	}
	list, err := client.List()
	if err != nil {
		return
	}
	t.core = client
	t.coreTerminals = make(map[int]core.Record, len(list.Terminals))
	for _, rec := range list.Terminals {
		t.coreTerminals[rec.ID] = rec
	}
}

// startRemote asks the core to run the program and attaches this pane to it.
// It reports false when the core cannot start or attach, so start can fall
// back to a terminal owned by the window rather than losing the pane.
func (t *Plugin) startRemote(s *session, argv []string) bool {
	if s == nil || s.client == nil || !s.alive || len(argv) == 0 {
		return false
	}
	rec, err := s.client.Start(core.Start{
		Argv: argv, Kind: s.kind.apiName(), Dir: s.dir,
		Cols: max(s.cols, 2), Rows: max(s.rows, 2),
		Dark: t.dark(), Theme: core.ThemeOf(t.terminalTheme()),
	})
	if err != nil {
		return false
	}
	if !t.attachRemote(s, rec.ID) {
		_ = s.client.Kill(rec.ID)
		s.alive = true
		s.status = ""
		return false
	}
	return true
}

// attachRemote joins a session to a terminal the core already runs. The
// session's terminal shows the core's frames: the first starts it over from
// the screen as it is, and it takes the sizes the core's terminal takes.
func (t *Plugin) attachRemote(s *session, id int) bool {
	if s == nil || s.client == nil {
		return false
	}
	stream, err := s.client.Attach(id, max(s.cols, 2), max(s.rows, 2), core.ThemeOf(t.terminalTheme()))
	if err != nil {
		s.alive = false
		s.status = "The core could not attach: " + err.Error()
		return false
	}
	s.scope = t.ctx.NewScope()
	s.remote, s.coreID, s.stream = true, id, stream
	s.cols, s.rows = stream.Cols, stream.Rows
	gate := &gated{Stream: stream, ready: make(chan struct{})}
	opts := t.terminalOptions(s, gate)
	// The core's terminal is the program's; this one lays out none of its
	// output and takes the size the core's does, between the frames of one
	// size and the next.
	opts.ResizeByConn = true
	term, err := mterm.New(opts)
	if err != nil {
		stream.Close()
		s.scope.Dispose()
		s.alive = false
		s.status = "The terminal could not start: " + err.Error()
		return false
	}
	term.Resize(stream.Cols, stream.Rows)
	stream.OnResized = func(cols, rows int) {
		term.Resize(cols, rows)
		s.scope.Post(func() { s.cols, s.rows = cols, rows })
	}
	stream.OnInfo = func(info core.Info) { s.scope.Post(func() { s.applyInfo(info) }) }
	stream.OnOutput = s.outputSoon
	s.term = term
	s.scope.OnDispose(func() {
		_ = term.Close()
		_ = stream.Close()
	})
	close(gate.ready)
	return true
}

// gated holds the stream's frames back until the session's terminal and the
// stream's callbacks are in place.
type gated struct {
	*core.Stream
	ready chan struct{}
}

func (g *gated) Read(p []byte) (int, error) {
	<-g.ready
	return g.Stream.Read(p)
}

// applyInfo takes what the core says beside the screen: the program's title
// and directory, its status reports, its bracketed paste mode and its
// hyperlinks. The first
// Info of an attachment carries the program's record as it stands, so a
// pane that was blocked or working before the window reopened still says so,
// and a window opening on an agent has seen it, so a finish shows as idle
// rather than Done.
func (s *session) applyInfo(info core.Info) {
	titled := info.Title != s.progTitle
	s.progTitle, s.progCwd, s.bracketed = info.Title, info.Cwd, info.Bracketed
	if info.Status != nil || (info.Reported && !s.reported && len(info.Reports) == 0) {
		s.reported = info.Reported
		s.busy, s.blocked, s.done, s.asked = false, false, false, false
		s.word, s.wordKind, s.wordProgress = "", "", -1
		s.app, s.statusTitle, s.statusMsg = "", "", ""
		if st := info.Status; st != nil {
			s.word, s.wordKind, s.wordProgress = st.State, st.Kind, st.Progress
			s.app, s.statusTitle, s.statusMsg = st.App, st.Title, st.Message
		}
		s.seen = true
	}
	s.applyStatusReports(info.Reports)
	s.links = status.AppendLinks(s.links, info.Links, maxLinks)
	if titled {
		s.t.detectAgent(s)
	}
	s.t.ctx.Invalidate()
}
