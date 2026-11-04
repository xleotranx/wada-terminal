package terminal

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// defaultSidebarWidth is how wide the column beside the panes is until a
// layout says otherwise.
const defaultSidebarWidth = 236

// sidebarPathWidth is how many characters of a workspace's folder the column
// shows before it keeps the last folders and drops the start.
const sidebarPathWidth = 26

// page draws the window's page as the layout's tree of workspaces, panes and
// agents. It is the only place the window's chrome is arranged.
func (t *Plugin) page(c *ui.Context) {
	t.services = c.Services()
	ch := chromeFor(c)
	t.modAnimatedFrame = false
	t.noteOverlay()
	page := ui.Box(c).Fill().MinWidth(0).MinHeight(0).Background(ch.pal.PanelBg)
	page.Children(func() {
		if t.taskInboxOpen {
			t.taskInbox(c, ch)
			return
		}
		root := t.layoutTree()
		t.blockElement(c, ch, root, axisNone, posOnly, "root")
		for _, block := range t.floatingBlocks(root) {
			t.floatElement(c, ch, block)
		}
	})
	if bounds := page.Bounds(); bounds.W > 0 && bounds.H > 0 {
		t.pageW, t.pageH = bounds.W, bounds.H
	}
	if t.modAnimated.Swap(t.modAnimatedFrame) != t.modAnimatedFrame {
		select {
		case t.modAnimatedWake <- struct{}{}:
		default:
		}
	}
}

// workspaceView is the column beside the panes: the workspace on show and the
// menu that switches to another, its folder, its tabs, and the actions at its
// foot. One workspace shows at a time, so the tabs are one level, not the
// second level of a tree.
func (t *Plugin) workspaceView(c *ui.Context, ch chrome, position blockPosition) ui.Element {
	pal := ch.pal
	side := ui.Column(c).MinWidth(0).MinHeight(0).Background(pal.DrawerBg)
	switch position {
	case posLast:
		side = side.BorderWidth(0, 0, 0, 1)
	case posOnly:
		// The only block needs no divider.
	default:
		side = side.BorderWidth(0, 1, 0, 0)
	}
	side.BorderColor(pal.Hairline).Padding(10, 8, 9, 8).Gap(1).
		Children(func() {
			t.workspaceSwitcher(c, ch)
			t.workspacePathLine(c, ch)
			t.tabSection(c, ch)
			t.sidebarFooter(c, ch)
		})
	return side
}

// workspaceSwitcher is the workspace on show as a control: its color, its
// name, and the menu of the others.
func (t *Plugin) workspaceSwitcher(c *ui.Context, ch chrome) {
	pal := ch.pal
	w := t.current
	row := ui.Box(c).Height(32).FillWidth().Shrink(0).Radius(7).Padding(0, 7)
	switch {
	case t.workspaceMenu:
		row = row.Background(pal.Selected)
	case row.Hovered():
		row = row.Background(pal.Selected)
	default:
		row = row.Background(pal.Hover)
	}
	row = row.Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			if w == nil {
				ui.Text(c, "No workspace").FontSize(12.5).TextColor(pal.Muted).Grow(1)
				return
			}
			ui.Box(c).Width(8).Height(8).Radius(4).Shrink(0).Background(projectColor(ch, w.color))
			ui.Text(c, w.name).FontSize(12.5).FontWeight(600).TextColor(pal.Text).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
			chevronDown(c, pal.Muted)
		})
	})
	row.OnClick(func() {
		t.workspaceMenu = !t.workspaceMenu
		t.repaint()
	})
	ui.PopoverBase(c, row, &t.workspaceMenu, func(panel ui.Element) {
		t.workspaceMenuPanel(c, ch, row, panel)
	})
}

// chevronDown draws the mark of a row that opens a menu below it.
func chevronDown(c *ui.Context, color ui.Color) {
	ui.Box(c).Width(9).Height(9).Shrink(0).Center().Draw(func(p *ui.Painter, r ui.Rect) {
		var path ui.Path
		path.MoveTo(r.X, r.Y+r.H*0.32).
			LineTo(r.X+r.W/2, r.Y+r.H*0.72).
			LineTo(r.X+r.W, r.Y+r.H*0.32)
		p.StrokePath(&path, 1.3, color)
	})
}

// workspaceMenuPanel lists every workspace with what it holds, and the way to
// open another one. Empty workspaces say so instead of showing a count that
// could mean several things.
func (t *Plugin) workspaceMenuPanel(c *ui.Context, ch chrome, anchor, panel ui.Element) {
	pal := ch.pal
	width := anchor.Bounds().W
	if width < 200 {
		width = 200
	}
	panel.Width(width).Padding(5).Radius(10).Background(pal.PanelBg).
		Border(1, pal.Hairline).Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.22))
	panel.Children(func() {
		ui.Scroll(c).FillWidth().MaxHeight(320).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				for _, w := range t.workspaces {
					t.workspaceMenuRow(c, ch, w)
				}
			})
		})
		ui.Box(c).Height(1).FillWidth().Background(pal.Hairline).Margin(4, 0, 4, 0)
		t.menuActionRow(c, ch, "New task…", "⌘⇧T", func() { t.workspaceMenu = false; t.openTaskDialog() })
		t.menuActionRow(c, ch, "New workspace…", "⌘⇧N", func() {
			t.workspaceMenu = false
			t.pickWorkspace()
		})
	})
}

// workspaceMenuRow is one workspace in the menu: its color, its name, and what
// it asks of the user. The close button takes the row's place while the
// pointer is on it.
func (t *Plugin) workspaceMenuRow(c *ui.Context, ch chrome, w *workspace) {
	pal := ch.pal
	current := w == t.current
	row := ui.Box(c).Height(30).FillWidth().Shrink(0).Radius(6).Padding(0, 7)
	switch {
	case current:
		row.Background(pal.Selected)
	case row.Hovered():
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	hovered := row.Hovered()
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(8).Height(8).Radius(4).Shrink(0).Background(projectColor(ch, w.color))
			ui.Text(c, w.name).FontSize(12).FontWeight(600).TextColor(pal.Text).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
			if w.machine != "" {
				ui.Text(c, w.machine).FontSize(8.5).TextColor(pal.Link).Shrink(0).
					MaxWidth(58).SingleLine().Ellipsis("…")
			}
			if hovered && len(t.workspaces) > 1 {
				t.closeWorkspaceButton(c, ch, w)
				return
			}
			if w.task != nil {
				ui.Text(c, taskStatus(w)).FontSize(10).TextColor(pal.Muted).Shrink(0)
			} else {
				t.workspaceMenuMeta(c, ch, w)
			}
		})
	})
	row.OnClick(func() {
		t.workspaceMenu = false
		t.selectWorkspace(w)
	})
}

// workspaceMenuMeta is what a workspace holds, said in words: the agents that
// need the user first, else how many tabs it has.
func (t *Plugin) workspaceMenuMeta(c *ui.Context, ch chrome, w *workspace) {
	pal := ch.pal
	if st, n := w.rollup(); n > 0 {
		ui.Text(c, rollupLabel(st, n)).FontSize(9.5).TextColor(stateColor(ch, st)).Shrink(0)
		return
	}
	switch len(w.tabs) {
	case 0:
		ui.Text(c, "empty").FontSize(9.5).TextColor(pal.Faint).Shrink(0)
	case 1:
		ui.Text(c, "1 tab").FontSize(9.5).TextColor(pal.Faint).Shrink(0)
	default:
		ui.Text(c, strconv.Itoa(len(w.tabs))+" tabs").FontSize(9.5).TextColor(pal.Faint).Shrink(0)
	}
}

// rollupLabel says how many agents of a workspace are in a state.
func rollupLabel(st agentState, n int) string {
	switch st {
	case agentBlocked:
		if n == 1 {
			return "needs you"
		}
		return strconv.Itoa(n) + " waiting"
	case agentDone:
		if n == 1 {
			return "done"
		}
		return strconv.Itoa(n) + " done"
	case agentWorking:
		if n == 1 {
			return "working"
		}
		return strconv.Itoa(n) + " working"
	default:
		return ""
	}
}

// menuActionRow is a verb at the foot of the workspace menu.
func (t *Plugin) menuActionRow(c *ui.Context, ch chrome, label, key string, fn func()) {
	pal := ch.pal
	row := ui.Box(c).Height(28).FillWidth().Shrink(0).Radius(6).Padding(0, 7)
	if row.Hovered() {
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(8).Shrink(0).Center().Children(func() {
				ui.Text(c, "+").FontSize(12).TextColor(pal.Faint)
			})
			ui.Text(c, label).FontSize(11.5).TextColor(pal.Muted).Grow(1).SingleLine()
			ui.Text(c, key).FontSize(10).TextColor(pal.Faint).Shrink(0)
		})
	})
	row.OnClick(fn)
}

// workspacePathLine is the folder of the workspace on show, under its name,
// with the machine it runs on when it is not this computer.
func (t *Plugin) workspacePathLine(c *ui.Context, ch chrome) {
	w := t.current
	if w == nil {
		return
	}
	line := workspaceDirLine(w.dir, w.machine)
	if line == "" {
		return
	}
	ui.Text(c, line).Font("monospace").FontSize(9.5).
		TextColor(ch.pal.Faint).SingleLine().Ellipsis("…").Shrink(0).
		Padding(1, 7, 3, 22)
}

// tabSection is the tabs of the workspace on show, under a label that names
// them, so they are not mistaken for workspaces.
func (t *Plugin) tabSection(c *ui.Context, ch chrome) {
	pal := ch.pal
	w := t.current
	ui.Row(c).Height(18).FillWidth().Shrink(0).Margin(9, 0, 0, 0).Padding(0, 7).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, "TERMINALS").FontSize(9.5).FontWeight(600).LetterSpacing(0.6).
			TextColor(pal.Faint).Grow(1).SingleLine()
		if w != nil && len(w.tabs) > 0 {
			ui.Text(c, strconv.Itoa(len(w.sessions(nil)))).FontSize(9.5).TextColor(pal.Faint).
				Shrink(0)
		}
	})
	ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			if w == nil || len(w.tabs) == 0 {
				ui.Text(c, "No terminals open").FontSize(11).TextColor(pal.Faint).
					Padding(4, 7)
				return
			}
			for i, tb := range w.tabs {
				t.tabTree(c, ch, w, i, tb)
			}
		})
	})
}

// sidebarFooter is the foot of the column: what opens a tab, and the index of
// every session. Creation comes first and navigation last.
func (t *Plugin) sidebarFooter(c *ui.Context, ch chrome) {
	pal := ch.pal
	ui.Column(c).FillWidth().Shrink(0).Gap(1).Children(func() {
		ui.Box(c).Height(1).FillWidth().Background(pal.Hairline).Margin(7, 2, 7, 2)
		t.footerAction(c, ch, "☷", "Tasks", "⌘⇧J", t.openTaskInbox)
		t.footerAction(c, ch, "+", "New task…", "⌘⇧T", t.openTaskDialog)
		t.footerAction(c, ch, "+", "New terminal", "⌘T", func() { t.newTab(kindShell) })
		t.footerAction(c, ch, "⌗", "All terminals", "⌘⇧I", t.openIndex)
	})
}

// footerAction is one row at the foot of the column, with the key that does the
// same from the keyboard.
func (t *Plugin) footerAction(c *ui.Context, ch chrome, glyph, label, key string, fn func()) {
	pal := ch.pal
	row := ui.Box(c).Height(28).FillWidth().Shrink(0).Radius(6).Padding(0, 7)
	if row.Hovered() {
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(14).Shrink(0).Center().Children(func() {
				ui.Text(c, glyph).FontSize(11.5).TextColor(pal.Muted)
			})
			ui.Text(c, label).FontSize(11.5).TextColor(pal.Muted).Grow(1).
				SingleLine().Ellipsis("…")
			ui.Text(c, key).FontSize(10).TextColor(pal.Faint).Shrink(0)
		})
	})
	row.OnClick(fn)
}

// repaint asks for a frame after a change the frame being built did not see.
func (t *Plugin) repaint() {
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// projectColor is the color a workspace takes from the palette, by its index.
func projectColor(ch chrome, i int) ui.Color {
	return ch.pal.Projects[i%len(ch.pal.Projects)].Mark
}

// shortenHome writes a path under the home folder with a leading ~.
func shortenHome(dir string) string {
	home := homeDir()
	switch {
	case home == "":
		return dir
	case dir == home:
		return "~"
	case strings.HasPrefix(dir, home+string(filepath.Separator)):
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// workspaceDirLine is the folder a workspace shows under its name, with the
// machine it runs on and its tail kept when it is too long for the column.
func workspaceDirLine(dir, machine string) string {
	line := shortenHome(dir)
	if machine != "" {
		if line == "" {
			line = "remote"
		}
		return machine + " · " + fitPath(line, sidebarPathWidth-len([]rune(machine))-3)
	}
	return fitPath(line, sidebarPathWidth)
}

// fitPath shortens a path to width characters, keeping whole folders from the
// end: the project's name is what tells two workspaces apart, so it is the
// last thing dropped.
func fitPath(path string, width int) string {
	runes := []rune(path)
	if len(runes) <= width || width < 8 {
		return path
	}
	start := len(runes) - (width - 2)
	// A suffix that already starts a folder needs no cut; one that starts
	// inside a name moves to the next separator.
	if start > 0 && runes[start-1] != '/' {
		if i := strings.IndexRune(string(runes[start:]), '/'); i >= 0 {
			start += i + 1
		}
	}
	if start >= len(runes) {
		return "…/" + string(runes[len(runes)-width+2:])
	}
	return "…/" + string(runes[start:])
}

// selectWorkspace shows a workspace and its focused pane.
func (t *Plugin) selectWorkspace(w *workspace) {
	if w == nil {
		return
	}
	t.taskInboxOpen = false
	if w == t.current {
		return
	}
	t.current = w
	t.workspaceMenu = false
	if w.focus == nil {
		if tb := w.currentTab(); tb != nil {
			w.focus = tb.focus
		}
	}
	t.seen(w.focus)
}

// tabTree lists terminals in screen order. A neutral header and shared rail
// identify a split arrangement; neither terminal is promoted above its peers.
func (t *Plugin) tabTree(c *ui.Context, ch chrome, w *workspace, i int, tb *tab) {
	panes := tb.root.sessions(nil)
	if len(panes) == 0 {
		return
	}
	active := i == w.active
	if len(panes) == 1 {
		t.treeRow(c, ch, panes[0], active && w.focus == panes[0])
		return
	}
	t.groupRow(c, ch, tb)
	ui.Box(c).FillWidth().Shrink(0).BorderWidth(0, 0, 0, 1).
		BorderColor(ch.pal.Hairline).Margin(0, 0, 5, 7).Padding(0, 0, 0, 5).Children(func() {
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			for _, s := range panes {
				t.treeRow(c, ch, s, active && w.focus == s)
			}
		})
	})
}

// treeRow gives every terminal the same visual weight, including split peers.
func (t *Plugin) treeRow(c *ui.Context, ch chrome, s *session, selected bool) {
	pal := ch.pal
	row := ui.Box(c).Height(32).FillWidth().Shrink(0).Radius(6).Padding(0, 8)
	switch {
	case selected:
		row.Background(pal.Selected)
	case row.Hovered():
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer).Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
			t.sessionIcon(c, ch, s, 17)
			ui.Text(c, s.title()).FontSize(12.5).TextColor(pal.Text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
			t.sessionBadge(c, ch, s, selected)
		})
	})
	row.OnClick(func() { t.focusOn(s) })
}

// groupRow names the arrangement, independently of the terminal with focus.
func (t *Plugin) groupRow(c *ui.Context, ch chrome, tb *tab) {
	pal := ch.pal
	row := ui.Box(c).Height(27).FillWidth().Shrink(0).Padding(0, 8).Margin(5, 0, 0, 0)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			t.layoutMark(c, ch, tb)
			ui.Text(c, tabTitle(tb)).FontSize(11).FontWeight(600).TextColor(pal.Muted).
				SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			ui.Text(c, strconv.Itoa(len(tb.root.sessions(nil)))+" panes").FontSize(10).TextColor(pal.Muted).Shrink(0)
		})
	})
	row.Cursor(ui.CursorPointer).OnClick(func() { t.focusOn(tb.focus) })
}

// layoutMark draws how a split tab puts its panes: two columns side by side,
// or two rows one above the other.
func (t *Plugin) layoutMark(c *ui.Context, ch chrome, tb *tab) {
	pal := ch.pal
	vertical := tb.root != nil && tb.root.split != nil && tb.root.split.vertical
	ui.Box(c).Width(14).Height(14).Shrink(0).Center().Children(func() {
		if vertical {
			ui.Row(c).Gap(1).Children(func() {
				ui.Box(c).Width(4).Height(9).Radius(1).Background(pal.Faint)
				ui.Box(c).Width(4).Height(9).Radius(1).Background(pal.Faint)
			})
			return
		}
		ui.Column(c).Gap(1).Children(func() {
			ui.Box(c).Width(9).Height(4).Radius(1).Background(pal.Faint)
			ui.Box(c).Width(9).Height(4).Radius(1).Background(pal.Faint)
		})
	})
}

// sessionBadge says what a pane holds: working, waiting on the user, done,
// idle or stopped. A pane that needs the user says so in words, so the colors
// alone do not have to carry it.
func (t *Plugin) sessionBadge(c *ui.Context, ch chrome, s *session, strong bool) {
	pal := ch.pal
	if s.isAgent() {
		switch s.agentState() {
		case agentBlocked:
			ui.Box(c).Height(17).Shrink(0).Padding(0, 5).Radius(4).
				Background(pal.Raised).Center().Children(func() {
				ui.Text(c, "needs you").FontSize(9.5).FontWeight(600).TextColor(pal.Waiting)
			})
			return
		case agentDone:
			ui.Box(c).Height(17).Shrink(0).Padding(0, 5).Radius(4).
				Background(pal.Raised).Center().Children(func() {
				ui.Text(c, "done").FontSize(9.5).FontWeight(600).TextColor(pal.Link)
			})
			return
		}
	}
	size := float32(6)
	if !strong {
		size = 5
	}
	ui.Box(c).Width(size).Height(size).Shrink(0).Radius(size / 2).
		Background(sessionStateColor(ch, s))
}

// closeWorkspaceButton closes a workspace from its row in the menu, which only
// shows while the pointer is on it.
func (t *Plugin) closeWorkspaceButton(c *ui.Context, ch chrome, w *workspace) {
	pal := ch.pal
	btn := ui.Box(c).Width(16).Height(16).Shrink(0).Radius(4).Center()
	if btn.Hovered() {
		btn.Background(pal.Raised)
	}
	btn.Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, "✕").FontSize(9).TextColor(pal.Faint)
	})
	btn.OnClick(func() {
		t.workspaceMenu = false
		t.closeWorkspace(w)
	})
}

// sessionIcon draws the chip that stands for a session: its kind's mark on its
// accent.
func (t *Plugin) sessionIcon(c *ui.Context, ch chrome, s *session, size float32) {
	glyph := s.kind.spec().glyph
	if s.agentGlyph != "" {
		glyph = s.agentGlyph
	}
	color := accentFor(s.kind, ch.dark)
	if s.agentColor != "" {
		color = ui.Hex(s.agentColor)
	}
	ui.Box(c).Width(size).Height(size).Shrink(0).Radius(size * 0.3).
		Background(color).Center().Children(func() {
		if icon := t.agentIcons[s.agentID]; icon != nil {
			if s.agentTint() {
				ui.Icon(c, icon).Width(size * 0.7).Height(size * 0.7)
			} else {
				ui.Image(c, icon).Width(size * 0.7).Height(size * 0.7)
			}
			return
		}
		ui.Text(c, glyph).FontSize(size * 0.58).FontWeight(700).TextColor(onAccent(ch.dark))
	})
}

// content draws the panes of the tab on show.
func (t *Plugin) content(c *ui.Context, ch chrome) ui.Element {
	tb := t.currentTab()
	if tb == nil {
		return t.nothingOpen(c, ch)
	}
	body := ui.Box(c).MinWidth(0).MinHeight(0).Padding(8)
	body.Children(func() {
		t.layout(c, ch, tb.root)
	})
	if !t.search.open {
		return body
	}
	column := ui.Column(c).MinWidth(0).MinHeight(0)
	column.Children(func() {
		t.searchBar(c, ch)
		body.Grow(1).FillWidth()
	})
	return column
}

// nothingOpen is the page with no session at all.
func (t *Plugin) nothingOpen(c *ui.Context, ch chrome) ui.Element {
	pal := ch.pal
	el := ui.Column(c).Center().Gap(12)
	el.Children(func() {
		ui.Text(c, "No sessions open").FontSize(13).TextColor(pal.Muted)
		ui.Box(c).Height(28).Padding(0, 14).Radius(7).Background(pal.Raised).Center().
			Cursor(ui.CursorPointer).OnClick(func() { t.newTab(kindShell) }).Children(func() {
			ui.Text(c, "New Session").FontSize(12.5).TextColor(pal.Text)
		})
	})
	return el
}

// layout draws one node of a tab: a pane, or the two sides of a split.
func (t *Plugin) layout(c *ui.Context, ch chrome, n *node) {
	if n == nil {
		return
	}
	if n.leaf != nil {
		t.pane(c, ch, n.leaf)
		return
	}
	b := n.split
	row := ui.Row(c)
	if !b.vertical {
		row = ui.Column(c)
	}
	row.Fill().MinWidth(0).MinHeight(0).Children(func() {
		// The room the panes share is what the frame before laid out,
		// without the divider between them.
		if r := row.Bounds(); r.W > 0 && r.H > 0 {
			b.usable = max(r.W, r.H) - dividerSize
		}
		// A child joins the layout as it is built, so the divider is made
		// between the two panes rather than before them.
		a := ui.Box(c).Grow(b.ratio).Basis(0).MinWidth(0).MinHeight(0)
		if b.vertical { // side by side: both take the split's whole height
			a = a.FillHeight()
		} else { // one above the other: both take its whole width
			a = a.FillWidth()
		}
		a.Children(func() { t.layout(c, ch, b.a) })
		t.divider(c, ch, b)
		z := ui.Box(c).Grow(1 - b.ratio).Basis(0).MinWidth(0).MinHeight(0)
		if b.vertical {
			z = z.FillHeight()
		} else {
			z = z.FillWidth()
		}
		z.Children(func() { t.layout(c, ch, b.b) })
	})
}

// strip draws the window's title strip: one tab per layout, and a button that
// opens another.
func (t *Plugin) strip(c *ui.Context) ui.Element {
	ch := chromeFor(c)
	pal := ch.pal
	w := t.current
	var tabs []*tab
	if w != nil {
		tabs = w.tabs
	}
	return ui.Row(c).FillHeight().MinWidth(0).AlignItems(ui.Center).Gap(3).Clip().Children(func() {
		for i, tb := range tabs {
			t.tabButton(c, ch, w, i, tb)
		}
		plus := ui.Box(c).Width(26).Height(26).Shrink(0).Radius(7).Center()
		if plus.Hovered() {
			plus.Background(pal.Hover)
		}
		plus.Cursor(ui.CursorPointer).Children(func() {
			ui.Text(c, "+").FontSize(15).TextColor(pal.Faint)
		})
		plus.OnClick(func() { t.newTab(kindShell) })
	})
}

// tabButton is one tab of the strip.
func (t *Plugin) tabButton(c *ui.Context, ch chrome, w *workspace, i int, tb *tab) {
	pal := ch.pal
	active := w != nil && i == w.active
	btn := ui.Box(c).Height(27).Shrink(0).Padding(0, 10).Radius(8)
	switch {
	case active:
		btn.Background(pal.ActiveTab)
	case btn.Hovered():
		btn.Background(pal.Hover)
	}
	btn.Cursor(ui.CursorPointer)
	btn.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown && ev.Button == 1 { // middle click
			t.closeTab(i)
			return true
		}
		return false
	})
	btn.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			s := tb.focus
			if s == nil {
				return
			}
			t.sessionIcon(c, ch, s, 15)
			color := pal.Muted
			if active {
				color = pal.Text
			}
			ui.Text(c, tabTitle(tb)).FontSize(12).FontWeight(500).TextColor(color).
				SingleLine().Ellipsis("…").MaxWidth(190)
			if n := len(tb.root.sessions(nil)); n > 1 {
				ui.Box(c).Height(15).Padding(0, 5).Radius(4).Shrink(0).
					Background(pal.Raised).Center().Children(func() {
					ui.Text(c, strconv.Itoa(n)).FontSize(9.5).TextColor(pal.Muted)
				})
			}
			closeColor := ui.Transparent
			if btn.Hovered() || active {
				closeColor = pal.Faint
			}
			close := ui.Box(c).Width(14).Height(14).Shrink(0).Radius(4).Center()
			if close.Hovered() {
				close.Background(pal.Hover)
				closeColor = pal.Text
			}
			close.Cursor(ui.CursorPointer)
			close.Children(func() {
				ui.Text(c, "✕").FontSize(9).TextColor(closeColor)
			})
			close.OnClick(func() { t.closeTab(i) })
		})
	})
	btn.OnClick(func() { t.selectTab(i) })
}

// tabTitle names split arrangements independently of focus. A single terminal
// continues to follow its program title unless the arrangement was named.
func tabTitle(tb *tab) string {
	if tb.name != "" {
		return tb.name
	}
	if len(tb.root.sessions(nil)) > 1 {
		return "Split view"
	}
	if tb.focus != nil {
		return tb.focus.title()
	}
	if names := tb.root.sessions(nil); len(names) > 0 {
		return names[0].title()
	}
	return "Terminal"
}

// actions draws the window's TitleBar items, at the end of the title strip.
func (t *Plugin) actions(c *ui.Context) {
	ch := chromeFor(c)
	ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(1).Children(func() {
		if st, n := t.attention(); n > 0 {
			t.attentionButton(c, ch, st, n)
		}
		t.actionButton(c, ch, "▤", t.toggleSidebar)
		t.actionButton(c, ch, "◫", func() { t.splitFocused(true) })
		t.actionButton(c, ch, "+", func() { t.newTab(kindShell) })
	})
}

// attentionButton says how many agents need the user, and opens the index
// that leads to them.
func (t *Plugin) attentionButton(c *ui.Context, ch chrome, st agentState, n int) {
	pal := ch.pal
	color := stateColor(ch, st)
	btn := ui.Box(c).Height(24).Shrink(0).Radius(6).Padding(0, 8).Center()
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	btn.Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(5).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Shrink(0).Background(color)
			ui.Text(c, rollupLabel(st, n)).FontSize(10.5).FontWeight(600).TextColor(color)
		})
	})
	btn.OnClick(t.openIndex)
}

// actionButton is one of the strip's buttons.
func (t *Plugin) actionButton(c *ui.Context, ch chrome, glyph string, fn func()) {
	pal := ch.pal
	btn := ui.Box(c).Width(24).Height(24).Shrink(0).Radius(6).Center()
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	btn.Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, glyph).FontSize(12.5).TextColor(pal.Muted)
	})
	btn.OnClick(fn)
}

// dividerSize is how wide the bar between two panes is, along their split.
const dividerSize = 8

// divider is the bar between two panes: a hairline until the pointer is on
// it, which drags it and changes how much room each side has.
func (t *Plugin) divider(c *ui.Context, ch chrome, b *branch) {
	pal := ch.pal
	box := ui.Box(c)
	if b.vertical {
		box = box.Width(dividerSize).FillHeight()
	} else {
		box = box.FillWidth().Height(dividerSize)
	}
	box = box.Shrink(0).Center()
	color := pal.Hairline
	if box.Hovered() || t.drag == b {
		color = pal.Muted
	}
	if b.vertical {
		box = box.Cursor(ui.CursorResizeColumn)
	} else {
		box = box.Cursor(ui.CursorResizeRow)
	}
	box = box.HandleInput(func(ev ui.InputEvent) bool { return t.dragDivider(b, ev, b.vertical) })
	box.Children(func() {
		line := ui.Box(c)
		if b.vertical {
			line = line.Width(1).FillHeight()
		} else {
			line = line.FillWidth().Height(1)
		}
		line.Background(color)
	})
}

// agentTint reports whether a custom agent's SVG should take the text color.
func (s *session) agentTint() bool {
	def, ok := s.t.agentDefs[s.agentID]
	return ok && def.Tint
}

// titleActions is the title-bar strip as a layout block.
func (t *Plugin) titleActions(c *ui.Context) ui.Element {
	ch := chromeFor(c)
	return ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(1).Children(func() {
		if st, n := t.attention(); n > 0 {
			t.attentionButton(c, ch, st, n)
		}
		t.actionButton(c, ch, "▤", t.toggleSidebar)
		t.actionButton(c, ch, "◫", func() { t.splitFocused(true) })
		t.actionButton(c, ch, "+", func() { t.newTab(kindShell) })
	})
}
