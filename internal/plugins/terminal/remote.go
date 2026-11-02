package terminal

import (
	"context"
	"errors"
	"os"
	"time"

	"wada-terminal/internal/core"
	"wada-terminal/internal/vt"
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
	fg, bg := surfaceRGB(t.surface())
	rec, err := s.client.Start(core.Start{
		Argv: argv, Kind: s.kind.apiName(), Dir: s.dir,
		Cols: max(s.cols, 2), Rows: max(s.rows, 2),
		Dark: t.dark(), FgRGB: fg, BgRGB: bg,
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

// attachRemote joins a session to a terminal the core already runs. It copies
// the screen the program last drew, then streams what it writes next.
func (t *Plugin) attachRemote(s *session, id int) bool {
	if s == nil || s.client == nil {
		return false
	}
	fg, bg := surfaceRGB(t.surface())
	stream, snap, err := s.client.Attach(id, max(s.cols, 2), max(s.rows, 2), fg, bg)
	if err != nil {
		s.alive = false
		s.status = "The core could not attach: " + err.Error()
		return false
	}
	if snap.Cols >= 2 && snap.Rows >= 2 {
		s.cols, s.rows = snap.Cols, snap.Rows
	}
	s.screen = vt.New(max(s.cols, 2), max(s.rows, 2))
	s.screen.Restore(snap)
	s.screen.SetColors(fg, bg)
	s.remote, s.coreID = true, id
	s.setStream(stream)
	s.restoreStatus()
	s.scope = t.ctx.NewScope()
	s.scope.OnDispose(func() { s.closeStream() })
	s.readCore()
	return true
}

// restoreStatus takes the program's OSC 7501 record from a restored screen,
// so a pane that was blocked or working before the window reopened still says
// so. A window opening on an agent has seen it, so a finish shows as idle
// rather than Done.
func (s *session) restoreStatus() {
	s.reported = s.screen.Reported()
	s.busy, s.blocked, s.done, s.asked = false, false, false, false
	s.word, s.wordKind, s.wordProgress = "", "", -1
	s.app, s.statusTitle, s.statusMsg = "", "", ""
	if st, ok := s.screen.Status(); ok {
		s.word, s.wordKind, s.wordProgress = st.State, st.Kind, st.Progress
		s.app, s.statusTitle, s.statusMsg = st.App, st.Title, st.Message
	}
	s.seen = true
}

// readCore streams the terminal's output on a scope worker. If the connection
// breaks while the program still runs, it attaches again and replaces the
// screen; the core kept everything that happened in between.
func (s *session) readCore() {
	s.scope.Spawn(func(ctx context.Context) {
		for {
			stream := s.currentStream()
			if stream == nil {
				return
			}
			data, exit, err := stream.Next()
			if err != nil {
				if s.closing.Load() || ctx.Err() != nil {
					return
				}
				if s.reattach(ctx) {
					continue
				}
				if ctx.Err() == nil {
					s.scope.Post(func() { s.t.closed(s, err) })
				}
				return
			}
			if len(data) > 0 {
				s.push(data)
			}
			if exit != nil {
				var ended error
				if exit.Error != "" {
					ended = errors.New(exit.Error)
				}
				s.scope.Post(func() { s.t.closed(s, ended) })
				return
			}
		}
	})
}

// reattach tries to join the same core terminal again a few times. It hands
// the fresh screen to the UI thread and waits until it is in place, so the
// next bytes are applied to the right screen.
func (s *session) reattach(ctx context.Context) bool {
	if s.t == nil || s.client == nil || s.coreID == 0 {
		return false
	}
	fg, bg := surfaceRGB(s.t.surface())
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
		stream, snap, err := s.client.Attach(s.coreID, max(s.cols, 2), max(s.rows, 2), fg, bg)
		if err != nil {
			continue
		}
		done := make(chan struct{})
		s.scope.Post(func() {
			s.screen.Restore(snap)
			s.screen.SetColors(fg, bg)
			if snap.Cols >= 2 && snap.Rows >= 2 {
				s.cols, s.rows = snap.Cols, snap.Rows
			}
			s.setStream(stream)
			s.restoreStatus()
			s.t.ctx.Invalidate()
			close(done)
		})
		select {
		case <-done:
			return true
		case <-ctx.Done():
			_ = stream.Close()
			return false
		}
	}
	return false
}

// setStream replaces the attach stream this session reads from and writes to.
func (s *session) setStream(stream *core.Stream) {
	s.streamMu.Lock()
	s.stream = stream
	s.streamMu.Unlock()
}

// currentStream is the stream the session is using now.
func (s *session) currentStream() *core.Stream {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	return s.stream
}

// closeStream detaches this pane without ending the program.
func (s *session) closeStream() {
	s.streamMu.Lock()
	stream := s.stream
	s.stream = nil
	s.streamMu.Unlock()
	if stream != nil {
		_ = stream.Close()
	}
}

// sendInput gives the program what the user typed.
func (s *session) sendInput(b []byte) {
	stream := s.currentStream()
	if stream == nil {
		return
	}
	_ = stream.Input(b)
}

// sendResize tells the core and its program the pane's new size.
func (s *session) sendResize(cols, rows int) {
	stream := s.currentStream()
	if stream == nil {
		return
	}
	_ = stream.Resize(cols, rows)
}

// surfaceRGB is the terminal's foreground and background as the three bytes
// the core reports to a program that asks for its colors.
func surfaceRGB(sf surface) ([3]uint8, [3]uint8) {
	return [3]uint8{sf.fg.R, sf.fg.G, sf.fg.B}, [3]uint8{sf.bg.R, sf.bg.G, sf.bg.B}
}
