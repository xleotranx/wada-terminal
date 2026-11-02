package terminal

import (
	"fmt"
	"testing"

	"wada-terminal/internal/vt"
)

// testSession is a session with a screen but no program: enough to read the
// Sessions's state and its screen.
func testSession(k kind, id int) *session {
	sp := k.spec()
	return &session{id: id, kind: k, name: sp.name, detail: sp.detail, alive: true,
		cols: 60, rows: 12, screen: vt.New(60, 12), wordProgress: -1}
}

// TestBlockedOnApprovalPrompt checks that an approval with a cursor on it is
// blocked, and that prose or an idle prompt is not.
func TestBlockedOnApprovalPrompt(t *testing.T) {
	cases := []struct {
		name   string
		screen string
		want   bool
	}{
		{
			name:   "question with choices",
			screen: "Do you want to proceed?\r\n\r\n❯ 1. Yes\r\n  2. No\r\n",
			want:   true,
		},
		{
			name:   "numbered choices only",
			screen: "Which change should I make?\r\n❯ 1. Yes, apply it\r\n  2. No, keep it\r\n",
			want:   true,
		},
		{
			name:   "a working prompt",
			screen: "● Reading src/main.go…\r\n\r\n> \r\n",
			want:   false,
		},
		{
			name:   "an answer about wanting",
			screen: "Do you want to know what the code does?\r\nIt reads a file and prints it.\r\n> \r\n",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			screen := vt.New(60, 12)
			screen.Write([]byte(tc.screen))
			if got := blockedOnScreen(screen); got != tc.want {
				t.Fatalf("blockedOnScreen() = %v, want %v for screen:\n%s", got, tc.want, tc.screen)
			}
		})
	}
}

// TestSettleClassifiesAnAgent checks the states herdr calls idle, done and
// blocked: a quiet prompt the user watched is idle, the same prompt in
// another pane is done, and an approval is blocked.
func TestSettleClassifiesAnAgent(t *testing.T) {
	s := testSession(kindClaude, 1)
	s.screen.Write([]byte("Ready.\r\n> \r\n"))
	s.busy, s.asked = true, true
	if !s.settle(false) || s.agentState() != agentDone {
		t.Fatalf("a quiet agent away from the keyboard is %v, want done", s.agentState())
	}
	if s.stateLabel() != "Done" {
		t.Fatalf("stateLabel() = %q, want Done", s.stateLabel())
	}

	s = testSession(kindCodex, 2)
	s.screen.Write([]byte("Ready.\r\n> \r\n"))
	s.busy, s.asked = true, true
	s.settle(true)
	if s.agentState() != agentIdle || s.asked || s.done {
		t.Fatalf("a quiet agent the user watched is %+v, want idle and seen", s.agentState())
	}

	s = testSession(kindClaude, 3)
	s.screen.Write([]byte("Allow this command?\r\n❯ 1. Yes\r\n  2. No\r\n"))
	s.busy, s.asked = true, true
	s.settle(false)
	if s.agentState() != agentBlocked {
		t.Fatalf("an approval is %v, want blocked", s.agentState())
	}

	// Output after a prompt starts a new working spell.
	s = testSession(kindClaude, 4)
	s.done, s.asked = true, true
	plug := &Plugin{}
	plug.agentOutput(s)
	if s.agentState() != agentWorking || s.done {
		t.Fatalf("output left %v (done %v), want working", s.agentState(), s.done)
	}
}

// TestShellStateIsRunning checks that a shell has no agent states to report.
func TestShellStateIsRunning(t *testing.T) {
	s := testSession(kindShell, 1)
	s.busy = true
	s.settle(false)
	if s.agentState() != agentIdle || s.stateLabel() != "Running" {
		t.Fatalf("a shell is %v (%q), want running", s.agentState(), s.stateLabel())
	}
	s.alive = false
	if s.stateLabel() != "Stopped" {
		t.Fatalf("a stopped shell is %q, want Stopped", s.stateLabel())
	}
}

// TestAttentionPutsBlockedFirst checks the rollup that marks a workspace, and
// the count the window shows: a blocked agent outranks one that finished.
func TestAttentionPutsBlockedFirst(t *testing.T) {
	blocked := testSession(kindClaude, 1)
	blocked.blocked = true
	done := testSession(kindCodex, 2)
	done.done = true
	working := testSession(kindOpenCode, 3)
	working.busy = true
	shell := testSession(kindShell, 4)

	w := &workspace{tabs: []*tab{{root: &node{split: &branch{a: leafOf(blocked), b: leafOf(done), vertical: true}}},
		{root: leafOf(working)}, {root: leafOf(shell)}}}
	if st, n := w.rollup(); st != agentBlocked || n != 1 {
		t.Fatalf("rollup() = %v, %d; want blocked, 1", st, n)
	}
	tb := &Plugin{workspaces: []*workspace{w}}
	if st, n := tb.attention(); st != agentBlocked || n != 2 {
		t.Fatalf("attention() = %v, %d; want blocked, 2", st, n)
	}

	blocked.blocked = false
	if st, n := w.rollup(); st != agentDone || n != 1 {
		t.Fatalf("rollup() = %v, %d; want done, 1", st, n)
	}
	done.done = false
	if st, n := w.rollup(); st != agentWorking || n != 1 {
		t.Fatalf("rollup() = %v, %d; want working, 1", st, n)
	}
}

// TestIndexOrdersSessions checks that the index lists every session in the
// order the window draws them, and opens on the first that needs the user.
func TestIndexOrdersSessions(t *testing.T) {
	a := testSession(kindClaude, 1)
	b := testSession(kindShell, 2)
	c := testSession(kindCodex, 3)
	c.done = true
	d := testSession(kindOpenCode, 4)
	d.busy = true

	wa := &workspace{tabs: []*tab{{root: leafOf(a)}}}
	wb := &workspace{tabs: []*tab{{root: leafOf(b)},
		{root: &node{split: &branch{a: leafOf(c), b: leafOf(d), vertical: true}}}}}
	tb := &Plugin{workspaces: []*workspace{wa, wb}, current: wb}
	entries := tb.indexEntries()
	if len(entries) != 4 {
		t.Fatalf("indexEntries() has %d entries, want 4", len(entries))
	}
	for i, want := range []*session{a, b, c, d} {
		if entries[i].s != want {
			t.Fatalf("entry %d is session %d, want %d", i, entries[i].s.id, want.id)
		}
	}
	if got := tb.firstAttention(entries); got != 2 {
		t.Fatalf("firstAttention() = %d, want the finished Codex at 2", got)
	}
	a.blocked = true
	if got := tb.firstAttention(entries); got != 0 {
		t.Fatalf("firstAttention() = %d, want the blocked Claude at 0", got)
	}
}

// TestSeenClearsADoneAgent checks that opening a finished agent's pane takes
// its done badge away.
func TestSeenClearsADoneAgent(t *testing.T) {
	s := testSession(kindClaude, 1)
	s.done, s.asked = true, true
	(&Plugin{}).seen(s)
	if s.done || s.asked || s.agentState() != agentIdle {
		t.Fatalf("seen() left %v, asked %v; want idle", s.agentState(), s.asked)
	}
}

// TestScreenTailReadsTheBottom checks that the state machine reads the live
// bottom of a screen, not its scrollback.
func TestScreenTailReadsTheBottom(t *testing.T) {
	screen := vt.New(40, 5)
	screen.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n"))
	tail := screenTail(screen, 3)
	want := []string{"four", "five", "six"}
	if len(tail) != len(want) {
		t.Fatalf("screenTail() = %q, want %q", tail, want)
	}
	for i := range want {
		if tail[i] != want[i] {
			t.Fatalf("screenTail() = %q, want %q", tail, want)
		}
	}
}

// TestScreenTailReadsTheLiveBottom checks that the state machine reads what
// the program last drew even when the user has scrolled the pane back.
func TestScreenTailReadsTheLiveBottom(t *testing.T) {
	screen := vt.New(30, 5)
	for i := 0; i < 12; i++ {
		screen.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}
	screen.Scroll(4)
	if screen.Scrolled() == 0 {
		t.Fatal("the test did not scroll the screen")
	}
	tail := screenTail(screen, 1)
	if len(tail) != 1 || tail[0] != "line 11" {
		t.Fatalf("screenTail() = %q, want the live bottom line 11", tail)
	}
}
