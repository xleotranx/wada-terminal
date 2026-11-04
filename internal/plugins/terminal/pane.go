package terminal

import (
	mterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// bodyPad is the room a pane's cells keep from its border, in DIPs.
const (
	bodyPadX = 8
	bodyPadY = 6
)

// pane draws one session: its header, and the cells of its screen.
func (t *Plugin) pane(c *ui.Context, ch chrome, s *session) {
	sf := t.terminalSurface(c)
	focused := t.focused(s)
	line := sf.border
	if focused {
		line = accentFor(s.kind, ch.dark)
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Radius(10).Clip().Background(t.terminalSurface(c).bg).
		Border(1, line).Children(func() {
		t.header(c, ch, s)
		t.body(c, ch, s, focused)
	})
}

// header is the pane's line of identity: the program's icon and title, the
// mode it reports, where it runs, and what the pane can do.
func (t *Plugin) header(c *ui.Context, ch chrome, s *session) {
	pal := ch.pal
	sf := t.terminalSurface(c)
	ui.Column(c).FillWidth().Shrink(0).Padding(7, 10, 7, 10).Gap(3).Children(func() {
		ui.Row(c).Height(16).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
			t.sessionIcon(c, ch, s, 15)
			ui.Text(c, s.title()).FontSize(12).FontWeight(600).TextColor(sf.fg).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			if badge := s.statusText(); badge != "" {
				ui.Text(c, badge).FontSize(10).TextColor(pal.Faint).SingleLine()
			}
			if s.isAgent() {
				switch st := s.agentState(); st {
				case agentWorking, agentBlocked, agentDone:
					t.stateBadge(c, ch, st, s.stateLabel())
				}
			}
			ui.Box(c).Grow(1).MinWidth(0)
			t.paneButton(c, ch, "◫", func() { t.splitAt(s, true) })
			t.paneButton(c, ch, "⬓", func() { t.splitAt(s, false) })
			t.paneButton(c, ch, "✕", func() { t.close(s) })
		})
		ui.Row(c).Height(13).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			text, color := s.subtitle(), pal.Muted
			if text == s.title() {
				text = ""
			}
			if !s.alive {
				text, color = "stopped", pal.Warning
			}
			ui.Text(c, text).FontSize(10.5).TextColor(color).SingleLine().Ellipsis("…").
				Shrink(1).MinWidth(0).Grow(1)
			ui.Text(c, shortenPath(s.cwd(), 34)).Font("monospace").FontSize(9.5).
				TextColor(pal.Faint).SingleLine().Ellipsis("…")
		})
	})
}

// stateBadge is what an agent in the pane is doing, beside its title. The
// label comes from the session, so a protocol's own kind says what it waits for.
func (t *Plugin) stateBadge(c *ui.Context, ch chrome, st agentState, label string) {
	color := stateColor(ch, st)
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Box(c).Width(5).Height(5).Radius(2.5).Shrink(0).Background(color)
		ui.Text(c, label).FontSize(9.5).TextColor(color).SingleLine()
	})
}

// paneButton is one of a pane header's small buttons.
func (t *Plugin) paneButton(c *ui.Context, ch chrome, glyph string, fn func()) {
	pal := ch.pal
	btn := ui.Box(c).Width(18).Height(16).Shrink(0).Radius(4).Center()
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	btn.Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, glyph).FontSize(9.5).TextColor(pal.Faint)
	})
	btn.OnClick(fn)
}

// body is the pane's terminal: MyGo's view of the session's terminal, which
// draws the screen, takes the keyboard, selects and scrolls, and sizes the
// terminal to the room it has.
func (t *Plugin) body(c *ui.Context, ch chrome, s *session, focused bool) {
	if (!s.alive && s.status != "") || s.term == nil {
		el := ui.Box(c).Grow(1).FillWidth().MinWidth(0).MinHeight(0).
			Background(t.terminalSurface(c).bg)
		t.stopped(c, ch, s, el)
		return
	}
	el := mterm.View(c, s.term).Grow(1).FillWidth().MinWidth(0).MinHeight(0)
	switch {
	case focused && t.paneFocusAsked != s && !t.overlayOpen():
		// The keyboard moves to the pane once, as it becomes the focused
		// one or an overlay closes; asked every frame, it would take the
		// keyboard back from a mod's field the user clicked into.
		el.Focus()
		t.paneFocusAsked = s
	case !focused && el.Focused():
		// The user clicked into this pane; the session follows them.
		t.ctx.Post(func() { t.focusOn(s) })
	}
}

// overlayOpen reports whether something over the panes holds the keyboard:
// the find bar, a dialog, the Sessions view or the workspace menu.
func (t *Plugin) overlayOpen() bool {
	return t.search.open || t.links.open || t.indexOpen || t.workspaceMenu || t.taskInboxOpen ||
		t.tasksUI.open || t.tasksUI.editOpen || t.finishUI.open || t.integrateUI.open
}

// noteOverlay gives the focused pane the keyboard again once the overlay
// that held it closes. It runs once a frame, before the panes draw.
func (t *Plugin) noteOverlay() {
	open := t.overlayOpen()
	if t.overlayWasOpen && !open {
		t.paneFocusAsked = nil
	}
	t.overlayWasOpen = open
}

// stopped draws a session whose program ended: what happened, and a way back.
func (t *Plugin) stopped(c *ui.Context, ch chrome, s *session, el ui.Element) {
	pal := ch.pal
	text := s.status
	if text == "" {
		text = "The terminal is not running."
	}
	el.Children(func() {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, text).FontSize(11.5).TextColor(pal.Muted).MaxWidth(420)
			btn := ui.Box(c).Height(26).Padding(0, 12).Radius(7).Background(pal.Raised).Center()
			if btn.Hovered() {
				btn.Background(pal.Hover)
			}
			btn.Cursor(ui.CursorPointer)
			btn.Children(func() { ui.Text(c, "Restart").FontSize(12).TextColor(pal.Text) })
			btn.OnClick(func() { t.restart(s) })
		})
	})
}

// onAccent is the color of an icon's mark on its accent.
func onAccent(dark bool) ui.Color {
	if dark {
		return ui.Hex("#0e0e11")
	}
	return ui.Hex("#fbfbfc")
}

// shortenPath keeps the end of a path, which names it.
func shortenPath(path string, width int) string {
	if len(path) <= width {
		return path
	}
	return "…" + path[len(path)-width+1:]
}
