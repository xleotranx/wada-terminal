package terminal

import "testing"

// TestSplitAndRemove checks that a layout splits where its pane is and comes
// back together when a pane goes.
func TestSplitAndRemove(t *testing.T) {
	a, b, c := &session{id: 1}, &session{id: 2}, &session{id: 3}
	root := leafOf(a)
	root, ok := splitAt(root, a, b, true)
	if !ok {
		t.Fatal("splitAt did not find the pane")
	}
	if got := len(root.sessions(nil)); got != 2 {
		t.Fatalf("the split shows %d sessions, want 2", got)
	}
	root, ok = splitAt(root, b, c, false)
	if !ok {
		t.Fatal("splitAt did not find the nested pane")
	}
	if got := root.sessions(nil); len(got) != 3 || got[0] != a || got[1] != b || got[2] != c {
		t.Fatalf("sessions = %v, want a, b, c", got)
	}
	root = removeAt(root, b)
	if got := root.sessions(nil); len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("after removing b, sessions = %v, want a, c", got)
	}
	root = removeAt(root, c)
	root = removeAt(root, a)
	if root != nil {
		t.Fatalf("the empty layout is %v, want nil", root)
	}
}

// TestReplace checks that a restarted pane keeps its place in the layout.
func TestReplace(t *testing.T) {
	a, b := &session{id: 1}, &session{id: 2}
	next := &session{id: 3}
	root := leafOf(a)
	root, _ = splitAt(root, a, b, true)
	root = replaceAt(root, b, next)
	got := root.sessions(nil)
	if len(got) != 2 || got[0] != a || got[1] != next {
		t.Fatalf("sessions = %v, want a and the new pane", got)
	}
}

// TestCloseTab checks that closing a tab drops its sessions and keeps the one
// on show sensible.
func TestCloseTab(t *testing.T) {
	a, b, c := &session{id: 1}, &session{id: 2}, &session{id: 3}
	w := &workspace{tabs: []*tab{{root: leafOf(a), focus: a}, {root: leafOf(b), focus: b}, {root: leafOf(c), focus: c}}}
	w.active, w.focus = 1, b
	p := &Plugin{workspaces: []*workspace{w}, current: w}
	p.closeTab(0)
	if len(w.tabs) != 2 || w.active != 0 || w.focus != b {
		t.Fatalf("after closing the first: %d tabs, active %d, focus %v", len(w.tabs), w.active, w.focus != b)
	}
	p.closeTab(1)
	if len(w.tabs) != 1 || w.focus != b {
		t.Fatalf("after closing the last tab: %d tabs, focus %v", len(w.tabs), w.focus != b)
	}
	p.closeTab(0)
	if len(w.tabs) != 0 || w.focus != nil {
		t.Fatalf("after closing every tab: %d tabs, focus %v", len(w.tabs), w.focus)
	}
}

// TestUniqueWorkspaceName checks that a second workspace of the same folder
// gets a name of its own.
func TestUniqueWorkspaceName(t *testing.T) {
	p := &Plugin{workspaces: []*workspace{{name: "home"}, {name: "home 2"}}}
	if got := p.uniqueName("home"); got != "home 3" {
		t.Fatalf("uniqueName(home) = %q", got)
	}
	if got := p.uniqueName("work"); got != "work" {
		t.Fatalf("uniqueName(work) = %q", got)
	}
}

// TestCloseWorkspaceShowsNeighbour checks that closing the workspace on show
// leaves the one beside it on show.
func TestCloseWorkspaceShowsNeighbour(t *testing.T) {
	a := &workspace{name: "a"}
	b := &workspace{name: "b"}
	p := &Plugin{workspaces: []*workspace{a, b}, current: a}
	p.closeWorkspace(a)
	if len(p.workspaces) != 1 || p.current != b {
		t.Fatalf("after closing a: %d workspaces, current %v", len(p.workspaces), p.current == b)
	}
	c := &workspace{name: "c"}
	p.workspaces = append(p.workspaces, c)
	p.current = c
	p.closeWorkspace(c)
	if len(p.workspaces) != 1 || p.current != b {
		t.Fatalf("after closing the last: %d workspaces, current %v", len(p.workspaces), p.current == b)
	}
}
