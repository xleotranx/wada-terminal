package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mterm "github.com/egoist/mygo/plugins/terminal"
)

// TestMain lets this test binary act as the core process Ensure starts. A
// real Ensure runs the app's executable with --serve-core; the test's
// executable is this binary, so the same argument starts a core here.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--serve-core" {
		if err := Serve(os.Getenv("WADATERM_DATA_DIR")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--proxy" {
		if err := Proxy(os.Getenv("WADA_PROXY_DIR"), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
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

// testCore starts a core on a short temp folder and a client for it.
func testCore(t *testing.T) (*Server, *Client) {
	t.Helper()
	dir, err := os.MkdirTemp("", "wada-core-")
	if err != nil {
		t.Fatal(err)
	}
	server, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Connect(dir)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		_ = os.RemoveAll(dir)
	})
	return server, client
}

// view is a window's side of a stream: a terminal showing the core's frames,
// as a pane does, and the Info frames that came beside them.
type view struct {
	term   *mterm.Terminal
	stream *Stream
	mu     sync.Mutex
	infos  []Info
}

// attachView attaches a terminal to a core's terminal, as a window does.
func attachView(t *testing.T, client *Client, id, cols, rows int) (*view, error) {
	t.Helper()
	stream, err := client.Attach(id, cols, rows, nil)
	if err != nil {
		return nil, err
	}
	v := &view{stream: stream}
	stream.OnInfo = func(info Info) {
		v.mu.Lock()
		v.infos = append(v.infos, info)
		v.mu.Unlock()
	}
	term, err := mterm.New(mterm.Options{Conn: stream, ResizeByConn: true})
	if err != nil {
		stream.Close()
		t.Fatal(err)
	}
	term.Resize(stream.Cols, stream.Rows)
	stream.OnResized = term.Resize
	v.term = term
	t.Cleanup(v.close)
	return v, nil
}

func (v *view) close() {
	v.term.Close()
	v.stream.Close()
}

// text is what the view's terminal shows, with its scrollback.
func (v *view) text() string { return v.term.Text() }

// info is the first Info the stream carried, the program's record as it was.
func (v *view) info() (Info, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.infos) == 0 {
		return Info{}, false
	}
	return v.infos[0], true
}

// waitFor polls until cond is true or the test times out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCoreRunsListsAndAttaches checks the whole control path: a program runs,
// its output reaches the screen, and a viewer gets that screen and then the
// program's next bytes.
func TestCoreRunsListsAndAttaches(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "printf ready; sleep 30"}, Kind: "shell", Dir: t.TempDir(), Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID == 0 || !rec.Alive {
		t.Fatalf("record = %+v", rec)
	}

	v, err := attachView(t, client, rec.ID, 40, 10)
	if err != nil {
		t.Fatal(err)
	}
	if cols, rows := v.term.Size(); cols != 40 || rows != 10 {
		t.Fatalf("view size = %dx%d", cols, rows)
	}
	waitFor(t, "the program's output", func() bool { return strings.Contains(v.text(), "ready") })

	list, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ID != rec.ID {
		t.Fatalf("list = %+v", list)
	}
	if list.Instance != client.Instance() {
		t.Fatalf("instance = %q, connection saw %q", list.Instance, client.Instance())
	}
}

// TestCoreStreamsInputAndResize checks the viewer's side of the stream.
func TestCoreStreamsInputAndResize(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh"}, Kind: "shell", Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	v, err := attachView(t, client, rec.ID, 40, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.stream.Write([]byte("echo wada-core-test\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the shell's echo", func() bool {
		return strings.Count(v.text(), "wada-core-test") >= 2 // the command and its output
	})

	if err := v.stream.ResizePixels(52, 14, 8, 16); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the new size", func() bool {
		list, err := client.List()
		return err == nil && len(list.Terminals) == 1 && list.Terminals[0].Cols == 52 && list.Terminals[0].Rows == 14
	})
	// The core's terminal took the size, and the window's took it from the
	// core, as frames of that size need.
	waitFor(t, "the view to take the size", func() bool {
		cols, rows := v.term.Size()
		return cols == 52 && rows == 14
	})
}

// TestAttachCarriesStatus checks that a window attaching after the fact sees
// the program's OSC 7501 record, not only its cells.
func TestAttachCarriesStatus(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	script := "printf '\\033]7501;state=working:app=cargo:progress=40\\033\\\\'; sleep 30"
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", script}, Kind: "shell", Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the status report", func() bool {
		list, err := client.List()
		return err == nil && len(list.Terminals) == 1 && list.Terminals[0].Reported
	})
	v, err := attachView(t, client, rec.ID, 40, 10)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first info", func() bool {
		info, ok := v.info()
		return ok && info.Reported && info.Status != nil && info.Status.State == "working" && info.Status.Progress == 40
	})
}

// TestAgentRedrawInPlace checks an agent's interface as pi draws it: a cursor
// marker in an APC that BEL ends, then a redraw that moves up and erases the
// lines it drew. The window shows the redraw, with nothing of the old frame
// left and no escape sequence printed as text.
func TestAgentRedrawInPlace(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	script := `printf '\033[38;2;72;78;82m|\033[0m old command line\r\n> typed\033_pi:c\007\033[7m \033[27m'; sleep 0.2; ` +
		`printf '\033[?2026h\r\033[1A\033[2K\033[38;2;72;78;82m|\033[0m new\r\n\033[2K> x\033_pi:c\007\033[?2026l'; sleep 30`
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", script}, Kind: "shell", Cols: 40, Rows: 6})
	if err != nil {
		t.Fatal(err)
	}
	v, err := attachView(t, client, rec.ID, 40, 6)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the redraw", func() bool { return strings.Contains(v.text(), "> x") })
	text := v.text()
	for _, stale := range []string{"old command", "typed", "72;78", "pi:c"} {
		if strings.Contains(text, stale) {
			t.Fatalf("the view shows %q after the redraw:\n%s", stale, text)
		}
	}
	if !strings.Contains(text, "| new") {
		t.Fatalf("the view lost the redrawn line:\n%s", text)
	}
}

// TestCoreKeepsTerminalAfterDetach checks that a viewer going away does not
// end the program: a window closing is not a pane closing.
func TestCoreKeepsTerminalAfterDetach(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "printf once; sleep 30"}, Kind: "shell", Cols: 20, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	first, err := attachView(t, client, rec.ID, 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	first.close()

	// The program is still there, and a new viewer sees its screen.
	waitFor(t, "the terminal to stay alive", func() bool {
		list, err := client.List()
		return err == nil && len(list.Terminals) == 1 && list.Terminals[0].Alive
	})
	waitFor(t, "the screen to survive the detach", func() bool {
		v, err := attachView(t, client, rec.ID, 20, 5)
		if err != nil {
			return false
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(v.text(), "once") {
			time.Sleep(20 * time.Millisecond)
		}
		return strings.Contains(v.text(), "once")
	})
}

// TestCoreKillEndsTheTerminal checks the explicit end of a program.
func TestCoreKillEndsTheTerminal(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Kind: "shell", Cols: 20, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Kill(rec.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the terminal to end", func() bool {
		list, err := client.List()
		return err == nil && len(list.Terminals) == 1 && !list.Terminals[0].Alive
	})
}

// TestOneCorePerFolder checks that a second core refuses the same data folder
// rather than taking the socket from the first.
func TestOneCorePerFolder(t *testing.T) {
	dir, err := os.MkdirTemp("", "wada-core-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	first, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Listen(dir); err == nil {
		t.Fatal("a second core took the same data folder")
	}
	if _, err := os.Stat(filepath.Join(dir, InstanceName)); err != nil {
		t.Fatalf("no instance file: %v", err)
	}
	if _, err := os.Stat(SocketPath(dir)); err != nil {
		t.Fatalf("no socket: %v", err)
	}
}

// TestEnsureStartsACore checks the window's side of startup: no core on the
// folder, Ensure starts this executable as one, and it answers.
func TestEnsureStartsACore(t *testing.T) {
	dir, err := os.MkdirTemp("", "wada-ensure-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	client, err := Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if client.Instance() == "" {
		t.Fatal("the started core has no instance")
	}
	raw, err := os.ReadFile(filepath.Join(dir, InstanceName))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		PID int `json:"pid"`
	}
	if err := jsonUnmarshal(raw, &info); err != nil || info.PID == 0 {
		t.Fatalf("core.json = %s (%v)", raw, err)
	}
	process, err := os.FindProcess(info.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatalf("stopping the test core: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Connect(dir); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the test core did not stop")
}

// TestCoreNamesTerminalsAndSendsInput checks the CLI's side: a name in the
// record, and input that reaches the program.
func TestCoreNamesTerminalsAndSendsInput(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh"}, Kind: "shell", Name: "dev", Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "dev" {
		t.Fatalf("record name = %q, want dev", rec.Name)
	}
	if err := client.Send(rec.ID, []byte("echo sent-by-core\r")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the shell to run the sent line", func() bool {
		read, err := client.Read(rec.ID, 20)
		if err != nil {
			return false
		}
		return strings.Contains(strings.Join(read.Lines, "\n"), "sent-by-core")
	})
}

// TestCoreReadsAFinishedTerminal checks that a read after the program ended
// still finds its output, and that the exit is reported.
func TestCoreReadsAFinishedTerminal(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "printf bye; exit 3"}, Kind: "shell", Cols: 20, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the program to end", func() bool {
		list, err := client.List()
		return err == nil && len(list.Terminals) == 1 && !list.Terminals[0].Alive
	})
	read, err := client.Read(rec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if read.Alive {
		t.Fatal("a read of an ended terminal says it is alive")
	}
	if !strings.Contains(strings.Join(read.Lines, "\n"), "bye") {
		t.Fatalf("read = %q, want the program's output", read.Lines)
	}
}

// TestCoreGivesATerminalItsAddress checks the environment a program gets: its
// own address, and the address of the terminal that started it.
func TestCoreGivesATerminalItsAddress(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", `printf '%s\n%s\n' "$WADA_TERMINAL_ID" "$WADA_PARENT_ID"`},
		Kind: "shell", Parent: 3, Cols: 20, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the program to print its address", func() bool {
		read, err := client.Read(rec.ID, 10)
		return err == nil && strings.Contains(strings.Join(read.Lines, "\n"), "t3")
	})
	read, _ := client.Read(rec.ID, 10)
	text := strings.Join(read.Lines, "\n")
	if !strings.Contains(text, address(rec.ID)) {
		t.Fatalf("the program did not see its own address: %q", text)
	}
	if !strings.Contains(text, "t3") {
		t.Fatalf("the program did not see its parent: %q", text)
	}
}

func TestTerminalUsesItsCoreDataDirectory(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	t.Setenv("WADATERM_DATA_DIR", "/wrong/inherited/directory")
	first, firstClient := testCore(t)
	second, secondClient := testCore(t)
	for _, tc := range []struct {
		server *Server
		client *Client
	}{{first, firstClient}, {second, secondClient}} {
		rec, err := tc.client.Start(Start{Argv: []string{"/bin/sh", "-c", `printf '%s\n%s\n' "$WADATERM_CONTEXT_DATA_DIR" "${WADATERM_DATA_DIR:-no-override}"`}, Cols: 200, Rows: 5})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the program to print its own core data directory", func() bool {
			read, err := tc.client.Read(rec.ID, 10)
			text := strings.Join(read.Lines, "\n")
			return err == nil && strings.Contains(text, tc.server.dir) && strings.Contains(text, "no-override")
		})
	}
}

// TestChildExitNoteReachesParent checks the small helper channel: a child that
// asked to notify types a tagged note into the terminal that started it.
func TestChildExitNoteReachesParent(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	parent, err := client.Start(Start{Argv: []string{"/bin/sh"}, Kind: "shell", Name: "boss", Cols: 60, Rows: 12})
	if err != nil {
		t.Fatal(err)
	}
	child, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "exit 5"}, Kind: "shell",
		Name: "kid", Parent: parent.ID, Notify: true, Cols: 20, Rows: 4})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the child to end", func() bool {
		read, err := client.Read(child.ID, 1)
		return err == nil && !read.Alive
	})
	waitFor(t, "the note in the parent's input", func() bool {
		read, err := client.Read(parent.ID, 30)
		if err != nil {
			return false
		}
		text := strings.Join(read.Lines, "\n")
		return strings.Contains(text, `<wada from="t`+strconv.Itoa(child.ID)+` kid" exit="5">finished</wada>`)
	})
}

// TestCoreEvents checks the watch stream: ready, created, a status change and
// the exit.
func TestCoreEvents(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	events, err := client.Events()
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	ready, err := events.Next()
	if err != nil || ready.Type != "ready" {
		t.Fatalf("first event = %+v, %v", ready, err)
	}
	script := "sleep 0.1; printf '\\033]7501;state=working:app=cargo\\033\\\\'; sleep 30"
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", script}, Kind: "shell", Cols: 30, Rows: 6})
	if err != nil {
		t.Fatal(err)
	}
	created, err := events.Next()
	if err != nil || created.Type != "created" || created.Terminal != rec.ID {
		t.Fatalf("created = %+v, %v", created, err)
	}
	changed, err := events.Next()
	if err != nil || changed.Type != "changed" || changed.Record == nil || changed.Record.Word != "working" {
		t.Fatalf("changed = %+v, %v", changed, err)
	}
	if err := client.Kill(rec.ID); err != nil {
		t.Fatal(err)
	}
	exited, err := events.Next()
	if err != nil || exited.Type != "exit" || exited.Record == nil || exited.Record.Alive {
		t.Fatalf("exit = %+v, %v", exited, err)
	}
}

// TestBlockedChildNotifiesParent checks that a helper which reports blocked
// types a note into the terminal that started it.
func TestBlockedChildNotifiesParent(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	_, client := testCore(t)
	parent, err := client.Start(Start{Argv: []string{"/bin/sh"}, Kind: "shell", Name: "boss", Cols: 60, Rows: 14})
	if err != nil {
		t.Fatal(err)
	}
	script := "sleep 0.2; printf '\\033]7501;state=blocked:kind=permission:msg=QXBwcm92ZSB0aGUgZWRpdD8=\\033\\\\'; sleep 5"
	child, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", script}, Kind: "shell",
		Name: "kid", Parent: parent.ID, Notify: true, Cols: 20, Rows: 4})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the blocked note in the parent", func() bool {
		read, err := client.Read(parent.ID, 30)
		if err != nil {
			return false
		}
		text := strings.Join(read.Lines, "\n")
		return strings.Contains(text, `state="blocked"`) &&
			strings.Contains(text, "Approve the edit") &&
			strings.Contains(text, `from="t`+strconv.Itoa(child.ID)+` kid"`)
	})
}

// TestCommandTransportThroughProxy checks the transport a machine uses: a
// command carrying the core protocol over its pipes, with the proxy bridging
// to the Unix socket. SSH is only the command that runs it.
func TestCommandTransportThroughProxy(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	dir, err := os.MkdirTemp("", "wada-proxy-")
	if err != nil {
		t.Fatal(err)
	}
	server, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close(); _ = os.RemoveAll(dir) })
	transport := CommandTransport{
		Args: []string{os.Args[0], "--proxy"},
		Env:  withEnv(os.Environ(), "WADA_PROXY_DIR", dir),
	}
	client, err := ConnectTransport(transport)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "printf remote; sleep 2"}, Kind: "shell", Cols: 30, Rows: 6})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the remote terminal's output", func() bool {
		read, err := client.Read(rec.ID, 5)
		return err == nil && strings.Contains(strings.Join(read.Lines, "\n"), "remote")
	})
	stream, err := client.Attach(rec.ID, 20, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stream.Cols != 20 || stream.Rows != 4 {
		t.Fatalf("attached at %dx%d", stream.Cols, stream.Rows)
	}
	stream.Close()
}

// TestQuestions checks a helper's question reaches its parent and its answer
// comes back through the core, with cancel and validation.
func TestQuestions(t *testing.T) {
	if !hasPTY() {
		t.Skip("no pseudo-terminals on this machine")
	}
	dir, err := os.MkdirTemp("", "wada-questions-")
	if err != nil {
		t.Fatal(err)
	}
	server, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close(); _ = os.RemoveAll(dir) })
	client, err := Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := client.Start(Start{Argv: []string{"/bin/cat"}, Kind: "shell", Cols: 120, Rows: 8})
	if err != nil {
		t.Fatal(err)
	}
	child, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "sleep 10"}, Kind: "shell",
		Parent: parent.ID, Notify: true, Cols: 40, Rows: 8})
	if err != nil {
		t.Fatal(err)
	}
	question, err := client.Ask(child.ID, parent.ID, "Which file should I edit?")
	if err != nil {
		t.Fatal(err)
	}
	if question.ID == "" || question.Child != child.ID || question.Parent != parent.ID {
		t.Fatalf("question = %+v", question)
	}
	waitFor(t, "the question in the parent's input", func() bool {
		read, err := client.Read(parent.ID, 20)
		text := strings.Join(read.Lines, "\n")
		return err == nil && strings.Contains(text, `question="`+question.ID+`"`) &&
			strings.Contains(text, "Which file should I edit?")
	})
	open, err := client.Questions()
	if err != nil || len(open) != 1 || open[0].Answer != "" {
		t.Fatalf("open questions = %+v, %v", open, err)
	}
	answered, err := client.AnswerQuestion(question.ID, "src/x.go")
	if err != nil {
		t.Fatal(err)
	}
	if answered.Answer != "src/x.go" || answered.Answered == 0 {
		t.Fatalf("answered = %+v", answered)
	}
	if _, err := client.AnswerQuestion(question.ID, "again"); err == nil {
		t.Fatal("a question was answered twice")
	}
	if _, err := client.Ask(child.ID, parent.ID, "  "); err == nil {
		t.Fatal("an empty question was accepted")
	}

	second, err := client.Ask(child.ID, parent.ID, "And the test?")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CancelQuestion(second.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := client.Question(second.ID)
	if err != nil || !cancelled.Closed || cancelled.Answer != "" {
		t.Fatalf("cancelled = %+v, %v", cancelled, err)
	}

	alone, err := client.Start(Start{Argv: []string{"/bin/sh", "-c", "sleep 2"}, Kind: "shell", Cols: 20, Rows: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Ask(alone.ID, 0, "Anyone?"); err == nil {
		t.Fatal("a terminal with no parent asked successfully")
	}

	third, err := client.Ask(child.ID, parent.ID, "One more?")
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan Question, 1)
	go func() {
		question, err := client.WaitQuestion(third.ID, 5*time.Second)
		if err == nil {
			waited <- question
		}
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := client.AnswerQuestion(third.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	select {
	case question := <-waited:
		if question.Answer != "yes" {
			t.Fatalf("waited = %+v", question)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("WaitQuestion did not wake for the answer")
	}
}

// TestTerminalLabels checks labels survive the start request and a list.
func TestTerminalLabels(t *testing.T) {
	w, client := testCore(t)
	_ = w
	rec, err := client.Start(Start{Argv: []string{"/bin/sleep", "5"}, Kind: "shell",
		Cols: 20, Rows: 5, Labels: map[string]string{"space": "review", "tab": "fix"}})
	if err != nil {
		t.Fatal(err)
	}
	list, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range list.Terminals {
		if got.ID == rec.ID {
			if got.Labels["space"] != "review" || got.Labels["tab"] != "fix" {
				t.Fatalf("labels = %+v", got.Labels)
			}
			return
		}
	}
	t.Fatalf("terminal %d is not in %+v", rec.ID, list)
}

// TestLayoutDocument checks the core stores a spaces document and keeps it
// across a restart.
func TestLayoutDocument(t *testing.T) {
	dir, err := os.MkdirTemp("", "wada-layout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	server, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{"spaces":[{"name":"review","tabs":[{"root":{"pane":{"terminal":3}}}]}]}`)
	if err := client.SetLayout(doc); err != nil {
		t.Fatal(err)
	}
	got, err := client.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(doc) {
		t.Fatalf("layout = %s", got)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	server, err = Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err = Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, rev, err := client.LayoutVersioned()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(doc) || rev == 0 {
		t.Fatalf("layout after restart = %s (revision %d)", got, rev)
	}
	next := []byte(`{"spaces":[{"name":"next"}]}`)
	if err := client.SetLayoutAt(next, rev); err != nil {
		t.Fatal(err)
	}
	if err := client.SetLayoutAt(doc, rev); err == nil {
		t.Fatal("a stale revision overwrote the newer document")
	}
}

// TestForegroundName reports the job in front of the shell. Some systems
// without job control leave it empty; those skip rather than fail.
func TestForegroundName(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skip("no pseudo-terminals")
	}
	_, client := testCore(t)
	rec, err := client.Start(Start{Argv: []string{"/bin/sh", "-i"}, Kind: "shell",
		Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Send(rec.ID, []byte("sleep 3\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := client.List()
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range list.Terminals {
			if got.ID == rec.ID && got.Foreground != "" {
				if !strings.Contains(got.Foreground, "sleep") && !strings.Contains(got.Foreground, "sh") {
					t.Fatalf("foreground = %q", got.Foreground)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Skip("this shell did not expose a foreground job")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
