package terminal

import (
	"testing"

	"wada-terminal/internal/vt"
)

// screenOf makes a session with a screen showing text, without a program.
func screenOf(t *testing.T, cols, rows int, text string) *session {
	t.Helper()
	s := &session{screen: vt.New(cols, rows), alive: true}
	s.screen.Write([]byte(text))
	return s
}

// TestSelectedText checks what a selection copies: a row's cells, blanks at
// the end left out, a line per row.
func TestSelectedText(t *testing.T) {
	s := screenOf(t, 20, 4, "hello world\r\nsecond line\r\n")
	if got := s.selectedText(); got != "" {
		t.Fatalf("nothing selected copies %q", got)
	}
	s.begin(0, 0)
	s.extend(10, 0)
	s.finish()
	if got := s.selectedText(); got != "hello world" {
		t.Fatalf("one row = %q", got)
	}
	s.begin(6, 0)
	s.extend(4, 1)
	s.finish()
	if got := s.selectedText(); got != "world\nsecon" {
		t.Fatalf("two rows = %q", got)
	}
	// A selection that ends on blanks leaves them out.
	s.begin(6, 0)
	s.extend(14, 0)
	s.finish()
	if got := s.selectedText(); got != "world" {
		t.Fatalf("trailing blanks = %q", got)
	}
}

// TestSelectionClicksAndCovers checks that a press without a drag selects
// nothing, that ranges work in either direction, and that select all covers
// the screen.
func TestSelectionClicksAndCovers(t *testing.T) {
	s := screenOf(t, 10, 3, "abc\r\ndef\r\n")
	s.begin(2, 1)
	s.finish()
	if s.sel.on {
		t.Fatal("a press without a drag kept a selection")
	}
	s.begin(5, 2)
	s.extend(1, 0)
	s.finish()
	x0, y0, x1, y1, ok := s.selRange()
	if !ok || x0 != 1 || y0 != 0 || x1 != 5 || y1 != 2 {
		t.Fatalf("range = %d,%d..%d,%d", x0, y0, x1, y1)
	}
	if !s.selected(1, 0) || !s.selected(5, 2) || s.selected(0, 0) || s.selected(6, 2) {
		t.Fatal("the selected cells are wrong")
	}
	s.selectAll()
	if !s.selected(0, 0) || !s.selected(9, 2) {
		t.Fatal("select all did not cover the screen")
	}
	if got := s.selectedText(); got != "abc\ndef" {
		t.Fatalf("select all = %q", got)
	}
}

// TestSelectWordAndLine checks what a double and a triple click select.
func TestSelectWordAndLine(t *testing.T) {
	s := screenOf(t, 40, 3, "run echo hello world\r\npath: src/main.go\r\n")
	s.selectWordAt(5, 0)
	if got := s.selectedText(); got != "echo" {
		t.Fatalf("double click selected %q", got)
	}
	s.selectWordAt(12, 1)
	if got := s.selectedText(); got != "src/main.go" {
		t.Fatalf("double click on a path selected %q", got)
	}
	s.selectWordAt(25, 0) // a blank past the text: an empty selection
	if got := s.selectedText(); got != "" {
		t.Fatalf("double click on a blank selected %q", got)
	}
	s.selectLineAt(0)
	if got := s.selectedText(); got != "run echo hello world" {
		t.Fatalf("triple click selected %q", got)
	}
}
