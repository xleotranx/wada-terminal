package terminal

import (
	"strconv"

	"wada-terminal/internal/vt"

	"github.com/egoist/mygo/ui"
)

// searchState is the in-pane find bar and the matches on the focused screen.
type searchState struct {
	open    bool
	input   string
	session *session
	matches []vt.Match
	index   int
}

// openSearch shows the find bar for the focused pane, or hides it.
func (t *Plugin) openSearch() {
	if t.search.open {
		t.closeSearch()
		return
	}
	s := t.currentFocus()
	if s == nil {
		return
	}
	t.search = searchState{open: true, session: s, index: -1}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// closeSearch hides the bar and stops highlighting.
func (t *Plugin) closeSearch() {
	t.search = searchState{}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// searchApply scans the screen for the current query and shows the first hit.
func (t *Plugin) searchApply() {
	s := t.search.session
	if s == nil {
		return
	}
	t.search.matches = s.screen.Search(t.search.input)
	t.search.index = 0
	if len(t.search.matches) > 0 {
		s.screen.ScrollToLine(t.search.matches[0].Line)
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// searchNext moves to the next or previous match and scrolls the screen.
func (t *Plugin) searchNext(delta int) {
	if t.search.session == nil || len(t.search.matches) == 0 {
		return
	}
	n := len(t.search.matches)
	t.search.index = (t.search.index + delta + n) % n
	t.search.session.screen.ScrollToLine(t.search.matches[t.search.index].Line)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// searchHighlight reports whether a cell is part of a match in this session.
func (t *Plugin) searchHighlight(s *session, y, x int) bool {
	if !t.search.open || t.search.session != s || len(t.search.matches) == 0 {
		return false
	}
	return s.screen.SearchHighlight(t.search.matches, y, x)
}

// searchBar draws the find field above the panes.
func (t *Plugin) searchBar(c *ui.Context, ch chrome) {
	pal := ch.pal
	ui.Row(c).Height(36).FillWidth().Shrink(0).Padding(0, 10).AlignItems(ui.Center).Gap(8).
		Background(pal.Raised).Children(func() {
		ui.Text(c, "Find").FontSize(10.5).TextColor(pal.Muted)
		input := ui.TextInput(c, &t.search.input).Grow(1).FontSize(11.5).
			Placeholder("Find in scrollback").AutoFocus()
		changed := input.Changed()
		submitted := input.Submitted()
		count := ""
		if t.search.input != "" {
			if len(t.search.matches) == 0 {
				count = "No matches"
			} else {
				count = strconv.Itoa(t.search.index+1) + "/" + strconv.Itoa(len(t.search.matches))
			}
		}
		if count != "" {
			ui.Text(c, count).FontSize(10).TextColor(pal.Faint).Shrink(0)
		}
		prev := ui.Box(c).Width(20).Height(20).Shrink(0).Radius(4).Center().Cursor(ui.CursorPointer)
		if prev.Hovered() {
			prev.Background(pal.Hover)
		}
		prev.Children(func() { ui.Text(c, "↑").FontSize(10).TextColor(pal.Muted) })
		prev.OnClick(func() { t.searchNext(-1) })
		next := ui.Box(c).Width(20).Height(20).Shrink(0).Radius(4).Center().Cursor(ui.CursorPointer)
		if next.Hovered() {
			next.Background(pal.Hover)
		}
		next.Children(func() { ui.Text(c, "↓").FontSize(10).TextColor(pal.Muted) })
		next.OnClick(func() { t.searchNext(1) })
		close := ui.Box(c).Width(20).Height(20).Shrink(0).Radius(4).Center().Cursor(ui.CursorPointer)
		if close.Hovered() {
			close.Background(pal.Hover)
		}
		close.Children(func() { ui.Text(c, "✕").FontSize(9).TextColor(pal.Faint) })
		close.OnClick(t.closeSearch)
		if changed {
			t.ctx.Post(t.searchApply)
		}
		if submitted {
			t.ctx.Post(func() { t.searchNext(1) })
		}
	})
}

// currentFocus is the pane with the keyboard, if any.
func (t *Plugin) currentFocus() *session {
	if t.current == nil {
		return nil
	}
	return t.current.focus
}

// viHighlight reports whether the cell at row y, column x is inside the
// visual selection.
func (t *Plugin) viHighlight(s *session, y, x int) bool {
	if s == nil || !s.scrollMode || !s.viVisual {
		return false
	}
	line := s.screen.VisibleTop() + y
	startLine, startCol := s.viAnchor, s.viAnchorCol
	endLine, endCol := s.viCursor, s.viCursorCol
	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}
	if line < startLine || line > endLine {
		return false
	}
	if s.viLinewise {
		return true
	}
	if startLine == endLine {
		return line == startLine && x >= startCol && x <= endCol
	}
	switch line {
	case startLine:
		return x >= startCol
	case endLine:
		return x <= endCol
	default:
		return true
	}
}
