package terminal

import (
	"testing"

	"wada-terminal/internal/vt"
)

// TestTabTitleFollowsFocus checks that a tab names the pane on show, not the
// one it opened with.
func TestTabTitleFollowsFocus(t *testing.T) {
	a := &session{name: "Claude Code", screen: vt.New(4, 2)}
	b := &session{name: "Shell", screen: vt.New(4, 2)}
	tb := &tab{root: leafOf(a), focus: a}
	if got := tabTitle(tb); got != "Claude Code" {
		t.Fatalf("tab title = %q", got)
	}
	tb.focus = b
	if got := tabTitle(tb); got != "Shell" {
		t.Fatalf("tab title after focusing the other pane = %q", got)
	}
}

// TestTabTitleWithoutFocus checks the fallback when a tab has no focused pane.
func TestTabTitleWithoutFocus(t *testing.T) {
	a := &session{name: "Claude Code", screen: vt.New(4, 2)}
	tb := &tab{root: leafOf(a)}
	if got := tabTitle(tb); got != "Claude Code" {
		t.Fatalf("tab title = %q", got)
	}
}

// TestFitPathKeepsTheLastFolders checks that a long folder loses its start and
// keeps the end, which is what tells two workspaces apart.
func TestFitPathKeepsTheLastFolders(t *testing.T) {
	const dir = "~/project/wada_labs/wada-terminal"
	cases := []struct {
		path  string
		width int
		want  string
	}{
		{dir, 34, dir},
		{dir, 30, "…/wada_labs/wada-terminal"},
		{dir, 25, "…/wada_labs/wada-terminal"},
		{dir, 24, "…/wada-terminal"},
		{"short", 34, "short"},
		{"~/octo/工作/项目", 8, "…/工作/项目"},
	}
	for _, tc := range cases {
		if got := fitPath(tc.path, tc.width); got != tc.want {
			t.Errorf("fitPath(%q, %d) = %q, want %q", tc.path, tc.width, got, tc.want)
		}
	}
}

// TestWorkspaceDirLine checks the folder under a workspace's name: home becomes
// ~, and a machine comes before the folder.
func TestWorkspaceDirLine(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	cases := []struct {
		dir, machine, want string
	}{
		{"/home/me", "", "~"},
		{"/home/me/project/wada_labs/wada-terminal", "", "…/wada_labs/wada-terminal"},
		{"", "box", "box · remote"},
		{"/home/me/project/wada_labs/wada-terminal", "box", "box · …/wada-terminal"},
	}
	for _, tc := range cases {
		if got := workspaceDirLine(tc.dir, tc.machine); got != tc.want {
			t.Errorf("workspaceDirLine(%q, %q) = %q, want %q", tc.dir, tc.machine, got, tc.want)
		}
	}
}

// TestRollupLabel checks a workspace's agents said in words.
func TestRollupLabel(t *testing.T) {
	cases := []struct {
		st   agentState
		n    int
		want string
	}{
		{agentBlocked, 1, "needs you"},
		{agentBlocked, 2, "2 waiting"},
		{agentDone, 1, "done"},
		{agentDone, 3, "3 done"},
		{agentWorking, 1, "working"},
		{agentIdle, 0, ""},
	}
	for _, tc := range cases {
		if got := rollupLabel(tc.st, tc.n); got != tc.want {
			t.Errorf("rollupLabel(%v, %d) = %q, want %q", tc.st, tc.n, got, tc.want)
		}
	}
}
