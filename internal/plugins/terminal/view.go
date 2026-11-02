package terminal

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// defaultSidebarWidth is how wide the workspace tree is until a layout says
// otherwise.
const defaultSidebarWidth = 236

// page draws the window's page as the layout's tree of workspaces, panes and
// agents. It is the only place the window's chrome is arranged.
func (t *Plugin) page(c *ui.Context) {
	t.services = c.Services()
	ch := chromeFor(c)
	t.modAnimatedFrame = false
	page := ui.Box(c).Fill().MinWidth(0).MinHeight(0).Background(ch.pal.PanelBg)
	page.Children(func() {
		root := t.layoutTree()
		t.blockElement(c, ch, root, axisNone, posOnly, "root")
		for _, block := range t.floatingBlocks(root) {
			t.floatElement(c, ch, block)
		}
	})
	if bounds := page.Bounds(); bounds.W > 0 && bounds.H > 0 {
		t.pageW, t.pageH = bounds.W, bounds.H
	}
	t.modAnimated.Store(t.modAnimatedFrame)
}

// workspaceView is the tree on the left: every workspace, and the tabs and
// panes of the one on show.
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
			ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, w := range t.workspaces {
						t.workspaceSection(c, ch, w)
					}
				})
			})
			t.newTabRow(c, ch)
		})
	return side
}

// workspaceSection is one workspace: its header, and when it is the one on
// show, its folder and its tabs.
func (t *Plugin) workspaceSection(c *ui.Context, ch chrome, w *workspace) {
	pal := ch.pal
	current := w == t.current
	header := ui.Box(c).Height(30).FillWidth().Shrink(0).Radius(7).Padding(0, 6)
	switch {
	case current:
		header.Background(pal.Hover)
	case header.Hovered():
		header.Background(pal.Hover)
	}
	header.Cursor(ui.CursorPointer)
	header.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(8).Height(8).Shrink(0).Radius(4).Background(projectColor(ch, w.color))
			color := pal.Muted
			if current {
				color = pal.Text
			}
			ui.Text(c, w.name).FontSize(12.5).FontWeight(600).TextColor(color).
				SingleLine().Ellipsis("…").Grow(1)
			if w.machine != "" {
				ui.Text(c, w.machine).FontSize(8.5).TextColor(pal.Link).Shrink(0).
					MaxWidth(58).SingleLine().Ellipsis("…")
			}
			if st, n := w.rollup(); n > 0 {
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Box(c).Width(6).Height(6).Radius(3).Background(stateColor(ch, st))
					ui.Text(c, strconv.Itoa(n)).FontSize(10).TextColor(stateColor(ch, st))
				})
			} else if !current {
				ui.Text(c, strconv.Itoa(len(w.tabs))).FontSize(10).TextColor(pal.Faint)
			}
			if current && len(t.workspaces) > 1 && header.Hovered() {
				t.closeWorkspaceButton(c, ch, w)
			}
		})
	})
	header.OnClick(func() { t.selectWorkspace(w) })
	if !current {
		return
	}
	line := shortenHome(w.dir)
	if w.machine != "" {
		if line == "" {
			line = "remote"
		}
		line = w.machine + " · " + line
	}
	ui.Text(c, line).Font("monospace").FontSize(9.5).TextColor(pal.Faint).
		SingleLine().Ellipsis("…").Padding(0, 0, 0, 15).Shrink(0)
	for i, tb := range w.tabs {
		t.tabTree(c, ch, w, i, tb)
	}
}

// closeWorkspaceButton closes a workspace from its header, which only shows
// while the pointer is on it.
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
	btn.OnClick(func() { t.closeWorkspace(w) })
}

// projectColor is the color a workspace takes from the palette, by its index.
func projectColor(ch chrome, i int) ui.Color {
	return ch.pal.Projects[i%len(ch.pal.Projects)].Mark
}

// shortenHome writes a path under the home folder with a leading ~.
func shortenHome(dir string) string {
	if home := homeDir(); home != "" && dir != home && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// selectWorkspace shows a workspace and its focused pane.
func (t *Plugin) selectWorkspace(w *workspace) {
	if w == nil || w == t.current {
		return
	}
	t.current = w
	if w.focus == nil {
		if tb := w.currentTab(); tb != nil {
			w.focus = tb.focus
		}
	}
	t.seen(w.focus)
}

// tabTree draws one tab of the tree: its row when it holds one pane, or the
// focused pane and, under it, the other panes of the split.
func (t *Plugin) tabTree(c *ui.Context, ch chrome, w *workspace, i int, tb *tab) {
	panes := tb.root.sessions(nil)
	if len(panes) == 0 {
		return
	}
	active := i == w.active
	if len(panes) == 1 {
		t.treeRow(c, ch, panes[0], active && w.focus == panes[0], false)
		return
	}
	focused := tb.focus
	if focused == nil {
		focused = panes[0]
	}
	t.groupRow(c, ch, tb, focused, active)
	for _, s := range panes {
		if s != focused {
			t.treeRow(c, ch, s, active && w.focus == s, true)
		}
	}
}

// treeRow is one pane in the tree: its icon, its title, and whether its
// program runs. A row under a split tab is indented behind a rule.
func (t *Plugin) treeRow(c *ui.Context, ch chrome, s *session, selected, child bool) {
	pal := ch.pal
	height, radius := float32(30), float32(7)
	if child {
		height, radius = 26, 6
	}
	row := ui.Box(c).Height(height).FillWidth().Shrink(0).Radius(radius).Padding(0, 8)
	switch {
	case selected:
		row.Background(pal.Selected)
	case row.Hovered():
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
			if child {
				ui.Box(c).Width(1).FillHeight().Background(pal.Hairline)
			}
			size := float32(17)
			if child {
				size = 14
			}
			t.sessionIcon(c, ch, s, size)
			color := pal.Text
			if child && !selected {
				color = pal.Muted
			}
			fontSize := float32(12.5)
			if child {
				fontSize = 11.5
			}
			ui.Text(c, s.title()).FontSize(fontSize).TextColor(color).SingleLine().Ellipsis("…").
				Shrink(1).MinWidth(0).Grow(1)
			t.stateDot(c, ch, s, selected)
		})
	})
	row.OnClick(func() { t.focusOn(s) })
}

// groupRow is the row of a tab that splits: the layout it has, the pane the
// window shows, how many panes it holds, and whether that pane runs.
func (t *Plugin) groupRow(c *ui.Context, ch chrome, tb *tab, focused *session, active bool) {
	pal := ch.pal
	row := ui.Box(c).Height(30).FillWidth().Shrink(0).Radius(7).Padding(0, 8)
	switch {
	case active:
		row.Background(pal.Selected)
	case row.Hovered():
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer)
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
			t.layoutMark(c, ch, tb)
			t.sessionIcon(c, ch, focused, 17)
			ui.Text(c, focused.title()).FontSize(12.5).TextColor(pal.Text).SingleLine().
				Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
			if n := len(tb.root.sessions(nil)); n > 1 {
				ui.Box(c).Height(15).Padding(0, 5).Radius(4).Shrink(0).Background(pal.Raised).Center().
					Children(func() {
						ui.Text(c, strconv.Itoa(n)).FontSize(9.5).TextColor(pal.Muted)
					})
			}
			t.stateDot(c, ch, focused, true)
		})
	})
	row.OnClick(func() { t.focusOn(focused) })
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

// stateDot says what a pane holds: working, waiting on the user, done, idle
// or stopped.
func (t *Plugin) stateDot(c *ui.Context, ch chrome, s *session, strong bool) {
	color := sessionStateColor(ch, s)
	size := float32(6)
	if !strong {
		size = 5
	}
	ui.Box(c).Width(size).Height(size).Shrink(0).Radius(size / 2).Background(color)
}

// newTabRow is the foot of the tree: it opens the Sessions, a tab of a
// shell, and a workspace.
func (t *Plugin) newTabRow(c *ui.Context, ch chrome) {
	pal := ch.pal
	ui.Column(c).FillWidth().Shrink(0).Gap(1).Children(func() {
		index := ui.Box(c).Height(28).FillWidth().Radius(7).Padding(0, 8)
		if index.Hovered() {
			index.Background(pal.Hover)
		}
		index.Cursor(ui.CursorPointer)
		index.Children(func() {
			ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "⌗").FontSize(12).TextColor(pal.Faint)
				ui.Text(c, "Sessions").FontSize(12).TextColor(pal.Faint).Grow(1)
				ui.Text(c, "⌘⇧I").FontSize(10).TextColor(pal.Hairline)
			})
		})
		index.OnClick(t.openIndex)

		row := ui.Box(c).Height(28).FillWidth().Radius(7).Padding(0, 8)
		if row.Hovered() {
			row.Background(pal.Hover)
		}
		row.Cursor(ui.CursorPointer)
		row.Children(func() {
			ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "+").FontSize(13).TextColor(pal.Faint)
				ui.Text(c, "New tab").FontSize(12).TextColor(pal.Faint).Grow(1)
				ui.Text(c, "⌘T").FontSize(10).TextColor(pal.Hairline)
			})
		})
		row.OnClick(func() { t.newTab(kindShell) })

		space := ui.Box(c).Height(28).FillWidth().Radius(7).Padding(0, 8)
		if space.Hovered() {
			space.Background(pal.Hover)
		}
		space.Cursor(ui.CursorPointer)
		space.Children(func() {
			ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "+").FontSize(13).TextColor(pal.Faint)
				ui.Text(c, "New workspace").FontSize(12).TextColor(pal.Faint).Grow(1)
				ui.Text(c, "⌘⇧N").FontSize(10).TextColor(pal.Hairline)
			})
		})
		space.OnClick(t.pickWorkspace)
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

// tabTitle is the name of a tab: the pane the window shows, so the tab names
// what the user is working in rather than what it opened with.
func tabTitle(tb *tab) string {
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
		t.actionButton(c, ch, "▤", func() { t.sidebar.Set(!t.sidebar.Get()) })
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
			ui.Text(c, strconv.Itoa(n)).FontSize(10.5).FontWeight(600).TextColor(color)
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
		t.actionButton(c, ch, "▤", func() { t.sidebar.Set(!t.sidebar.Get()) })
		t.actionButton(c, ch, "◫", func() { t.splitFocused(true) })
		t.actionButton(c, ch, "+", func() { t.newTab(kindShell) })
	})
}
