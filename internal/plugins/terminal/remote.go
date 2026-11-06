package terminal

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

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

// startRemote starts and attaches in a session worker. The pane is returned
// immediately; only the UI-thread completion installs its terminal.
func (t *Plugin) startRemote(s *session, argv []string) bool {
	if s == nil || s.client == nil || !s.alive || len(argv) == 0 {
		return false
	}
	request := core.Start{Argv: append([]string(nil), argv...), Kind: s.kind.apiName(), Dir: s.dir,
		Cols: max(s.cols, 2), Rows: max(s.rows, 2), Dark: t.dark(), Theme: core.ThemeOf(t.terminalTheme())}
	client := s.client
	s.scope = t.ctx.NewScope()
	scope := s.scope
	var installed atomic.Bool
	scope.Spawn(func(ctx context.Context) {
		rec, err := client.Start(request)
		var stream *core.Stream
		if err == nil {
			stream, err = client.Attach(rec.ID, request.Cols, request.Rows, request.Theme)
		}
		if err != nil && rec.ID > 0 {
			_ = client.Kill(rec.ID)
		}
		if ctx.Err() != nil {
			if stream != nil {
				_ = stream.Close()
			}
			if (s.closing.Load() || !installed.Load()) && rec.ID > 0 {
				_ = client.Kill(rec.ID)
			}
			return
		}
		// Post through the plugin so a pane closed before delivery still releases
		// its stream. Explicit pane closure also ends the newly started program.
		t.ctx.Post(func() {
			if s.closing.Load() || !scope.Alive() {
				t.ctx.Spawn(func(context.Context) {
					if stream != nil {
						_ = stream.Close()
					}
					if s.closing.Load() {
						_ = client.Kill(rec.ID)
					}
				})
				return
			}
			if err != nil {
				if s.machine == "" {
					scope.Dispose()
					t.startLocal(s, request.Argv)
				} else {
					s.alive = false
					s.status = "The core could not start this program: " + err.Error()
				}
				return
			}
			t.installRemote(s, rec.ID, stream)
			t.saveWorkspaces()
			installed.Store(s.term != nil)
		})
		<-ctx.Done()
		if stream != nil {
			_ = stream.Close()
		}
		if (s.closing.Load() || !installed.Load()) && rec.ID > 0 {
			_ = client.Kill(rec.ID)
		}
	})
	return true
}

// attachRemote schedules an attachment without waiting for the core on the UI.
func (t *Plugin) attachRemote(s *session, id int) bool {
	if s == nil || s.client == nil {
		return false
	}
	s.scope = t.ctx.NewScope()
	scope, client := s.scope, s.client
	cols, rows, theme := max(s.cols, 2), max(s.rows, 2), core.ThemeOf(t.terminalTheme())
	s.remote, s.coreID = true, id
	scope.Spawn(func(ctx context.Context) {
		stream, err := client.Attach(id, cols, rows, theme)
		if ctx.Err() != nil {
			if stream != nil {
				_ = stream.Close()
			}
			return
		}
		t.ctx.Post(func() {
			if !scope.Alive() || s.closing.Load() {
				if stream != nil {
					t.ctx.Spawn(func(context.Context) { _ = stream.Close() })
				}
				return
			}
			if err != nil {
				s.alive = false
				s.status = "The core could not attach: " + err.Error()
				return
			}
			t.installRemote(s, id, stream)
		})
		<-ctx.Done()
		if stream != nil {
			_ = stream.Close()
		}
	})
	return true
}

// installRemote is the UI-thread half of an attachment.
func (t *Plugin) installRemote(s *session, id int, stream *core.Stream) {
	s.remote, s.coreID, s.stream = true, id, stream
	s.cols, s.rows = stream.Cols, stream.Rows
	gate := &gated{Stream: stream, ready: make(chan struct{})}
	opts := t.terminalOptions(s, gate)
	opts.ResizeByConn = true
	term, err := mterm.New(opts)
	if err != nil {
		close(gate.ready)
		t.ctx.Spawn(func(context.Context) { _ = stream.Close() })
		s.alive = false
		s.status = fmt.Sprintf("The terminal could not start: %v", err)
		return
	}
	term.Resize(stream.Cols, stream.Rows)
	stream.OnResized = func(cols, rows int) { term.Resize(cols, rows); s.scope.Post(func() { s.cols, s.rows = cols, rows }) }
	stream.OnInfo = func(info core.Info) { s.scope.Post(func() { s.applyInfo(info) }) }
	stream.OnOutput = s.outputSoon
	s.term = term
	// Closing a terminal can wait for transport writes; cleanup belongs to a worker.
	s.scope.Spawn(func(ctx context.Context) { <-ctx.Done(); _ = term.Close(); _ = stream.Close() })
	close(gate.ready)
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
