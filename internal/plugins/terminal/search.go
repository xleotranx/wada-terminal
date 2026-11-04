package terminal

import (
	"strconv"

	mterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// searchState is the in-pane find bar. The focused pane's terminal searches
// its screen and scrollback, highlights the matches and scrolls to the one
// selected; status is how far it got, as it tells (Options.OnSearch).
type searchState struct {
	open    bool
	input   string
	session *session
	status  mterm.SearchStatus
}

// openSearch shows the find bar for the focused pane, or hides it.
func (t *Plugin) openSearch() {
	if t.search.open {
		t.closeSearch()
		return
	}
	s := t.currentFocus()
	if s == nil || s.term == nil {
		return
	}
	t.search = searchState{open: true, session: s}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// closeSearch hides the bar and ends the search, with its highlights.
func (t *Plugin) closeSearch() {
	if s := t.search.session; s != nil && s.term != nil {
		s.term.EndSearch()
	}
	t.search = searchState{}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// searchApply searches the pane for the current query.
func (t *Plugin) searchApply() {
	s := t.search.session
	if s == nil || s.term == nil {
		return
	}
	t.search.status = mterm.SearchStatus{}
	s.term.Search(t.search.input)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// searchNext selects the next match up into the scrollback, or with a
// negative delta the one toward newer output, and shows it.
func (t *Plugin) searchNext(delta int) {
	s := t.search.session
	if s == nil || s.term == nil {
		return
	}
	if delta < 0 {
		s.term.SearchPrevious()
	} else {
		s.term.SearchNext()
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
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
		if st := t.search.status; t.search.input != "" && st.Text == t.search.input {
			switch {
			case st.Total > 0:
				count = strconv.Itoa(st.Selected) + "/" + strconv.Itoa(st.Total)
			case !st.Searching:
				count = "No matches"
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
		prev.OnClick(func() { t.searchNext(1) })
		next := ui.Box(c).Width(20).Height(20).Shrink(0).Radius(4).Center().Cursor(ui.CursorPointer)
		if next.Hovered() {
			next.Background(pal.Hover)
		}
		next.Children(func() { ui.Text(c, "↓").FontSize(10).TextColor(pal.Muted) })
		next.OnClick(func() { t.searchNext(-1) })
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
