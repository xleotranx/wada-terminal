package terminal

import (
	"testing"

	"wada-terminal/internal/status"
)

// report7501 reads one OSC 7501 report out of a program's output, as a
// session's reader does, and applies it.
func report7501(s *session, body string) {
	var scan status.Stream
	scan.Write([]byte("\x1b]7501;" + body + "\x1b\\"))
	s.applyStatusReports(scan.TakeReports())
}

// TestProtocolIsBelievedOverTheScreen checks that a program which reports its
// own state is an agent, even when it started as a shell, and that its word
// drives the state the window shows.
func TestProtocolIsBelievedOverTheScreen(t *testing.T) {
	s := testSession(kindShell, 1)
	report7501(s, "state=working:app=cargo:progress=40")
	if !s.isAgent() {
		t.Fatal("a shell that reports OSC 7501 is not an agent")
	}
	if s.agentState() != agentWorking || s.stateLabel() != "Working" {
		t.Fatalf("state = %v (%q), want working", s.agentState(), s.stateLabel())
	}
	if got := s.statusText(); got != "40%" {
		t.Fatalf("statusText() = %q, want 40%%", got)
	}
	if got := s.title(); got != "Cargo" {
		t.Fatalf("title() = %q, want the app's name", got)
	}
	if s.subtitle() != "" {
		t.Fatalf("subtitle() = %q, want none", s.subtitle())
	}

	// It finished away from the keyboard: that is Done until the user looks.
	report7501(s, "state=done:msg=QnVpbHQ=")
	if s.agentState() != agentDone {
		t.Fatalf("an unseen finish is %v, want done", s.agentState())
	}
	if s.subtitle() != "Built" {
		t.Fatalf("subtitle() = %q, want the protocol's message", s.subtitle())
	}
	(&Plugin{}).seen(s)
	if s.agentState() != agentIdle {
		t.Fatalf("a seen finish is %v, want idle", s.agentState())
	}
}

// TestProtocolKindNamesWhatABlockedAgentNeeds checks the three kinds the
// protocol defines, and that they colour the label the pane and index show.
func TestProtocolKindNamesWhatABlockedAgentNeeds(t *testing.T) {
	cases := []struct{ kind, want string }{
		{"permission", "Needs approval"},
		{"question", "Needs answer"},
		{"auth", "Needs login"},
		{"", "Needs input"},
	}
	for _, tc := range cases {
		s := testSession(kindClaude, 1)
		report7501(s, "state=blocked:kind="+tc.kind)
		if got := s.stateLabel(); got != tc.want {
			t.Errorf("kind %q: stateLabel() = %q, want %q", tc.kind, got, tc.want)
		}
		if s.agentState() != agentBlocked {
			t.Errorf("kind %q: state = %v, want blocked", tc.kind, s.agentState())
		}
	}
	// Error counts as something the user must deal with, as herdr shows it.
	s := testSession(kindClaude, 2)
	report7501(s, "state=error")
	if s.agentState() != agentBlocked {
		t.Fatalf("error is %v, want blocked", s.agentState())
	}
}

// TestProtocolSeenRule checks that watching a finish keeps it idle, while the
// same finish in a pane without the keyboard becomes done.
func TestProtocolSeenRule(t *testing.T) {
	s := testSession(kindClaude, 1)
	plug := &Plugin{current: &workspace{focus: s}}
	s.t = plug // focused, and with no host the window counts as in front

	report7501(s, "state=working")
	report7501(s, "state=idle")
	if s.agentState() != agentIdle {
		t.Fatalf("a finish the user watched is %v, want idle", s.agentState())
	}

	report7501(s, "state=working")
	s.t.current.focus = nil // the user looked elsewhere
	report7501(s, "state=idle")
	if s.agentState() != agentDone {
		t.Fatalf("a finish the user missed is %v, want done", s.agentState())
	}
}

// TestProtocolChildReportDoesNotChangeTheRoot checks that a program reporting
// on one part of its work is an agent, but its own state stays what it was.
func TestProtocolChildReportDoesNotChangeTheRoot(t *testing.T) {
	s := testSession(kindShell, 1)
	report7501(s, "state=working")
	report7501(s, "state=blocked:id=deploy/us-east:app=aws")
	if s.agentState() != agentWorking {
		t.Fatalf("a child report changed the program's own state: %v", s.agentState())
	}
	if !s.isAgent() {
		t.Fatal("a program with child records is not an agent")
	}
}

// TestProtocolClearWithdrawsAStatus checks that clear removes the state, and
// that a cleared program does not fall back to reading its screen.
func TestProtocolClearWithdrawsAStatus(t *testing.T) {
	s := testSession(kindShell, 1)
	report7501(s, "state=blocked:kind=permission:msg=QXBwbHk/")
	if s.agentState() != agentBlocked {
		t.Fatalf("state = %v, want blocked", s.agentState())
	}
	report7501(s, "state=clear")
	if s.agentState() != agentIdle || s.statusMsg != "" {
		t.Fatalf("after clear: %v, message %q", s.agentState(), s.statusMsg)
	}
	if !s.isAgent() || !s.reported {
		t.Fatal("clear forgot that the program speaks the protocol")
	}
}

// TestProtocolSuppressesTheScreenWatcher checks that the quiet watcher never
// moves an agent that reports: only the program decides when it is done.
func TestProtocolSuppressesTheScreenWatcher(t *testing.T) {
	s := testSession(kindClaude, 1)
	plug := &Plugin{}
	plug.agentOutput(s) // a quiet spell starts before the report arrives
	s.busy = true
	report7501(s, "state=working")
	if s.busy {
		t.Fatal("a report left the screen-reading busy flag set")
	}
	if s.settle(false) {
		t.Fatal("the quiet watcher changed a reporting agent's state")
	}
	s.busy = true
	s.settle(true)
	if s.agentState() != agentWorking {
		t.Fatalf("settle changed a reporting agent to %v", s.agentState())
	}
}
