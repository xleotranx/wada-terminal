package terminal

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	mterm "github.com/egoist/mygo/plugins/terminal"
	"wada-terminal/internal/control"
)

// recordConn is a terminal's Conn that keeps what the terminal sent its
// program.
type recordConn struct {
	idleConn
	mu   sync.Mutex
	sent bytes.Buffer
}

func (c *recordConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent.Write(p)
}

func (c *recordConn) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent.String()
}

// recordedSession is a live session whose terminal records its input.
func recordedSession(t *testing.T, id int) (*session, *recordConn) {
	t.Helper()
	conn := &recordConn{idleConn: idleConn{closed: make(chan struct{})}}
	term, err := mterm.New(mterm.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	s := testSession(kindShell, id)
	s.term = term
	return s, conn
}

// waitFor waits until a terminal has passed its input on to the program.
func waitFor(t *testing.T, conn *recordConn, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(conn.text(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the program got %q, want %q", conn.text(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// sendWindow is a window showing a tab of two panes, 11 focused and 12 not,
// and a second space with pane 13 that is not on show.
type sendWindow struct {
	p                *Plugin
	focused, beside  *session
	away             *session
	focusedIn        *recordConn
	besideIn, awayIn *recordConn
}

func newSendWindow(t *testing.T) sendWindow {
	t.Helper()
	var sw sendWindow
	sw.focused, sw.focusedIn = recordedSession(t, 11)
	sw.beside, sw.besideIn = recordedSession(t, 12)
	sw.away, sw.awayIn = recordedSession(t, 13)
	root, ok := splitAt(leafOf(sw.focused), sw.focused, sw.beside, true)
	if !ok {
		t.Fatal("the panes did not split")
	}
	shown := &workspace{id: 1, tabs: []*tab{{root: root, focus: sw.focused}}, focus: sw.focused}
	hidden := &workspace{id: 2, tabs: []*tab{{root: leafOf(sw.away), focus: sw.away}}, focus: sw.away}
	sw.p = &Plugin{workspaces: []*workspace{shown, hidden}, current: shown}
	return sw
}

// send runs terminal.send the way the control socket does, which delivers a
// JSON number as a float64.
func (sw sendWindow) send(args map[string]any) control.Response {
	return sw.p.controlResponse(control.Request{Method: "do", Action: "terminal.send", Args: args})
}

// TestSendReachesTheNamedPane checks that text goes to the pane the caller
// named, focused or not, and to no other: a line sent to the wrong pane could
// answer another agent's approval prompt.
func TestSendReachesTheNamedPane(t *testing.T) {
	sw := newSendWindow(t)
	for _, tc := range []struct {
		name string
		id   int
		in   *recordConn
	}{
		{"unfocused in the same tab", 12, sw.besideIn},
		{"in a space not on show", 13, sw.awayIn},
		{"focused", 11, sw.focusedIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := "for " + tc.name
			resp := sw.send(map[string]any{"terminal": float64(tc.id), "text": text})
			if resp.Error != "" {
				t.Fatalf("send failed: %s", resp.Error)
			}
			waitFor(t, tc.in, text+"\r")
			for _, in := range []*recordConn{sw.focusedIn, sw.besideIn, sw.awayIn} {
				if in != tc.in && strings.Contains(in.text(), text) {
					t.Fatalf("another pane got the text: %q", in.text())
				}
			}
		})
	}
}

// TestSendLeavesTheFocusAlone checks that sending to a pane does not move the
// keyboard: the person may be typing in the focused one.
func TestSendLeavesTheFocusAlone(t *testing.T) {
	sw := newSendWindow(t)
	if resp := sw.send(map[string]any{"terminal": float64(12), "text": "hi"}); resp.Error != "" {
		t.Fatalf("send failed: %s", resp.Error)
	}
	if sw.p.current.focus != sw.focused {
		t.Fatalf("the focus moved to pane %d", sw.p.current.focus.id)
	}
}

// TestSendRefusesAnUnnamedTarget checks every way of not naming a real pane:
// each is an error, and none falls back to the focused pane.
func TestSendRefusesAnUnnamedTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown id", map[string]any{"terminal": float64(99), "text": "secret"}, "no pane 99"},
		{"no id", map[string]any{"text": "secret"}, "needs --terminal"},
		{"null id", map[string]any{"terminal": nil, "text": "secret"}, "needs --terminal"},
		{"not a number", map[string]any{"terminal": "t7", "text": "secret"}, "is a number"},
		{"the other name", map[string]any{"pane": float64(12), "text": "secret"}, "does not take"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sw := newSendWindow(t)
			resp := sw.send(tc.args)
			if !strings.Contains(resp.Error, tc.want) {
				t.Fatalf("error = %q, want %q", resp.Error, tc.want)
			}
			// A terminal passes its input on in order, so once this has
			// arrived anything the refused send wrote would have too.
			for _, s := range []*session{sw.focused, sw.beside, sw.away} {
				s.write([]byte("marker"))
			}
			for _, in := range []*recordConn{sw.focusedIn, sw.besideIn, sw.awayIn} {
				waitFor(t, in, "marker")
				if got := in.text(); got != "marker" {
					t.Fatalf("a pane got text from a refused send: %q", got)
				}
			}
		})
	}
}

// TestSendHandlerNeedsATarget checks the handler on its own: a caller that
// reaches it without the action list's validation still gets no fallback.
func TestSendHandlerNeedsATarget(t *testing.T) {
	sw := newSendWindow(t)
	if _, err := sw.p.controlDo("terminal.send", map[string]any{"text": "secret"}); err == nil {
		t.Fatal("a send with no target was accepted")
	}
	sw.focused.write([]byte("marker"))
	waitFor(t, sw.focusedIn, "marker")
	if got := sw.focusedIn.text(); got != "marker" {
		t.Fatalf("the focused pane got %q", got)
	}
}
