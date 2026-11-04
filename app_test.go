package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/control"
	"wada-terminal/internal/core"
	"wada-terminal/internal/plugins/terminal"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/kernel/kerneltest"
	"github.com/xleotranx/wada-kit/plugins/inspector"
	"github.com/xleotranx/wada-kit/plugins/settings"
	"github.com/xleotranx/wada-kit/shell"
)

// window is the app running headless: the shell, the queued work workers post,
// and the tester that builds frames of its view.
type window struct {
	t       *testing.T
	s       *shell.Shell
	tt      *ui.Tester
	dir     string
	store   kernel.Store
	closed  bool
	capture bool
	mu      sync.Mutex
	q       []func()
}

// startWindow starts the app the way main composes it, and draws it.
func startWindow(t *testing.T) *window {
	t.Helper()
	// The agents are not part of the test: a minimal PATH makes every seeded
	// session run a shell, as it does on a machine without them.
	return startWindowPath(t, "/usr/bin:/bin")
}

// startWindowPath starts the app with PATH, so a test can put a program where
// an agent would be.
func startWindowPath(t *testing.T, path string) *window {
	t.Helper()
	dir, err := os.MkdirTemp("", "wada-window-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return startWindowIn(t, path, dir, kerneltest.NewStore())
}

// startWindowIn starts a window on a chosen data folder and store, which lets
// a test close one window and open another on the core it left running.
func startWindowIn(t *testing.T, path, dir string, store kernel.Store) *window {
	t.Helper()
	t.Setenv("PATH", path)
	t.Setenv(config.EnvDir, filepath.Join(dir, "config"))
	w := &window{t: t, dir: dir, store: store}
	post := func(fn func()) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.q = append(w.q, fn)
	}
	opts := shell.Options{Name: app.Name, Menus: app.Menus, Post: post, Strict: true, Store: store, DataDir: dir}
	w.capture = injectCapture(&opts, func() ([]byte, error) { return []byte("\x89PNG\r\n\x1a\nfake"), nil })
	s, err := shell.New(opts, terminal.New, inspector.New, settings.New)
	if err != nil {
		t.Fatal(err)
	}
	w.s = s
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	w.tt = ui.NewTester(s.View, 1100, 760)
	w.settle()
	return w
}

// injectCapture gives the shell a fake capture function when the installed
// wada-kit has the optional field, and reports whether it did. Reflection keeps
// this test compiling against a kit from before the capture hook.
func injectCapture(opts *shell.Options, fn func() ([]byte, error)) bool {
	field := reflect.ValueOf(opts).Elem().FieldByName("Capture")
	if !field.IsValid() || !field.CanSet() || field.Type() != reflect.TypeOf(fn) {
		return false
	}
	field.Set(reflect.ValueOf(fn))
	return true
}

// close ends the window, leaving the core's terminals running as a person
// quitting the app would.
func (w *window) close() {
	if w.closed {
		return
	}
	w.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.s.Close(ctx); err != nil {
		var stacks bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
		w.t.Errorf("%v\n%s", err, stacks.String())
	}
}

// run lands what workers posted. It lets go of the queue's lock while a
// closure runs: a closure can post again, as a command change does.
func (w *window) run() {
	for {
		w.mu.Lock()
		if len(w.q) == 0 {
			w.mu.Unlock()
			return
		}
		fn := w.q[0]
		w.q = w.q[1:]
		w.mu.Unlock()
		fn()
	}
}

// settle lets the sessions start, then draws.
func (w *window) settle() {
	w.t.Helper()
	for range 30 {
		w.run()
		time.Sleep(2 * time.Millisecond)
		w.tt.Frame()
	}
}

// texts is what the frame shows, as text.
func (w *window) texts() string {
	w.tt.Frame()
	return strings.Join(append(w.tt.Texts(), w.paneTexts()...), "\n")
}

// paneTexts is what every pane's terminal shows, which the terminal paints
// rather than lays out as text, read the way a script reads it.
func (w *window) paneTexts() []string {
	raw, err := w.call(control.Request{Method: "ls"})
	if err != nil {
		return nil
	}
	var list control.List
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []string
	for _, space := range list.Spaces {
		for _, tab := range space.Tabs {
			for _, pane := range tab.Panes {
				raw, err := w.call(control.Request{Method: "do", Action: "terminal.read",
					Args: map[string]any{"pane": pane.ID}})
				if err != nil {
					continue
				}
				var read struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(raw, &read) == nil && read.Text != "" {
					out = append(out, read.Text)
				}
			}
		}
	}
	return out
}

// command runs a command the way the menu bar does.
func (w *window) command(id string) {
	w.t.Helper()
	for _, e := range kernel.Items(w.s.Kernel, kernel.Commands) {
		if e.ID == id {
			kernel.Invoke(w.s.Kernel, e, func(c kernel.Command) {
				if c.Run != nil {
					c.Run()
				}
			})
			w.settle()
			return
		}
	}
	w.t.Fatalf("no command %q", id)
}

// TestWindowShowsWorkspaceAndTabs checks the frame has the workspace on show,
// the terminal list, the sessions the app starts with, and the way to open
// another tab. The other workspaces and a new one live behind the switcher.
func TestWindowShowsWorkspaceAndTabs(t *testing.T) {
	w := startWindow(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	texts := w.texts()
	for _, want := range []string{filepath.Base(wd), "New terminal", "TERMINALS", "All terminals", "2 panes"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the frame does not show %q:\n%s", want, texts)
		}
	}
	// The app starts with a session in a tab and a split of two in another;
	// each tab names a session, whatever program actually ran.
	if got := strings.Count(texts, "Shell"); got == 0 && !strings.Contains(texts, "OpenCode") {
		t.Errorf("the frame shows no session:\n%s", texts)
	}
	// Group labels occur once: the sidebar replaces the horizontal strip.
	if strings.Count(texts, "Agent terminals") != 1 {
		t.Fatalf("duplicate or missing split navigation:\n%s", texts)
	}
	w.command("terminal.sidebar")
	texts = w.texts()
	if strings.Contains(texts, "TERMINALS") || !strings.Contains(texts, "Agent terminals") {
		t.Fatalf("hiding the sidebar did not restore tab navigation:\n%s", texts)
	}
}

// TestWorkspaceSwitcherOpens checks that the row at the top of the column
// opens the menu of workspaces, with the other one and the way to open a new
// folder.
func TestWorkspaceSwitcherOpens(t *testing.T) {
	w := startWindow(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	switcher := filepath.Base(wd)
	if strings.Contains(w.texts(), "New workspace…") {
		t.Fatalf("the menu is open before it was asked for:\n%s", w.texts())
	}
	if err := w.tt.Click(switcher); err != nil {
		t.Fatalf("no workspace switcher: %v", err)
	}
	texts := w.texts()
	for _, want := range []string{switcher, "New workspace…", "1 tab"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the open menu does not show %q:\n%s", want, texts)
		}
	}
	// The switcher toggles: a second click closes what it opened.
	if err := w.tt.Click(switcher); err != nil {
		t.Fatal(err)
	}
	if texts := w.texts(); strings.Contains(texts, "New workspace…") {
		t.Errorf("the switcher did not close the menu:\n%s", texts)
	}
	// The menu opens again, and choosing the home workspace shows it.
	if err := w.tt.Click(switcher); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if home != "" && home != wd {
		if err := w.tt.Click(filepath.Base(home)); err != nil {
			t.Fatalf("the menu does not list the home workspace: %v", err)
		}
		if texts := w.texts(); !strings.Contains(texts, "~") {
			t.Errorf("choosing the home workspace did not show its folder:\n%s", texts)
		}
	}
}

// TestSettingsPageReturns checks that opening Settings leaves a way back to
// the terminals, and that clicking it returns.
func TestSettingsPageReturns(t *testing.T) {
	w := startWindow(t)
	if texts := w.texts(); strings.Contains(texts, "← Back") {
		t.Fatalf("the terminals show a back button:\n%s", texts)
	}
	w.command("settings.open")
	if texts := w.texts(); !strings.Contains(texts, "Plugins") {
		t.Fatalf("settings did not open:\n%s", texts)
	}
	if !strings.Contains(w.texts(), "← Back") {
		t.Fatalf("settings has no way back to the terminals:\n%s", w.texts())
	}
	if back, ok := w.tt.Find("← Back"); !ok || back.X > 500 {
		t.Fatalf("the way back is not at the top left: %+v shown=%v", back, ok)
	}
	if err := w.tt.Click("← Back"); err != nil {
		t.Fatalf("the back button is not clickable: %v", err)
	}
	w.settle()
	if texts := w.texts(); strings.Contains(texts, "Color theme") {
		t.Fatalf("the back button did not leave settings:\n%s", texts)
	}
}

// TestSettingsButton checks that the title bar's gear opens Settings.
func TestSettingsButton(t *testing.T) {
	w := startWindow(t)
	if err := w.tt.Click("Settings"); err != nil {
		t.Fatalf("the settings button is not clickable: %v", err)
	}
	w.settle()
	if texts := w.texts(); !strings.Contains(texts, "Plugins") {
		t.Fatalf("the settings button did not open settings:\n%s", texts)
	}
}

// TestCommands checks that opening a tab and closing a pane leave a frame that
// still draws, as the menu bar's commands do.
func TestCommands(t *testing.T) {
	w := startWindow(t)
	w.command("terminal.new-tab")
	if texts := w.texts(); !strings.Contains(texts, "wada-terminal") {
		t.Fatalf("after a new tab the frame lost the workspace:\n%s", texts)
	}
	w.command("terminal.split-right")
	w.command("terminal.close-pane")
	w.command("terminal.tab-2")
	w.command("terminal.sidebar")
	w.texts()
}

// hasPTY reports whether this machine lets a test open a pseudo-terminal.
func hasPTY() bool {
	f, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// keyFor is the key that types r on a US keyboard.
func keyFor(r rune) ui.Key {
	switch {
	case r >= 'a' && r <= 'z':
		return ui.KeyA + ui.Key(r-'a')
	case r >= '0' && r <= '9':
		return ui.Key0 + ui.Key(r-'0')
	case r == ' ':
		return ui.KeySpace
	case r == '-':
		return ui.KeyMinus
	}
	return ui.KeyUnknown
}

// TestTypingReachesTheShell types a command at a pane and reads its output
// from the frame: the key encoding, the pseudo-terminal, the screen emulator
// and the painter all take part.
func TestTypingReachesTheShell(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	w.command("terminal.new-tab") // a shell, focused
	for _, r := range "echo zq7x" {
		w.tt.TypeKey(0, keyFor(r), string(r))
	}
	w.tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w.run()
		time.Sleep(10 * time.Millisecond)
		if strings.Count(w.texts(), "zq7x") >= 2 {
			return
		}
	}
	t.Fatalf("the shell never printed what was typed:\n%s", w.texts())
}

// typeLine types text and Return at the focused pane.
func (w *window) typeLine(text string) {
	for _, r := range text {
		w.tt.TypeKey(0, keyFor(r), string(r))
	}
	w.tt.Key(0, ui.KeyEnter)
}

// hasLine reports whether the frame has want as a line of its own, trimmed.
func hasLine(texts, want string) bool {
	for _, line := range strings.Split(texts, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// TestTheTerminalAnswersQueries asks the pane's terminal for its background
// color the way a program does, and reads the answer back from the frame: the
// query reaches the screen emulator, the reply goes to the program through the
// pseudo-terminal.
func TestTheTerminalAnswersQueries(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	w.command("terminal.new-tab")
	// The inner shell is not interactive, so nothing between it and the
	// terminal reads the reply: dd takes the 25 bytes of OSC 11.
	w.typeLine(`zsh -c 'printf "\033]11;?\007"; dd bs=1 count=25 2>/dev/null | cat -v'`)
	// The reply reports the surface the pane paints on: the dark one in a
	// dark window, the light one in a light window, as this tester builds it.
	want := []string{"rgb:0b0b/0b0b/0d0d", "rgb:fbfb/fbfb/fcfc"}
	deadline := time.Now().Add(20 * time.Second)
	for !containsAny(w.texts(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the terminal did not answer the color query:\n%s", w.texts())
		}
		w.run()
		time.Sleep(10 * time.Millisecond)
	}
}

// containsAny reports whether texts holds one of wants.
func containsAny(texts string, wants []string) bool {
	for _, want := range wants {
		if strings.Contains(texts, want) {
			return true
		}
	}
	return false
}

// TestSelectionCopiesWithTheKeys selects a pane's text with the keys that
// select and copy on macOS, which the pane's terminal binds: what it shows
// comes back as text on the clipboard.
func TestSelectionCopiesWithTheKeys(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	w.command("terminal.new-tab") // a shell, focused
	w.typeLine("echo hello-copy")
	deadline := time.Now().Add(20 * time.Second)
	for !hasLine(w.texts(), "hello-copy") {
		if time.Now().After(deadline) {
			t.Fatalf("the shell never printed the line:\n%s", w.texts())
		}
		w.run()
		time.Sleep(10 * time.Millisecond)
	}
	w.tt.Key(ui.Super, ui.KeyA)
	w.tt.Key(ui.Super, ui.KeyC)
	w.settle()
	if got := w.tt.Clipboard(); !strings.Contains(got, "hello-copy") {
		t.Fatalf("the clipboard holds %q, not the pane's text", got)
	}
}

// TestAgentRedrawInPlace runs a program that draws as pi does: its input's
// cursor marked with an APC that BEL ends, then redrawn in place by moving up
// and erasing lines. The pane shows the redraw alone, with nothing of the old
// frame left and no escape sequence printed as text.
func TestAgentRedrawInPlace(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	script := filepath.Join(w.dir, "redraw.sh")
	body := `printf '\033[38;2;72;78;82m|\033[0m old command line\r\n> typed\033_pi:c\007\033[7m \033[27m'
sleep 0.2
printf '\033[?2026h\r\033[1A\033[2K\033[38;2;72;78;82m|\033[0m redrawn\r\n\033[2K> x\033_pi:c\007\033[?2026l'
sleep 30
`
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	w.command("terminal.new-tab")
	w.typeLine("clear; sh " + script)
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(w.texts(), "| redrawn") {
		if time.Now().After(deadline) {
			t.Fatalf("the redraw never showed:\n%s", w.texts())
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	texts := w.texts()
	for _, stale := range []string{"old command line", "> typed", "72;78", "pi:c"} {
		if strings.Contains(texts, stale) {
			t.Fatalf("the pane shows %q after the redraw:\n%s", stale, texts)
		}
	}
}

// dividerAt finds the one-pixel line a split draws between its panes: the
// only column that differs from both its neighbours, down the whole pane.
func dividerAt(img *image.RGBA, ys []int) (int, bool) {
	b := img.Bounds()
	for x := b.Min.X + 320; x < b.Max.X-2; x++ {
		found := true
		for _, y := range ys {
			c := img.RGBAAt(x, y)
			if c == img.RGBAAt(x-1, y) || c == img.RGBAAt(x+1, y) || img.RGBAAt(x-1, y) != img.RGBAAt(x+1, y) {
				found = false
				break
			}
		}
		if found {
			return x, true
		}
	}
	return 0, false
}

// TestDraggingASplitDivider drags the bar between two panes and checks that it
// moved: the divider is hit-testable, holds the pointer, and redraws the split.
func TestDraggingASplitDivider(t *testing.T) {
	w := startWindow(t)
	w.command("terminal.split-right")
	w.tt.Frame()
	ys := []int{140, 260, 380, 500}
	before, ok := dividerAt(w.tt.Image(), ys)
	if !ok {
		t.Fatal("the split draws no divider")
	}
	w.tt.Press(float32(before), 300)
	w.tt.Move(float32(before+120), 300)
	w.tt.Release(float32(before+120), 300)
	w.texts()
	after, ok := dividerAt(w.tt.Image(), ys)
	if !ok {
		t.Fatal("the divider vanished after the drag")
	}
	if after < before+80 {
		t.Fatalf("the divider moved from %d to %d, not with the pointer", before, after)
	}
}

// TestMouseReportingReachesTheProgram turns mouse tracking on from the shell,
// then clicks a pane: what the program reads back is the report the terminal
// sent, shown by cat -v.
func TestMouseReportingReachesTheProgram(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	w.command("terminal.new-tab")
	w.typeLine(`printf '\033[?1000h\033[?1006h'; cat -v`)
	// Let the shell start the program before the click, then click until it
	// answers: the report is written to the program, which prints it.
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(w.texts(), "[<") {
		if time.Now().After(deadline) {
			t.Fatalf("the program never saw a mouse report:\n%s", w.texts())
		}
		w.tt.ClickAt(600, 400)
		w.run()
		time.Sleep(150 * time.Millisecond)
	}
}

// TestResizeReflowsTheLine types a line longer than the pane and narrows the
// window: the line wraps again, and no character is lost.
func TestResizeReflowsTheLine(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	w := startWindow(t)
	w.command("terminal.new-tab")
	w.typeLine(`printf 'z%.0s' {1..120}; printf '\n'`)
	deadline := time.Now().Add(20 * time.Second)
	for strings.Count(w.texts(), "z") < 121 {
		if time.Now().After(deadline) {
			t.Fatalf("the line never showed:\n%s", w.texts())
		}
		w.run()
		time.Sleep(10 * time.Millisecond)
	}
	before := strings.Count(w.texts(), "z")
	w.tt.SetSize(900, 760)
	for range 6 {
		w.run()
		w.tt.Frame()
		time.Sleep(10 * time.Millisecond)
	}
	if after := strings.Count(w.texts(), "z"); after != before {
		t.Fatalf("the line held %d characters before the resize and %d after", before, after)
	}
}

// TestSessionsIndexesEverySession opens the index with its shortcut and
// finds the sessions the window holds, then gives the keyboard back.
func TestSessionsIndexesEverySession(t *testing.T) {
	w := startWindow(t)
	// The shortcut belongs to the View menu, which a headless window has no
	// menu bar to press; the command is what the menu runs.
	w.command("terminal.index")
	texts := w.texts()
	for _, want := range []string{"Sessions", "Every session in this window", "move", "open", "close", "sessions"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("the index does not show %q:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "No sessions open") {
		t.Fatalf("the index is empty although the window has sessions:\n%s", texts)
	}

	// Return opens the chosen row and closes the index.
	w.tt.Key(0, ui.KeyDown)
	w.tt.Key(0, ui.KeyEnter)
	if texts := w.texts(); strings.Contains(texts, "Every session in this window") {
		t.Fatalf("the index stayed open after Return:\n%s", texts)
	}

	// Escape closes it too.
	w.command("terminal.index")
	if texts := w.texts(); !strings.Contains(texts, "Every session in this window") {
		t.Fatalf("the command did not open the index:\n%s", texts)
	}
	w.tt.Key(0, ui.KeyEscape)
	if texts := w.texts(); strings.Contains(texts, "Every session in this window") {
		t.Fatalf("the index stayed open after Escape:\n%s", texts)
	}
}

// TestSessionsFindsABlockedAgent puts a program where Claude Code would be,
// has it draw an approval, and waits for the quiet watcher: the pane says the
// agent needs input, and the index opens on it.
func TestSessionsFindsABlockedAgent(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\nprintf 'Do you want to proceed?\\n\\n\\342\\235\\257 1. Yes\\n  2. No\\n'\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	w := startWindowPath(t, dir+":/usr/bin:/bin")
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(w.texts(), "Needs input") {
		if time.Now().After(deadline) {
			t.Fatalf("the approval never reached the pane's state:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	w.command("terminal.index")
	texts := w.texts()
	if !strings.Contains(texts, "Needs input") {
		t.Fatalf("the index does not show the blocked agent:\n%s", texts)
	}
	// The blocked agent is the row the index chose: Return opens it and closes
	// the index.
	w.tt.Key(0, ui.KeyEnter)
	if texts := w.texts(); strings.Contains(texts, "Every session in this window") {
		t.Fatalf("the index stayed open after opening the blocked agent:\n%s", texts)
	}
}

// TestSessionsFollowsOSC7501 puts a program where Claude Code would be,
// has it speak the Program Status Protocol instead of drawing an approval, and
// checks that the pane and the index show what it said.
func TestSessionsFollowsOSC7501(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" +
		`printf '\033]7501;state=blocked:kind=permission:app=claude-code:title=Rml4IGxvZ2lu:msg=QWxsb3cgdGhlIGVkaXQ/\033\\'` + "\n" +
		"sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	w := startWindowPath(t, dir+":/usr/bin:/bin")
	w.command("terminal.index")
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Needs approval") &&
			strings.Contains(texts, "Fix login") && strings.Contains(texts, "Allow the edit?") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the 7501 report never reached the index:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	// The blocked agent is the row the index chose: Return opens its pane.
	w.tt.Key(0, ui.KeyEnter)
	texts := w.texts()
	if strings.Contains(texts, "Every session in this window") {
		t.Fatalf("the index stayed open after opening the reporting agent:\n%s", texts)
	}
	if !strings.Contains(texts, "Fix login") || !strings.Contains(texts, "Allow the edit?") {
		t.Fatalf("the pane does not show the protocol's title and message:\n%s", texts)
	}
}

// TestCoreKeepsTerminalsWhenTheWindowCloses starts a window on a core, closes
// it the way a person quitting does, and opens another window on the same
// data folder and saved layout: the core's terminals are still there and the
// second window attaches to them instead of starting new ones.
func TestCoreKeepsTerminalsWhenTheWindowCloses(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	dir, err := os.MkdirTemp("", "wada-core-window-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	server, err := core.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := core.Connect(dir)
	if err != nil {
		t.Fatal(err)
	}

	store := kerneltest.NewStore()
	w1 := startWindowIn(t, "/usr/bin:/bin", dir, store)
	before, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Terminals) == 0 || !before.Terminals[0].Alive {
		t.Fatalf("the first window started no core terminals: %+v", before.Terminals)
	}
	ids := map[int]bool{}
	for _, rec := range before.Terminals {
		ids[rec.ID] = true
	}
	w1.close()

	afterClose, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterClose.Terminals) != len(before.Terminals) {
		t.Fatalf("closing the window started or ended terminals: before %d, after %d", len(before.Terminals), len(afterClose.Terminals))
	}
	for _, rec := range afterClose.Terminals {
		if !rec.Alive || !ids[rec.ID] {
			t.Fatalf("a terminal did not survive the window: %+v", rec)
		}
	}

	w2 := startWindowIn(t, "/usr/bin:/bin", dir, store)
	afterReopen, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterReopen.Terminals) != len(before.Terminals) {
		t.Fatalf("the reopened window started new terminals instead of attaching: before %d, after %d", len(before.Terminals), len(afterReopen.Terminals))
	}
	for _, rec := range afterReopen.Terminals {
		if !ids[rec.ID] || !rec.Alive {
			t.Fatalf("the reopened window lost a terminal: %+v", rec)
		}
	}
	if texts := w2.texts(); !strings.Contains(texts, "Shell") {
		t.Fatalf("the reopened window does not show its terminals:\n%s", texts)
	}
}

// call sends one control request to the window and runs the posted work it
// waits on, the way the app's event loop would.
func (w *window) call(req control.Request) (json.RawMessage, error) {
	type answer struct {
		result json.RawMessage
		err    error
	}
	ch := make(chan answer, 1)
	go func() {
		result, err := control.Call(control.SocketPath(w.dir), req)
		ch <- answer{result, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case got := <-ch:
			return got.result, got.err
		default:
			w.run()
			if time.Now().After(deadline) {
				return nil, errors.New("the control request did not finish")
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// TestControlSocketDrivesTheWindow checks the agent-facing API: the window
// answers ls and status, makes a tab, and refuses an action it does not know.
func TestControlSocketDrivesTheWindow(t *testing.T) {
	w := startWindow(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := w.call(control.Request{Method: "status"}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			files, _ := filepath.Glob(filepath.Join(w.dir, "*"))
			t.Fatalf("the window never opened its control socket; dir %q holds %v; frame:\n%s", w.dir, files, w.texts())
		}
	}

	raw, err := w.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	var list control.List
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Spaces) < 2 || len(list.Spaces[0].Tabs) == 0 || len(list.Spaces[0].Tabs[0].Panes) == 0 {
		t.Fatalf("ls does not describe the seeded window: %s", raw)
	}
	if got := list.Spaces[0].Tabs[0].Panes[0].State; got == "" {
		t.Fatalf("a pane has no state: %s", raw)
	}

	raw, err = w.call(control.Request{Method: "do", Action: "tab.new", Args: map[string]any{"kind": "shell"}})
	if err != nil {
		t.Fatalf("tab.new: %v", err)
	}
	var made struct {
		Pane int `json:"pane"`
		Tab  int `json:"tab"`
	}
	if err := json.Unmarshal(raw, &made); err != nil || made.Pane == 0 {
		t.Fatalf("tab.new returned %s (%v)", raw, err)
	}
	raw, _ = w.call(control.Request{Method: "ls"})
	var after control.List
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if n := countPanes(after); n <= countPanes(list) {
		t.Fatalf("tab.new did not add a session: before %d, after %d", countPanes(list), n)
	}

	if w.capture {
		raw, err = w.call(control.Request{Method: "shot"})
		if err != nil {
			t.Fatalf("shot: %v", err)
		}
		var shot struct {
			PNG string `json:"png"`
		}
		if err := json.Unmarshal(raw, &shot); err != nil || shot.PNG == "" {
			t.Fatalf("shot returned %s (%v)", raw, err)
		}
		if png, err := base64.StdEncoding.DecodeString(shot.PNG); err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
			t.Fatalf("shot PNG = %q (%v)", png, err)
		}
	}

	if _, err := w.call(control.Request{Method: "do", Action: "pane.explode"}); err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if _, err := w.call(control.Request{Method: "do", Action: "pane.focus"}); err == nil {
		t.Fatal("a required argument was not enforced")
	}
	if _, err := w.call(control.Request{Method: "do", Action: "space.new", Args: map[string]any{"nope": "x"}}); err == nil {
		t.Fatal("an unknown argument was not refused")
	}
}

// countPanes counts the sessions in a list answer.
func countPanes(list control.List) int {
	n := 0
	for _, space := range list.Spaces {
		for _, tab := range space.Tabs {
			n += len(tab.Panes)
		}
	}
	return n
}

// TestInterfaceFilesReload checks the third herdr-app idea end to end: the
// window writes the files and the guide, notices a save, applies a good one,
// and keeps the last good version when a file is broken.
func TestInterfaceFilesReload(t *testing.T) {
	w := startWindow(t)
	dir := filepath.Join(w.dir, "config")
	for _, name := range []string{"AGENTS.md", "settings.json", "keys.json", "layouts/main.json", "themes/atom-one-dark.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("the window did not write %s: %v", name, err)
		}
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			w.run()
			frame := w.texts()
			if cond() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s:\n%s", what, frame)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}

	// A broken settings file is reported and changes nothing.
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"font_size": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wait("the settings error", func() bool {
		_, err := os.Stat(filepath.Join(dir, "settings.error"))
		return err == nil
	})
	if texts := w.texts(); !strings.Contains(texts, "did not load") {
		t.Fatalf("no toast for the broken settings:\n%s", texts)
	}

	// A good settings file selects a theme and a layout, and its error goes.
	if err := os.WriteFile(settings, []byte(`{"font_size": 14, "theme": "atom-one-dark", "layout": "main", "appearance": "dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "layouts/main.json"), []byte(`{"sidebar": {"side": "right", "width": 300}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wait("the settings to load", func() bool {
		_, err := os.Stat(filepath.Join(dir, "settings.error"))
		return os.IsNotExist(err)
	})
	w.texts()

	// keys.json: an unknown command is an error, then a valid file clears it.
	keys := filepath.Join(dir, "keys.json")
	if err := os.WriteFile(keys, []byte(`{"explode": "cmd+e"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wait("the keys error", func() bool {
		_, err := os.Stat(filepath.Join(dir, "keys.error"))
		return err == nil
	})
	if err := os.WriteFile(keys, []byte(`{"new-tab": "ctrl+shift+t", "index": ["cmd+shift+i"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wait("the keys to load", func() bool {
		_, err := os.Stat(filepath.Join(dir, "keys.error"))
		return os.IsNotExist(err)
	})
	w.texts()
}

// TestLayoutTreePlacesAgentsBesidePanes checks the composable layout format
// end to end: a root row with workspaces, panes and the permanent agents list.
func TestLayoutTreePlacesAgentsBesidePanes(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nprintf '\\033]7501;state=blocked:kind=permission:app=claude-code:title=Rml4IGxvZ2lu:msg=QWxsb3cgdGhlIGVkaXQ/\\033\\\\'\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	w := startWindowPath(t, bin+":/usr/bin:/bin")
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {
	    "type": "row",
	    "gap": 4,
	    "children": [
	      {"type": "workspaces", "size": {"px": 220}},
	      {"type": "panes", "size": {"weight": 1}},
	      {"type": "agents", "size": {"px": 260}}
	    ]
	  }
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Agents") && strings.Contains(texts, "Fix login") && strings.Contains(texts, "Needs approval") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agents layout never drew:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	// A broken layout keeps the working one and reports beside it.
	if err := os.WriteFile(layout, []byte(`{"root": {"type": "row", "children": [{"type": "wat"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(w.dir, "config", "layout.error")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a broken layout did not write layout.error")
		}
		w.run()
		time.Sleep(30 * time.Millisecond)
	}
	if texts := w.texts(); !strings.Contains(texts, "Agents") {
		t.Fatalf("a broken layout dropped the working one:\n%s", texts)
	}
}

// TestModPanelRendersAndClicks checks the mod runtime end to end: a layout's
// hello block draws, its button calls the script, and the state is saved.
func TestModPanelRendersAndClicks(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "mod", "mod": "hello", "size": {"px": 280}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Hello from a mod") && strings.Contains(texts, "Clicks: 0") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the hello mod did not draw:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	if err := w.tt.Click("Clicks: 0"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Clicks: 1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mod click did not redraw:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	state, err := os.ReadFile(filepath.Join(w.dir, "config", "mods", "hello", "state.json"))
	if err != nil || !strings.Contains(string(state), `"count": 1`) {
		t.Fatalf("state = %s (%v)", state, err)
	}
}

// TestMachineWorkspace checks a space whose panes run on another machine: the
// machine is connected through the ssh command, the remote core starts, and
// the window's list carries the machine's name.
func TestMachineWorkspace(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	root := t.TempDir()
	appBin := filepath.Join(root, "wada-terminal")
	build := exec.Command("go", "build", "-o", appBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the remote wada-terminal: %v\n%s", err, out)
	}
	sshDir := filepath.Join(root, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
host=$1; shift
export WADATERM_DATA_DIR="${FAKE_REMOTE_DATA:?}"
exec sh -c "$*"
`
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	remoteDir := filepath.Join(root, "remote")
	t.Setenv("FAKE_REMOTE_DATA", remoteDir)
	w := startWindowPath(t, sshDir+":"+root+":/usr/bin:/bin")
	if err := os.MkdirAll(filepath.Join(w.dir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.dir, "config", "machines.json"),
		[]byte(`{"box": {"ssh": "box", "bin": "wada-terminal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if body, err := os.ReadFile(filepath.Join(remoteDir, "core.json")); err == nil {
			var instance struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(body, &instance) == nil && instance.PID > 0 {
				if proc, err := os.FindProcess(instance.PID); err == nil {
					_ = proc.Kill()
				}
			}
		}
	})

	result, err := w.call(control.Request{Method: "do", Action: "space.new",
		Args: map[string]any{"machine": "box", "name": "remote"}})
	if err != nil {
		t.Fatal(err)
	}
	var made map[string]any
	if err := json.Unmarshal(result, &made); err != nil || made["machine"] != "box" {
		t.Fatalf("space.new = %s, %v", result, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "remote") && strings.Contains(texts, "box") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the machine workspace did not draw:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	listResult, err := w.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	var list control.List
	if err := json.Unmarshal(listResult, &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, space := range list.Spaces {
		if space.Machine != "box" || space.Name != "remote" {
			continue
		}
		found = true
		for _, tab := range space.Tabs {
			for _, pane := range tab.Panes {
				if pane.Machine != "box" {
					t.Fatalf("pane = %+v", pane)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no remote space in %+v", list)
	}
	// A new tab in that space starts on the same machine, not this computer.
	if _, err := w.call(control.Request{Method: "do", Action: "tab.new",
		Args: map[string]any{"kind": "shell"}}); err != nil {
		t.Fatal(err)
	}
	listResult, err = w.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	list = control.List{}
	if err := json.Unmarshal(listResult, &list); err != nil {
		t.Fatal(err)
	}
	for _, space := range list.Spaces {
		if space.Machine != "box" {
			continue
		}
		for _, tab := range space.Tabs {
			for _, pane := range tab.Panes {
				if pane.Machine != "box" {
					t.Fatalf("a tab on the machine made a local pane: %+v", pane)
				}
			}
		}
	}
	before := remoteTerminalCount(t, remoteDir)
	w.close()
	// A second window on the same store and data folder reattaches to the
	// machine's terminals instead of starting new ones.
	w = startWindowIn(t, sshDir+":"+root+":/usr/bin:/bin", w.dir, w.store)
	w.texts()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The menu over the column lists every workspace with the machine it runs
	// on, so the machine one is there to choose without opening it first.
	if err := w.tt.Click(filepath.Base(wd)); err != nil {
		t.Fatalf("no workspace switcher after reopening: %v", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "remote") && strings.Contains(texts, "box") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the machine workspace did not come back:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	after := remoteTerminalCount(t, remoteDir)
	if after != before {
		t.Fatalf("the machine had %d terminals before the window closed and %d after; it started new ones instead of attaching", before, after)
	}
}

// remoteTerminalCount reads a core directly. The fake SSH put the remote core
// on this computer's disk, so a test can count what it holds.
func remoteTerminalCount(t *testing.T, dir string) int {
	t.Helper()
	client, err := core.Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	return len(list.Terminals)
}

// TestLayoutStackSwitches checks a stack shows one child and the control
// action changes it and reports the choice in status.
func TestLayoutStackSwitches(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "stack", "id": "side", "active": "hello", "size": {"px": 280},
	     "children": [
	       {"type": "agents", "id": "agents"},
	       {"type": "mod", "id": "hello", "mod": "hello"}
	     ]}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if strings.Contains(w.texts(), "Hello from a mod") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stack did not draw its active child:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := w.call(control.Request{Method: "do", Action: "layout.stack",
		Args: map[string]any{"stack": "side", "active": "agents"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Agents") && !strings.Contains(texts, "Hello from a mod") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stack did not switch to agents:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	status, err := w.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var current control.Status
	if err := json.Unmarshal(status, &current); err != nil {
		t.Fatal(err)
	}
	if current.Stacks["side"] != "agents" {
		t.Fatalf("status stacks = %+v", current.Stacks)
	}
	if _, err := w.call(control.Request{Method: "do", Action: "layout.stack.next",
		Args: map[string]any{"stack": "side"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if strings.Contains(w.texts(), "Hello from a mod") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stack did not cycle back:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestFloatingModDrawsOverThePage checks a float's mod draws in a frame on the
// right of the window, outside the normal flow.
func TestFloatingModDrawsOverThePage(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right",
	     "align": "center", "width": 300, "height": 240}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Hello from a mod") {
			rect, ok := w.tt.Find("Hello from a mod")
			if !ok {
				t.Fatalf("the floating text has no place")
			}
			if rect.X < 700 {
				t.Fatalf("the float is not on the right: %+v", rect)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the floating mod did not draw:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestModInput checks a native input in a mod: typing updates api.inputs and
// Enter calls the mod with the value.
func TestModInput(t *testing.T) {
	w := startWindow(t)
	folder := filepath.Join(w.dir, "config", "mods", "form")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `function render(api) {
  return {type: "column", padding: 10, gap: 6, children: [
    {type: "text", text: "Value: " + (api.inputs.name || "")},
    {type: "text", text: "Last: " + (api.state.last || "none"), color: "muted", font_size: 10},
    {type: "input", id: "name", value: "", placeholder: "Type name", on_submit: true, autofocus: true}
  ]};
}
function click(id, api) {
  if (id === "name") { api.state.last = api.inputs.name; }
}
`
	if err := os.WriteFile(filepath.Join(folder, "main.js"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "mod", "mod": "form", "size": {"px": 340}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Value: ") && strings.Contains(texts, "Last: none") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the form mod did not draw:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	w.tt.Type("src/x.go")
	w.tt.Key(0, ui.KeyEnter)
	deadline = time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Value: src/x.go") && strings.Contains(texts, "Last: src/x.go") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the input never reached the mod:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestAnimatedModRedraws checks api.animate makes a mod draw again without
// another input event.
func TestAnimatedModRedraws(t *testing.T) {
	w := startWindow(t)
	folder := filepath.Join(w.dir, "config", "mods", "pulse")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	source := `function render(api) {
  api.animate(30);
  return {type: "text", text: "Tick " + Math.floor(api.now / 30), translate_x: 2};
}
`
	if err := os.WriteFile(filepath.Join(folder, "main.js"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "mod", "mod": "pulse", "size": {"px": 260}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first := ""
	deadline := time.Now().Add(20 * time.Second)
	for {
		w.tt.Frame()
		for _, text := range w.tt.Texts() {
			if strings.HasPrefix(text, "Tick ") {
				first = text
				break
			}
		}
		if first != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the animated mod did not draw:\n%s", w.texts())
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		w.tt.Frame()
		for _, text := range w.tt.Texts() {
			if strings.HasPrefix(text, "Tick ") && text != first {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the animated mod stayed at %q", first)
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
}

// TestFloatingModDrags checks a float follows the pointer, saves its offset,
// and goes back to the layout anchor with layout.float.reset.
func TestFloatingModDrags(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right",
	     "align": "center", "width": 300, "height": 240}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var start ui.Rect
	for {
		w.tt.Frame()
		if rect, ok := w.tt.Find("Hello from a mod"); ok {
			start = rect
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the float did not draw:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	x, y := start.X+20, start.Y+8
	w.tt.Move(x, y)
	w.tt.Press(x, y)
	for i := 1; i <= 8; i++ {
		w.tt.Move(x-float32(15*i), y-float32(8*i))
	}
	w.tt.Release(x-120, y-64)
	w.tt.Frame()
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		rect, ok := w.tt.Find("Hello from a mod")
		if ok && rect.X < start.X-60 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the float did not follow the drag; start %v, now %v\n%s", start, rect, w.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, err := w.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var current control.Status
	if err := json.Unmarshal(status, &current); err != nil {
		t.Fatal(err)
	}
	offset, ok := current.Floats["hello"]
	if !ok || offset[0] > -30 {
		t.Fatalf("the drag offset was not saved: %+v", current.Floats)
	}
}

// TestFloatingModPeek checks a peek float starts as a strip and expands on
// hover, then collapses when the pointer leaves.
func TestFloatingModPeek(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right",
	     "align": "center", "width": 300, "height": 240, "peek": 52}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var strip ui.Rect
	for {
		w.tt.Frame()
		if rect, ok := w.tt.Find("⋮ hello"); ok {
			if strings.Contains(w.texts(), "Hello from a mod") {
				t.Fatalf("the float did not start as a strip:\n%s", w.texts())
			}
			strip = rect
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the peek strip did not draw:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	w.tt.Move(strip.X+strip.W/2, strip.Y+strip.H/2)
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		if strings.Contains(w.texts(), "Hello from a mod") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the peek did not expand on hover:\n%s", w.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.tt.Move(80, 80)
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		if !strings.Contains(w.texts(), "Hello from a mod") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the peek did not collapse:\n%s", w.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestFloatingModSnaps checks a snap float springs back to the nearest edge
// after a drag and saves the settled offset.
func TestFloatingModSnaps(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right",
	     "align": "center", "width": 300, "height": 240, "snap": true}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var start ui.Rect
	for {
		w.tt.Frame()
		if rect, ok := w.tt.Find("Hello from a mod"); ok {
			start = rect
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the float did not draw:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	x, y := start.X+20, start.Y+8
	w.tt.Move(x, y)
	w.tt.Press(x, y)
	for i := 1; i <= 8; i++ {
		w.tt.Move(x-float32(15*i), y)
	}
	w.tt.Release(x-120, y)
	w.tt.Advance(500 * time.Millisecond)
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		if rect, ok := w.tt.Find("Hello from a mod"); ok && absFloat32(rect.X-start.X) < 20 {
			status, err := w.call(control.Request{Method: "status"})
			if err != nil {
				t.Fatal(err)
			}
			var current control.Status
			if err := json.Unmarshal(status, &current); err != nil {
				t.Fatal(err)
			}
			offset, ok := current.Floats["hello"]
			if !ok || absFloat32(offset[0]) > 2 {
				t.Fatalf("snapped offset = %v", offset)
			}
			return
		}
		if time.Now().After(deadline) {
			now, _ := w.tt.Find("Hello from a mod")
			t.Fatalf("the float did not snap back; start %v, now %v", start, now)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// absFloat32 is a small pixel distance.
func absFloat32(n float32) float32 {
	if n < 0 {
		return -n
	}
	return n
}

// TestLayoutResizesAndDividerDrags checks a fixed layout block's saved size
// through the action, then by dragging the divider between it and the panes.
func TestLayoutResizesAndDividerDrags(t *testing.T) {
	w := startWindow(t)
	layout := filepath.Join(w.dir, "config", "layouts", "main.json")
	if err := os.WriteFile(layout, []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "workspaces", "id": "side", "size": {"px": 220}},
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "agents", "id": "agents", "size": {"px": 260}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if strings.Contains(w.texts(), "Agents") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the resizable layout did not draw:\n%s", w.texts())
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := w.call(control.Request{Method: "do", Action: "layout.resize",
		Args: map[string]any{"id": "side", "px": 300}}); err != nil {
		t.Fatal(err)
	}
	status, err := w.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var current control.Status
	if err := json.Unmarshal(status, &current); err != nil {
		t.Fatal(err)
	}
	if current.Sizes["side"] != 300 {
		t.Fatalf("sizes = %+v", current.Sizes)
	}
	// Drag the divider right by 40: the sidebar's fixed size grows.
	x, y := float32(312), float32(300)
	w.tt.Move(x, y)
	w.tt.Press(x, y)
	for i := 1; i <= 4; i++ {
		w.tt.Move(x+float32(10*i), y)
		w.tt.Frame()
		w.run()
	}
	w.tt.Release(x+40, y)
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		status, err := w.call(control.Request{Method: "status"})
		if err != nil {
			t.Fatal(err)
		}
		current = control.Status{}
		if err := json.Unmarshal(status, &current); err != nil {
			t.Fatal(err)
		}
		if current.Sizes["side"] > 330 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the divider did not resize the sidebar: %+v", current.Sizes)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestMachinesPanelListsAndOpensRemoteTerminals checks a remote core's own
// terminal appears in the panel and opens as a machine space.
func TestMachinesPanelListsAndOpensRemoteTerminals(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	root, err := os.MkdirTemp("", "wada-mp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	appBin := filepath.Join(root, "wada-terminal")
	if out, err := exec.Command("go", "build", "-o", appBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, out)
	}
	sshDir := filepath.Join(root, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *) break ;;
  esac
done
host=$1; shift
export WADATERM_DATA_DIR="${FAKE_REMOTE_DATA:?}"
exec sh -c "$*"
`
	if err := os.WriteFile(filepath.Join(sshDir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	remoteDir := filepath.Join(root, "remote")
	t.Setenv("FAKE_REMOTE_DATA", remoteDir)
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(dataDir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "layouts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "machines.json"),
		[]byte(`{"box": {"ssh": "box", "bin": "wada-terminal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "agents.json"),
		[]byte(`{"cat": {"command": ["cat"], "match": ["cat"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "layouts", "main.json"), []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "machines", "size": {"px": 280}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := startWindowIn(t, sshDir+":"+root+":/usr/bin:/bin", dataDir, kerneltest.NewStore())
	t.Cleanup(func() {
		if body, err := os.ReadFile(filepath.Join(remoteDir, "core.json")); err == nil {
			var instance struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(body, &instance) == nil && instance.PID > 0 {
				if proc, err := os.FindProcess(instance.PID); err == nil {
					_ = proc.Kill()
				}
			}
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Machines") && strings.Contains(texts, "box") &&
			strings.Contains(texts, "No running terminals") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the machines panel did not connect:\n%s", texts)
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	remote, err := core.Connect(remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Start(core.Start{Argv: []string{"/bin/cat"}, Kind: "shell",
		Name: "remote-cat", Cols: 40, Rows: 10,
		Labels: map[string]string{"space": "review"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "remote-cat") && strings.Contains(texts, "review") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the remote terminal never appeared:\n%s", w.texts())
		}
		w.run()
		time.Sleep(100 * time.Millisecond)
	}
	if err := w.tt.Click("remote-cat"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	found := false
	for !found {
		w.run()
		w.tt.Frame()
		result, err := w.call(control.Request{Method: "ls"})
		if err != nil {
			t.Fatal(err)
		}
		var list control.List
		if err := json.Unmarshal(result, &list); err != nil {
			t.Fatal(err)
		}
		for _, space := range list.Spaces {
			if space.Machine != "box" || space.Name != "review" {
				continue
			}
			for _, tab := range space.Tabs {
				for _, pane := range tab.Panes {
					if pane.Machine == "box" && strings.Contains(pane.Title, "remote-cat") {
						found = true
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("clicking the remote terminal did not open it: %+v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Save the space to the remote core, close it, and bring it back from the
	// core's own document.
	// The remote core's foreground process adopts the cat terminal.
	deadline = time.Now().Add(10 * time.Second)
	for {
		w.run()
		result, err := w.call(control.Request{Method: "status"})
		if err != nil {
			t.Fatal(err)
		}
		var current control.Status
		if err := json.Unmarshal(result, &current); err != nil {
			t.Fatal(err)
		}
		if current.Pane != nil && current.Pane.Agent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the remote foreground did not adopt the agent: %+v", current.Pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Saving the space publishes it to the remote core without an explicit
	// export command.
	deadline = time.Now().Add(10 * time.Second)
	for {
		document, err := remote.Layout()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(document), "review") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("remote layout = %s", document)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// A second window with no local spaces sees the remote document on its
	// own when the panel connects.
	data2 := filepath.Join(root, "data2")
	config2 := filepath.Join(data2, "config")
	if err := os.MkdirAll(filepath.Join(config2, "layouts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config2, "machines.json"),
		[]byte(`{"box": {"ssh": "box", "bin": "wada-terminal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config2, "layouts", "main.json"), []byte(`{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "machines", "size": {"px": 280}}
	  ]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w2 := startWindowIn(t, sshDir+":"+root+":/usr/bin:/bin", data2, kerneltest.NewStore())
	deadline = time.Now().Add(20 * time.Second)
	imported := false
	for !imported {
		w2.run()
		result, err := w2.call(control.Request{Method: "ls"})
		if err != nil {
			t.Fatal(err)
		}
		var list control.List
		if err := json.Unmarshal(result, &list); err != nil {
			t.Fatal(err)
		}
		for _, space := range list.Spaces {
			if space.Machine == "box" && space.Name == "review" {
				imported = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the second window did not import the remote space: %+v", list)
		}
		time.Sleep(100 * time.Millisecond)
	}
	w2.close()
	if _, err := w.call(control.Request{Method: "do", Action: "space.close"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.call(control.Request{Method: "do", Action: "space.import",
		Args: map[string]any{"machine": "box"}}); err != nil {
		t.Fatal(err)
	}
	result, err := w.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	var list control.List
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatal(err)
	}
	for _, space := range list.Spaces {
		if space.Machine == "box" && space.Name == "review" {
			return
		}
	}
	t.Fatalf("the imported space is missing: %+v", list)
}

// TestFindBarSearchesThePane checks Cmd+F opens a find bar, typing finds the
// shell's text and the bar reports a match, and the command closes it again.
func TestFindBarSearchesThePane(t *testing.T) {
	w := startWindow(t)
	w.command("terminal.new-tab")
	w.typeLine("echo findme")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if strings.Contains(w.texts(), "findme") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shell never printed the search text:\n%s", w.texts())
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	w.command("terminal.find")
	if texts := w.texts(); !strings.Contains(texts, "Find") {
		t.Fatalf("the find bar did not open:\n%s", texts)
	}
	w.tt.Type("findme")
	deadline = time.Now().Add(10 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "/") && !strings.Contains(texts, "No matches") &&
			!strings.Contains(texts, "Find in scrollback") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the find bar found nothing:\n%s", w.texts())
		}
		w.run()
		time.Sleep(30 * time.Millisecond)
	}
	w.command("terminal.find")
	if texts := w.texts(); strings.Contains(texts, "Find in scrollback") {
		t.Fatalf("the find bar did not close:\n%s", texts)
	}
}

// TestCustomAgentCommand starts an agents.json program from the Session menu,
// marks it an agent, and keeps its name and glyph.
func TestCustomAgentCommand(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '\\033]7501;state=blocked:app=fake-code:title=RmFrZSBBZ2VudA==:msg=QXBwcm92ZQ==\\033\\\\'\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "fakeagent"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "wada-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	agentsFile := filepath.Join(root, "config", "agents.json")
	if err := os.WriteFile(agentsFile, []byte(`{
	  "fake": {"name": "Fake Agent", "command": ["fakeagent"], "glyph": "F"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := startWindowIn(t, bin+":/usr/bin:/bin", root, kerneltest.NewStore())
	deadline := time.Now().Add(15 * time.Second)
	for {
		found := false
		for _, item := range kernel.Items(w.s.Kernel, kernel.Commands) {
			if item.ID == "terminal.agent-fake" {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the custom agent command never appeared")
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
	w.command("terminal.agent-fake")
	deadline = time.Now().Add(10 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "Fake Agent") && strings.Contains(texts, "Needs input") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the custom agent did not run:\n%s", texts)
		}
		w.run()
		time.Sleep(30 * time.Millisecond)
	}
	status, err := w.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var current control.Status
	if err := json.Unmarshal(status, &current); err != nil {
		t.Fatal(err)
	}
	if current.Pane == nil || !current.Pane.Agent || current.Pane.Title != "Fake Agent" {
		t.Fatalf("pane = %+v", current.Pane)
	}
	// The saved pane remembers which custom agent it ran.
	w.close()
	w = startWindowIn(t, bin+":/usr/bin:/bin", root, w.store)
	deadline = time.Now().Add(15 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		result, err := w.call(control.Request{Method: "ls"})
		if err != nil {
			t.Fatal(err)
		}
		var list control.List
		if err := json.Unmarshal(result, &list); err != nil {
			t.Fatal(err)
		}
		for _, space := range list.Spaces {
			for _, tab := range space.Tabs {
				for _, pane := range tab.Panes {
					if pane.Title == "Fake Agent" && pane.Agent {
						return
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the custom agent was not restored: %+v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestAgentsReload checks a new agents.json entry appears in the commands
// without restarting the window.
func TestAgentsReload(t *testing.T) {
	w := startWindow(t)
	path := filepath.Join(w.dir, "config", "agents.json")
	if err := os.WriteFile(path, []byte(`{
	  "late": {"name": "Late Agent", "command": ["lateagent"]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, item := range kernel.Items(w.s.Kernel, kernel.Commands) {
			if item.ID == "terminal.agent-late" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("a saved agents.json did not add its command")
		}
		w.run()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLinkPickerListsURLs checks the link picker shows URLs before one opens
// and closes without opening anything on Escape.
func TestLinkPickerListsURLs(t *testing.T) {
	w := startWindow(t)
	linkFile := filepath.Join(w.dir, "links.txt")
	body := "\x1b]8;;https://one.example/a\x07OneLink\x1b]8;;\x07 \x1b]8;;https://two.example/b\x07TwoLink\x1b]8;;\x07\n"
	if err := os.WriteFile(linkFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	w.command("terminal.new-tab")
	w.tt.Type("cat " + linkFile)
	w.tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(15 * time.Second)
	for {
		texts := w.texts()
		if strings.Contains(texts, "OneLink") && strings.Contains(texts, "TwoLink") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the links never printed:\n%s", texts)
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	w.command("terminal.links")
	texts := w.texts()
	if !strings.Contains(texts, "https://one.example/a") || !strings.Contains(texts, "https://two.example/b") {
		t.Fatalf("the link picker did not list the URLs:\n%s", texts)
	}
	w.tt.Key(0, ui.KeyEscape)
	texts = w.texts()
	if strings.Contains(texts, "Links this pane's program printed") {
		t.Fatalf("the link picker did not close:\n%s", texts)
	}
}

// TestProgramTitleMatching adopts an agent named by programs.json when its
// program sets the terminal title inside a shell.
func TestProgramTitleMatching(t *testing.T) {
	root, err := os.MkdirTemp("", "wada-match-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "agents.json"),
		[]byte(`{"aider": {"command": ["aider"], "match": ["aider"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := startWindowIn(t, os.Getenv("PATH"), root, kerneltest.NewStore())
	w.command("terminal.new-tab")
	w.tt.Type("printf '\\033]2;aider\\007'; sleep 10")
	w.tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(15 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		result, err := w.call(control.Request{Method: "status"})
		if err != nil {
			t.Fatal(err)
		}
		var current control.Status
		if err := json.Unmarshal(result, &current); err != nil {
			t.Fatal(err)
		}
		if current.Pane != nil && current.Pane.Agent {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the title did not adopt the agent: %+v\n%s", current.Pane, w.texts())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestForegroundAdoptsAgent adopts a matched agent from the real foreground
// process, not just its title.
func TestForegroundAdoptsAgent(t *testing.T) {
	root, err := os.MkdirTemp("", "wada-fg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "agents.json"),
		[]byte(`{"sleeper": {"command": ["sleep"], "match": ["sleep"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	w := startWindowIn(t, os.Getenv("PATH"), root, kerneltest.NewStore())
	w.command("terminal.new-tab")
	w.typeLine("sleep 5")
	deadline := time.Now().Add(15 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		result, err := w.call(control.Request{Method: "status"})
		if err != nil {
			t.Fatal(err)
		}
		var current control.Status
		if err := json.Unmarshal(result, &current); err != nil {
			t.Fatal(err)
		}
		if current.Pane != nil && current.Pane.Agent {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the foreground program did not adopt the agent: %+v\n%s", current.Pane, w.texts())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTaskWorkspaceFlow(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "initial"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	w := startWindow(t)
	w.command("terminal.new-task")
	if !strings.Contains(w.texts(), "Local repository") {
		t.Fatal("task dialog did not open")
	}
	if _, err := w.call(control.Request{Method: "do", Action: "task.new", Args: map[string]any{"name": "Fix login redirect", "brief": "Keep sessions\nAdd a regression test", "folder": repo, "agent": "shell"}}); err != nil {
		t.Fatal(err)
	}
	wait := func(predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !predicate() {
			if time.Now().After(deadline) {
				t.Fatalf("task did not settle:\n%s", w.texts())
			}
			w.run()
			w.tt.Frame()
			time.Sleep(20 * time.Millisecond)
		}
	}
	var space control.Space
	wait(func() bool {
		body, err := w.call(control.Request{Method: "status"})
		if err != nil {
			return false
		}
		var status control.Status
		if json.Unmarshal(body, &status) != nil || status.Space == nil || status.Space.Task == nil {
			return false
		}
		space = *status.Space
		return true
	})
	if space.Task.Name != "Fix login redirect" || space.Task.Brief != "Keep sessions\nAdd a regression test" || space.Folder == repo {
		t.Fatalf("task not isolated: %+v", space)
	}
	if !strings.Contains(w.texts(), "Send task") {
		t.Fatal("task toolbar missing")
	}
	if err := os.WriteFile(filepath.Join(space.Folder, "a.txt"), []byte("fixed redirect\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.call(control.Request{Method: "do", Action: "task.review"}); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return strings.Contains(w.texts(), "fixed redirect") })
	if !strings.Contains(w.texts(), "Checks: Not run") {
		t.Fatal("check success inferred without a run")
	}
	if _, err := w.call(control.Request{Method: "do", Action: "task.check", Args: map[string]any{"command": "printf verified"}}); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		return strings.Contains(w.texts(), "Last check: exit 0") && strings.Contains(w.texts(), "verified")
	})
	w.close()
	reopened := startWindowIn(t, "/usr/bin:/bin", w.dir, w.store)
	body, err := reopened.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	var list control.List
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	found := false
	taskSpace := 0
	for _, saved := range list.Spaces {
		if saved.Task != nil && saved.Task.Dir == space.Folder {
			taskSpace = saved.ID
			found = saved.Task.Check != nil && saved.Task.Check.ExitCode == 0 && saved.Task.Brief == "Keep sessions\nAdd a regression test"
		}
	}
	if !found {
		t.Fatal("task identity/check was lost on reopen")
	}

	if _, err := reopened.call(control.Request{Method: "do", Action: "space.select", Args: map[string]any{"id": taskSpace}}); err != nil {
		t.Fatal(err)
	}
	reopened.tt.Frame()
	if err := reopened.tt.Click("Task brief…"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reopened.texts(), "Task brief") {
		t.Fatal("task brief dialog did not open")
	}
	if err := reopened.tt.Click("Brief editor"); err != nil {
		t.Fatal(err)
	}
	reopened.tt.Key(ui.Cmd, ui.KeyA)
	reopened.tt.Type("Revised acceptance criteria")
	if err := reopened.tt.Click("Save brief"); err != nil {
		t.Fatal(err)
	}
	body, err = reopened.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var edited control.Status
	if json.Unmarshal(body, &edited) != nil || edited.Space == nil || edited.Space.Task == nil || edited.Space.Task.Brief != "Revised acceptance criteria" {
		t.Fatalf("edited brief not saved: %s", body)
	}
	if err := reopened.tt.Click("Finish task…"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(reopened.texts(), "Save work & finish") {
		if time.Now().After(deadline) {
			t.Fatalf("finish preview did not load: %s", reopened.texts())
		}
		reopened.run()
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(space.Folder); err != nil {
		t.Fatal("preview removed task files")
	}
	if err := reopened.tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(space.Folder); err != nil {
		t.Fatal("cancel removed task files")
	}
	if err := reopened.tt.Click("Finish task…"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(reopened.texts(), "Save work & finish") {
		if time.Now().After(deadline) {
			t.Fatal("finish preview did not reload")
		}
		reopened.run()
		time.Sleep(20 * time.Millisecond)
	}
	if err := reopened.tt.Click("Save work & finish"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		reopened.run()
		reopened.tt.Frame()
		body, err := reopened.call(control.Request{Method: "ls"})
		if err != nil {
			t.Fatal(err)
		}
		var remaining control.List
		json.Unmarshal(body, &remaining)
		finished := false
		for _, s := range remaining.Spaces {
			if s.ID == taskSpace && s.Task != nil && s.Task.Finished && s.Task.BranchKept && s.Task.Dir == "" && s.Task.Brief == "Revised acceptance criteria" && s.Task.Check != nil {
				finished = true
			}
		}
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("finish failed: %s", reopened.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(space.Folder); !os.IsNotExist(err) {
		t.Fatal("finished task folder remains")
	}
	saved, err := exec.Command("git", "-C", repo, "show", space.Task.Branch+":a.txt").CombinedOutput()
	if err != nil || string(saved) != "fixed redirect\n" {
		t.Fatalf("finished work was not saved: %s %v", saved, err)
	}
	reopened.run()
	reopened.tt.Frame()
	if err := reopened.tt.Click("Review saved changes"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(reopened.texts(), "+fixed redirect") {
		if time.Now().After(deadline) {
			t.Fatalf("saved review did not load: %s", reopened.texts())
		}
		reopened.run()
		time.Sleep(20 * time.Millisecond)
	}
	if err := reopened.tt.Click("Integrate…"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(reopened.texts(), "Target branch:") {
		if time.Now().After(deadline) {
			t.Fatalf("integration preview did not load: %s", reopened.texts())
		}
		reopened.run()
		time.Sleep(20 * time.Millisecond)
	}
	original, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	if string(original) != "original\n" {
		t.Fatal("preview changed checkout")
	}
	if err := reopened.tt.Click("Confirm integration"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		reopened.run()
		body, err := reopened.call(control.Request{Method: "status"})
		var status control.Status
		if err == nil && json.Unmarshal(body, &status) == nil && status.Space != nil && status.Space.Task != nil && status.Space.Task.IntegratedCommit != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("integration did not complete: %s", reopened.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	original, _ = os.ReadFile(filepath.Join(repo, "a.txt"))
	if string(original) != "fixed redirect\n" {
		t.Fatalf("checkout not integrated: %s", original)
	}
	reopened.run()
	reopened.tt.Frame()
	if err := reopened.tt.Click("Start another attempt"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		reopened.run()
		body, err := reopened.call(control.Request{Method: "status"})
		var status control.Status
		if err == nil && json.Unmarshal(body, &status) == nil && status.Space != nil && status.Space.Task != nil && status.Space.Task.Dir != "" && len(status.Space.Task.Runs) == 1 {
			if status.Space.Task.Runs[0].Branch != space.Task.Branch || status.Space.Task.Branch == space.Task.Branch || status.Space.Task.Runs[0].IntegratedCommit == "" {
				t.Fatalf("retry lost prior result: %+v", status.Space.Task)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry did not start: %s", reopened.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	reopened.run()
	reopened.tt.Frame()
	if err := reopened.tt.Click("Attempt 1 · shell"); err != nil {
		t.Fatal(err)
	}
	reopened.run()
	reopened.tt.Frame()
	if err := reopened.tt.Click("Review saved changes"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(reopened.texts(), "+fixed redirect") {
		if time.Now().After(deadline) {
			t.Fatalf("previous attempt could not be reviewed: %s", reopened.texts())
		}
		reopened.run()
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTaskSubmissionReceipt(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"commit", "--allow-empty", "-m", "initial"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	root, bin := t.TempDir(), t.TempDir()
	marker := filepath.Join(root, "received")
	script := "#!/bin/sh\nprintf '\\033]7501;state=idle:app=fake-code\\033\\\\'\nIFS= read -r line\nprintf '%s' \"$line\" > \"$1\"\nprintf '\\033]7501;state=done:app=fake-code\\033\\\\'\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "fakeagent"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	agents, err := json.Marshal(map[string]any{"fake": map[string]any{"name": "Fake Agent", "command": []string{"fakeagent", marker}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "agents.json"), agents, 0600); err != nil {
		t.Fatal(err)
	}
	w := startWindowIn(t, bin+":/usr/bin:/bin", root, kerneltest.NewStore())
	if _, err := w.call(control.Request{Method: "do", Action: "task.new", Args: map[string]any{"name": "Fix login", "folder": repo, "agent": "custom:fake"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(w.texts(), "Agent reports idle") {
		if time.Now().After(deadline) {
			t.Fatalf("agent state missing: %s", w.texts())
		}
		w.run()
		w.tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(w.texts(), "Not sent with Send task") {
		t.Fatal("missing unsent status")
	}
	if err := w.tt.Click("Send task"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		data, _ := os.ReadFile(marker)
		if string(data) == "Fix login" && strings.Contains(w.texts(), "Submitted to terminal transport") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not submitted: %q; %s", data, w.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, err := w.call(control.Request{Method: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var status control.Status
	if err := json.Unmarshal(body, &status); err != nil || status.Space == nil || status.Space.Task == nil || status.Space.Task.SentAt == nil || status.Space.Task.SentName != "Fix login" {
		t.Fatalf("submission receipt missing: %s (%v)", body, err)
	}
	w.command("terminal.task-inbox")
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(w.texts(), "Ready to review · 1") {
		if time.Now().After(deadline) {
			t.Fatalf("reported result missing from inbox: %s", w.texts())
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	if err := w.tt.Click("Review · Fix login"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for !strings.Contains(w.texts(), "No changes from the task's starting commit.") {
		if time.Now().After(deadline) {
			t.Fatalf("review action did not load changes: %s", w.texts())
		}
		w.run()
		time.Sleep(20 * time.Millisecond)
	}
	w.close()
	reopened := startWindowIn(t, bin+":/usr/bin:/bin", root, w.store)
	body, err = reopened.call(control.Request{Method: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	var spaces control.List
	if err := json.Unmarshal(body, &spaces); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, space := range spaces.Spaces {
		if space.Task != nil && space.Task.Name == "Fix login" && space.Task.SentAt != nil && space.Task.SentProgram == "Fake Agent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("submission receipt did not survive reopen: %s", body)
	}
}

func TestTasksInbox(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"commit", "--allow-empty", "-m", "initial"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	w := startWindow(t)
	w.command("terminal.task-inbox")
	if !strings.Contains(w.texts(), "No tasks yet") {
		t.Fatalf("missing empty state: %s", w.texts())
	}
	if err := w.tt.Click("New task…"); err != nil {
		t.Fatal(err)
	}
	if err := w.tt.Click("Task name"); err != nil {
		t.Fatal(err)
	}
	w.tt.Type("Queued inbox task")
	if err := w.tt.Click("Repository folder"); err != nil {
		t.Fatal(err)
	}
	w.tt.Key(ui.Cmd, ui.KeyA)
	w.tt.Type(repo)
	if err := w.tt.Click("Add to queue"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.texts(), "QUEUED") {
		t.Fatalf("queued task desk missing: %s", w.texts())
	}
	w.command("terminal.task-inbox")
	if !strings.Contains(w.texts(), "Queued inbox task") || !strings.Contains(w.texts(), "Start…") {
		t.Fatalf("queued task missing from inbox: %s", w.texts())
	}
	if err := w.tt.Click("Start… · Queued inbox task"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.texts(), "Start task") {
		t.Fatal("queue action did not open task desk")
	}
	if _, err := w.call(control.Request{Method: "do", Action: "task.new", Args: map[string]any{"name": "Fix inbox", "folder": repo, "agent": "shell"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		w.run()
		w.tt.Frame()
		body, err := w.call(control.Request{Method: "status"})
		var status control.Status
		if err == nil && json.Unmarshal(body, &status) == nil && status.Space != nil && status.Space.Task != nil && status.Space.Task.Name == "Fix inbox" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not created: %s", w.texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.command("terminal.task-inbox")
	if text := w.texts(); !strings.Contains(text, "Open · 1") || !strings.Contains(text, "Fix inbox") || !strings.Contains(text, repo) {
		t.Fatalf("task missing from inbox: %s", text)
	}
	if err := w.tt.Click("Search tasks"); err != nil {
		t.Fatal(err)
	}
	w.tt.Type("not a task")
	if !strings.Contains(w.texts(), "No matching tasks") {
		t.Fatalf("search did not filter: %s", w.texts())
	}
	w.tt.Key(ui.Cmd, ui.KeyA)
	w.tt.Type("Fix inbox")
	if !strings.Contains(w.texts(), "Open task") || !strings.Contains(w.texts(), "Fix inbox") {
		t.Fatalf("matching task not shown: %s", w.texts())
	}
	if err := w.tt.Click("Open task · Fix inbox"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.texts(), "Send task") {
		t.Fatalf("task desk did not open: %s", w.texts())
	}
	w.command("terminal.task-inbox")
	if err := w.tt.Click("Back to workspace"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.texts(), "Send task") {
		t.Fatal("could not return to workspace")
	}
}
