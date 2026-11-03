package core

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/pty"
	"wada-terminal/internal/vt"
)

// viewer is one attached window. The terminal sends output on ch; closing
// done tells the writer to stop. exit is set only when the program itself
// ended, so a viewer that falls behind closes without pretending the program
// did.
type viewer struct {
	conn net.Conn
	wake chan struct{}
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	exit *Exit
	// pending is output the program wrote that no frame has carried yet;
	// lastWrite is when the writer last sent one, which tells it whether the
	// program is streaming and its reads may be gathered.
	pending   []byte
	lastWrite time.Time
}

// push queues what the program wrote for this viewer and wakes the writer. It
// reports false when the viewer is too far behind: one that stopped reading
// must not hold the program back, and can attach again for a fresh screen.
func (v *viewer) push(b []byte) bool {
	v.mu.Lock()
	if len(v.pending)+len(b) > maxPending {
		v.mu.Unlock()
		return false
	}
	v.pending = append(v.pending, b...)
	v.mu.Unlock()
	select {
	case v.wake <- struct{}{}:
	default:
	}
	return true
}

// take gives the writer what is pending and collects the next read in spare,
// so a stream reuses its buffers instead of allocating a frame each time.
func (v *viewer) take(spare []byte) []byte {
	v.mu.Lock()
	batch := v.pending
	v.pending = spare
	v.mu.Unlock()
	return batch
}

// close ends the viewer's stream, once.
func (v *viewer) close() {
	v.once.Do(func() { close(v.done) })
}

// setExit records why the program ended before closing the viewer.
func (v *viewer) setExit(exit Exit) {
	v.mu.Lock()
	v.exit = &exit
	v.mu.Unlock()
	v.close()
}

// terminal is one program on a pseudo-terminal, its screen, and the viewers
// watching it. It belongs to the core and outlives any window.
type terminal struct {
	id      int
	start   Start
	proc    *pty.Session
	screen  *vt.Screen
	started time.Time
	onExit  func(*terminal, Exit)   // called once, from the read loop
	onEvent func(*terminal, string) // created, changed or exit

	mu      sync.Mutex
	alive   bool
	exit    *Exit
	viewers map[*viewer]bool
	cols    int
	rows    int
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
	screen := vt.New(start.Cols, start.Rows)
	screen.SetColors(start.FgRGB, start.BgRGB)
	proc, err := pty.Start(pty.Options{
		Argv: start.Argv, Env: terminalEnv(start.Dir, dataDir, start.Dark, id, start.Parent), Dir: start.Dir,
		Cols: start.Cols, Rows: start.Rows,
	})
	if err != nil {
		return nil, err
	}
	t := &terminal{
		id: id, start: start, proc: proc, screen: screen, started: time.Now(),
		alive: true, viewers: map[*viewer]bool{}, cols: start.Cols, rows: start.Rows,
		onExit: onExit, onEvent: onEvent,
	}
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

// run reads the program's output until it ends. What it writes goes to the
// screen and to every viewer; what it asks is answered here, once, so a
// program never waits on a window that is closed.
func (t *terminal) run() {
	buf := make([]byte, 32*1024)
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

// apply gives the program's bytes to the screen and every viewer, and answers
// the queries it asked.
func (t *terminal) apply(b []byte) {
	t.mu.Lock()
	if !t.alive {
		t.mu.Unlock()
		return
	}
	beforeStatus, beforeOK := t.screen.Status()
	beforeTitle, beforeCwd := t.screen.Title(), t.screen.Cwd()
	t.screen.Write(b)
	afterStatus, afterOK := t.screen.Status()
	changed := beforeOK != afterOK || beforeStatus != afterStatus ||
		beforeTitle != t.screen.Title() || beforeCwd != t.screen.Cwd()
	replies := t.screen.TakeResponses()
	for v := range t.viewers {
		if v.push(b) {
			continue
		}
		// A viewer that stopped reading must not hold the program back. It
		// can attach again and get a fresh screen.
		delete(t.viewers, v)
		v.close()
	}
	t.mu.Unlock()
	if len(replies) > 0 {
		_, _ = t.proc.Write([]byte(strings.Join(replies, "")))
	}
	if changed && t.onEvent != nil {
		t.onEvent(t, "changed")
	}
}

// finish records why the program ended and releases every viewer.
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
	for v := range t.viewers {
		v.setExit(exit)
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

// attachViewer adds a viewer and returns the screen at that moment. Output
// that comes after the snapshot is queued for the viewer, so nothing is lost
// between the two.
func (t *terminal) attachViewer(v *viewer) (vt.Snapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.alive {
		if t.exit != nil {
			v.setExit(*t.exit)
		} else {
			v.setExit(Exit{Error: "the program has ended"})
		}
		return vt.Snapshot{}, false
	}
	t.viewers[v] = true
	return t.screen.Snapshot(), true
}

// detach forgets a viewer.
func (t *terminal) detach(v *viewer) {
	t.mu.Lock()
	delete(t.viewers, v)
	t.mu.Unlock()
}

// readViewer applies what a window sends: input and resizes. It returns when
// the viewer goes away.
func (t *terminal) readViewer(v *viewer, r *bufio.Reader) {
	for {
		kind, payload, err := readFrame(r)
		if err != nil {
			return
		}
		switch kind {
		case frameInput:
			_, _ = t.proc.Write(payload)
		case frameResize:
			if len(payload) == 4 {
				cols := int(uint16(payload[0]) | uint16(payload[1])<<8)
				rows := int(uint16(payload[2]) | uint16(payload[3])<<8)
				t.resize(cols, rows)
			}
		}
	}
}

// frameGap is how long a viewer's writer gathers output before sending it as
// one frame while a program streams: a flood then costs the window a frame
// every few milliseconds rather than one per read, which is most of both
// processes' work under output. Output that follows a pause goes at once, so
// a prompt still appears as soon as it is written.
const frameGap = 4 * time.Millisecond

// frameFlow is how recently output must have been written for a program to
// count as streaming.
const frameFlow = 25 * time.Millisecond

// maxPending bounds the output a viewer may be behind by before it is dropped:
// its writer reuses the buffer a frame was sent from, so a stream costs no
// allocation, but one that stops reading must not grow the core's memory.
const maxPending = 4 << 20

// writeViewer sends queued output until the viewer is done. If the program
// ended, it sends why; a viewer that fell behind just loses its stream.
func (t *terminal) writeViewer(v *viewer) {
	var spare []byte
	for {
		select {
		case <-v.wake:
		case <-v.done:
			// The program may have written just before it ended; deliver
			// what is queued before saying goodbye.
			if batch := v.take(spare[:0]); len(batch) > 0 {
				if err := writeFrame(v.conn, frameData, batch); err != nil {
					return
				}
			}
			v.mu.Lock()
			exit := v.exit
			v.mu.Unlock()
			if exit != nil {
				_ = writeFrame(v.conn, frameExit, frameExitPayload(*exit))
			}
			return
		}
		// While a program streams, let the reads that follow join this
		// frame; the output after a pause goes at once, so a prompt still
		// shows as soon as the program writes it.
		if time.Since(v.lastWrite) < frameFlow {
			timer := time.NewTimer(frameGap)
			select {
			case <-timer.C:
			case <-v.done:
			}
			timer.Stop()
		}
		batch := v.take(spare[:0])
		if len(batch) == 0 {
			continue
		}
		if err := writeFrame(v.conn, frameData, batch); err != nil {
			return
		}
		v.lastWrite = time.Now()
		if cap(batch) > 1<<20 {
			spare = nil // a flood's buffer is not worth holding on to
		} else {
			spare = batch[:0]
		}
	}
}

// resize changes the screen and the program's terminal together.
func (t *terminal) resize(cols, rows int) {
	if cols < 2 || rows < 2 {
		return
	}
	t.mu.Lock()
	if cols == t.cols && rows == t.rows {
		t.mu.Unlock()
		return
	}
	t.cols, t.rows = cols, rows
	t.screen.Resize(cols, rows)
	t.mu.Unlock()
	_ = t.proc.Resize(cols, rows)
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

// read returns the last lines of the screen, and whether the program still
// runs. It keeps working after the program ended: the screen is still here.
func (t *terminal) read(lines int) ReadResult {
	if lines <= 0 {
		lines = 40
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := ReadResult{Terminal: t.id, Lines: t.screen.Tail(lines), Alive: t.alive}
	if t.exit != nil {
		exit := *t.exit
		out.Exit = &exit
	}
	return out
}

// record is the terminal as a client sees it.
func (t *terminal) record() Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := Record{
		ID: t.id, Kind: t.start.Kind, Name: t.start.Name, Parent: t.start.Parent,
		Argv: append([]string(nil), t.start.Argv...),
		Dir:  t.start.Dir, Cols: t.cols, Rows: t.rows, Alive: t.alive,
		Cwd: t.screen.Cwd(), Reported: t.screen.Reported(),
		Started: t.started.UnixMilli(), Labels: cloneLabels(t.start.Labels),
	}
	if r.Cwd == "" {
		r.Cwd = t.start.Dir
	}
	if t.start.Name != "" {
		r.Title = t.start.Name
	} else if title := t.screen.Title(); title != "" {
		r.Title = title
	} else if len(t.start.Argv) > 0 {
		r.Title = filepath.Base(t.start.Argv[0])
	}
	if status, ok := t.screen.Status(); ok {
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
