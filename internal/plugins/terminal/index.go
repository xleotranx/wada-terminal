package terminal

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/native"
)

// entry is one pane in the Sessions list: where it sits, and what it runs.
type entry struct {
	w   *workspace
	tab int
	s   *session
}

// sessionIndex is the window's index of every session it holds, across
// workspaces, tabs and splits: herdr's agent list, for this terminal. It
// opens over the page, and a click or Return gives the chosen pane the
// keyboard.
func (t *Plugin) sessionIndex(c *ui.Context) {
	native.Dialog(c, &t.indexOpen, 640, "Sessions", func() {
		t.sessionIndexBody(c)
	})
}

// sessionIndexBody draws the open index: its title, its rows grouped by
// workspace, and the keys that move through it.
func (t *Plugin) sessionIndexBody(c *ui.Context) {
	ch := chromeFor(c)
	pal := ch.pal
	entries := t.indexEntries()
	if t.indexSel >= len(entries) {
		t.indexSel = len(entries) - 1
	}
	if t.indexSel < 0 {
		t.indexSel = 0
	}

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, "Sessions").FontSize(18).FontWeight(600).TextColor(pal.Text)
		ui.Box(c).Grow(1).MinWidth(0)
		t.attentionPill(c, ch)
	})
	ui.Text(c, "Every session in this window, with what it is doing. The one that needs you is chosen.").
		FontSize(11).TextColor(pal.Muted).SingleLine().Ellipsis("…")

	if len(entries) == 0 {
		ui.Column(c).FillWidth().Height(120).Center().Gap(8).Children(func() {
			ui.Text(c, "No sessions open").FontSize(13).TextColor(pal.Muted)
			keyHint(c, ch, "⌘T", "opens a tab")
		})
		return
	}

	reveal := t.indexReveal
	t.indexReveal = false
	var chosen ui.Element
	list := ui.Scroll(c).FillWidth().MaxHeight(400).Shrink(0).Focusable().Focus().
		HandleInput(func(ev ui.InputEvent) bool { return t.indexKey(ev) })
	list.Children(func() {
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			var last *workspace
			for i, e := range entries {
				if e.w != last {
					t.indexGroup(c, ch, e.w)
					last = e.w
				}
				row := t.indexRow(c, ch, e, i)
				if i == t.indexSel {
					chosen = row
				}
			}
		})
	})
	if reveal {
		chosen.ScrollIntoView()
	}

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		keyHint(c, ch, "↑↓", "move")
		keyHint(c, ch, "↵", "open")
		keyHint(c, ch, "esc", "close")
		ui.Box(c).Grow(1).MinWidth(0)
		ui.Text(c, indexSummary(entries)).FontSize(10).TextColor(pal.Faint).SingleLine()
	})
}

// indexGroup is the heading of one workspace's rows: its color, its name and
// where it works.
func (t *Plugin) indexGroup(c *ui.Context, ch chrome, w *workspace) {
	pal := ch.pal
	ui.Row(c).FillWidth().Height(28).AlignItems(ui.Center).Gap(7).Padding(0, 10).Children(func() {
		ui.Box(c).Width(6).Height(6).Shrink(0).Radius(3).Background(projectColor(ch, w.color))
		ui.Text(c, strings.ToUpper(w.name)).FontSize(9.5).FontWeight(600).TextColor(pal.Muted).
			SingleLine().Ellipsis("…")
		ui.Text(c, shortenHome(w.dir)).Font("monospace").FontSize(9).TextColor(pal.Faint).
			SingleLine().Ellipsis("…")
	})
}

// indexRow is one session of the index: its icon, name, folder and state.
func (t *Plugin) indexRow(c *ui.Context, ch chrome, e entry, i int) ui.Element {
	pal := ch.pal
	s := e.s
	row := ui.Box(c).Height(44).FillWidth().Shrink(0).Radius(7).Padding(0, 10)
	switch {
	case i == t.indexSel:
		row.Background(pal.Selected)
	case row.Hovered():
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	row.OnClick(func() { t.openFromIndex(s) })
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(10).Children(func() {
			t.sessionIcon(c, ch, s, 18)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Text(c, s.title()).FontSize(12.5).FontWeight(600).TextColor(pal.Text).
						SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					if sub := s.subtitle(); sub != "" && sub != s.title() {
						ui.Text(c, "·").FontSize(9.5).TextColor(pal.Faint).Shrink(0)
						ui.Text(c, sub).FontSize(9.5).TextColor(pal.Faint).SingleLine().
							Ellipsis("…").Shrink(1).MinWidth(0)
					}
					if len(e.w.tabs) > 1 {
						ui.Text(c, "·").FontSize(9).TextColor(pal.Faint).Shrink(0)
						ui.Text(c, "tab "+strconv.Itoa(e.tab+1)).FontSize(9).TextColor(pal.Faint).Shrink(0)
					}
				})
				ui.Text(c, shortenPath(s.cwd(), 52)).Font("monospace").FontSize(9.5).
					TextColor(pal.Faint).SingleLine().Ellipsis("…")
			})
			t.stateChip(c, ch, s)
			if i == t.indexSel {
				ui.Text(c, "↵").FontSize(10.5).TextColor(pal.Faint).Shrink(0)
			}
		})
	})
	return row
}

// stateChip is a session's state as a colored dot and a word.
func (t *Plugin) stateChip(c *ui.Context, ch chrome, s *session) {
	pal := ch.pal
	color := sessionStateColor(ch, s)
	ui.Row(c).Shrink(0).Height(20).AlignItems(ui.Center).Gap(6).Padding(0, 8).Radius(10).
		Background(pal.Raised).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Shrink(0).Background(color)
		ui.Text(c, s.stateLabel()).FontSize(9.5).TextColor(color).SingleLine()
	})
}

// attentionPill is how many agents need the user, beside the index's title.
func (t *Plugin) attentionPill(c *ui.Context, ch chrome) {
	n := t.attentionCount()
	if n == 0 {
		return
	}
	color := ch.pal.Waiting
	text := plural(n, "agent") + " need attention"
	if n == 1 {
		text = "1 agent needs attention"
	}
	ui.Row(c).Shrink(0).Height(22).AlignItems(ui.Center).Gap(6).Padding(0, 9).Radius(11).
		Background(ch.pal.Raised).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(color)
		ui.Text(c, text).FontSize(10).TextColor(color)
	})
}

// keyHint is a key and what it does.
func keyHint(c *ui.Context, ch chrome, key, label string) {
	pal := ch.pal
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Box(c).Height(18).Padding(0, 6).Radius(4).Background(pal.Raised).Center().Children(func() {
			ui.Text(c, key).FontSize(9.5).TextColor(pal.Muted)
		})
		ui.Text(c, label).FontSize(9.5).TextColor(pal.Faint).SingleLine()
	})
}

// plural writes a count and its noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// indexSummary counts what the index holds.
func indexSummary(entries []entry) string {
	agents := 0
	for _, e := range entries {
		if e.s.isAgent() {
			agents++
		}
	}
	parts := []string{plural(len(entries), "session")}
	if agents != len(entries) {
		parts = append(parts, plural(agents, "agent"))
	}
	return strings.Join(parts, " · ")
}

// indexEntries lists every session open, workspace after workspace and in the
// order each tab draws its panes.
func (t *Plugin) indexEntries() []entry {
	var out []entry
	for _, w := range t.workspaces {
		for i, tb := range w.tabs {
			for _, s := range tb.root.sessions(nil) {
				out = append(out, entry{w: w, tab: i, s: s})
			}
		}
	}
	return out
}

// openIndex shows the index, with the pane that most needs the user chosen:
// one blocked on an approval, then one that finished, then the pane with the
// keyboard. It closes again when it is open.
func (t *Plugin) openIndex() {
	if t.indexOpen {
		t.indexOpen = false
		if t.ctx != nil {
			t.ctx.Invalidate()
		}
		return
	}
	entries := t.indexEntries()
	t.indexOpen = true
	t.indexReveal = true
	t.indexSel = t.firstAttention(entries)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// firstAttention is the row the index opens on, 0 when no pane stands out.
func (t *Plugin) firstAttention(entries []entry) int {
	for _, want := range []agentState{agentBlocked, agentDone} {
		for i, e := range entries {
			if e.s.isAgent() && e.s.agentState() == want {
				return i
			}
		}
	}
	for i, e := range entries {
		if t.focused(e.s) {
			return i
		}
	}
	return 0
}

// openFromIndex closes the index and shows the session it chose.
func (t *Plugin) openFromIndex(s *session) {
	t.indexOpen = false
	t.focusOn(s)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// indexKey moves through the index and opens what it points at.
func (t *Plugin) indexKey(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown || !t.indexOpen {
		return false
	}
	entries := t.indexEntries()
	if len(entries) == 0 {
		return false
	}
	if t.indexSel >= len(entries) {
		t.indexSel = len(entries) - 1
	}
	if t.indexSel < 0 {
		t.indexSel = 0
	}
	switch ev.Key {
	case ui.KeyEscape:
		t.indexOpen = false
	case ui.KeyDown:
		t.indexSel = (t.indexSel + 1) % len(entries)
		t.indexReveal = true
	case ui.KeyUp:
		t.indexSel = (t.indexSel - 1 + len(entries)) % len(entries)
		t.indexReveal = true
	case ui.KeyHome:
		t.indexSel = 0
		t.indexReveal = true
	case ui.KeyEnd:
		t.indexSel = len(entries) - 1
		t.indexReveal = true
	case ui.KeyEnter:
		t.openFromIndex(entries[t.indexSel].s)
	default:
		return false
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
	return true
}
