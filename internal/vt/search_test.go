package vt

import "testing"

// TestSearch finds matches in the grid and in scrollback and scrolls to them.
func TestSearch(t *testing.T) {
	screen := New(20, 3)
	screen.Write([]byte("one alpha two\r\nbeta Alpha\r\ngamma\r\ndelta alpha\r\n"))
	matches := screen.Search("alpha")
	if len(matches) != 3 {
		t.Fatalf("matches = %+v", matches)
	}
	if matches[0].Line != 0 || matches[0].Start != 4 || matches[0].End != 9 {
		t.Fatalf("first = %+v", matches[0])
	}
	// Push the first line into history and find it again.
	screen.Write([]byte("x\r\ny\r\nz\r\n"))
	matches = screen.Search("alpha")
	if len(matches) != 3 {
		t.Fatalf("history matches = %+v", matches)
	}
	if len(screen.Search("ALPHA")) != len(matches) {
		t.Fatal("search is not case insensitive")
	}
	screen.ScrollToLine(matches[0].Line)
	if screen.Scrolled() == 0 {
		t.Fatal("ScrollToLine did not move into history")
	}
	if !screen.SearchHighlight(matches, 0, matches[0].Start) {
		t.Fatal("the match is not highlighted on the shown row")
	}
	if len(screen.Search("")) != 0 {
		t.Fatal("an empty query matched")
	}
}
