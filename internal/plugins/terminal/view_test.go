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
