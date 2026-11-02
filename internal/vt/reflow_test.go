package vt

import "testing"

// TestReflowWrapsAgain checks that a line wrapped at the margin comes back
// together on a wider screen and wraps again on a narrower one, the cursor
// keeping its place in the line.
func TestReflowWrapsAgain(t *testing.T) {
	s := New(10, 4)
	s.Write([]byte("abcdefghijklmnop")) // ten columns, then six on the next row
	if got := rowText(s, 0); got != "abcdefghij" {
		t.Fatalf("first row = %q", got)
	}
	if c := s.Cursor(); c.X != 6 || c.Y != 1 {
		t.Fatalf("cursor = %d,%d", c.X, c.Y)
	}
	s.Resize(20, 4)
	if got := rowText(s, 0); got != "abcdefghijklmnop" {
		t.Fatalf("one row at 20 columns = %q", got)
	}
	if got := rowText(s, 1); got != "" {
		t.Fatalf("the line did not come together: row 1 = %q", got)
	}
	if c := s.Cursor(); c.X != 16 || c.Y != 0 {
		t.Fatalf("cursor after growing = %d,%d", c.X, c.Y)
	}
	s.Resize(5, 4)
	for y, want := range []string{"abcde", "fghij", "klmno", "p"} {
		if got := rowText(s, y); got != want {
			t.Fatalf("row %d at 5 columns = %q, want %q", y, got, want)
		}
	}
	if c := s.Cursor(); c.X != 1 || c.Y != 3 {
		t.Fatalf("cursor after shrinking = %d,%d", c.X, c.Y)
	}
	// And back again.
	s.Resize(20, 4)
	if got := rowText(s, 0); got != "abcdefghijklmnop" {
		t.Fatalf("after the round trip = %q", got)
	}
}

// TestReflowKeepsNewlines checks that a line ended by a newline is not joined
// with the next one, even when it fills the width.
func TestReflowKeepsNewlines(t *testing.T) {
	s := New(10, 3)
	s.Write([]byte("abcdefghij\r\ndef"))
	s.Resize(20, 3)
	if got := rowText(s, 0); got != "abcdefghij" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(s, 1); got != "def" {
		t.Fatalf("row 1 = %q", got)
	}
	if c := s.Cursor(); c.X != 3 || c.Y != 1 {
		t.Fatalf("cursor = %d,%d", c.X, c.Y)
	}
}

// TestReflowShorterScreenScrolls checks that the rows a shorter screen has no
// room for go to the history, so the cursor stays on show.
func TestReflowShorterScreenScrolls(t *testing.T) {
	s := New(10, 4)
	s.Write([]byte("one\r\ntwo\r\nthree\r\nfour"))
	if s.History() != 0 {
		t.Fatalf("history = %d before the resize", s.History())
	}
	s.Resize(10, 2)
	if s.History() != 2 {
		t.Fatalf("history = %d after the resize", s.History())
	}
	if got := rowText(s, 0); got != "three" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(s, 1); got != "four" {
		t.Fatalf("row 1 = %q", got)
	}
	if c := s.Cursor(); c.X != 4 || c.Y != 1 {
		t.Fatalf("cursor = %d,%d", c.X, c.Y)
	}
}

// TestReflowTighterLines checks that widening a line that wrapped several
// times at a narrow width, with a long word, re-wraps it in order.
func TestReflowTighterLines(t *testing.T) {
	s := New(6, 6)
	s.Write([]byte("one two three"))
	s.Resize(4, 6)
	// "one two three" wraps at 4: "one ", "two ", "thre", "e".
	want := []string{"one", "two", "thre", "e"}
	for y, w := range want {
		if got := rowText(s, y); got != w {
			t.Fatalf("row %d at 4 columns = %q, want %q", y, got, w)
		}
	}
}

// TestAlternateScreenCrops checks that the alternate screen keeps the old
// crop-and-pad resize, as a program that draws it repaints on SIGWINCH.
func TestAlternateScreenCrops(t *testing.T) {
	s := New(10, 3)
	s.Write([]byte("\x1b[?1049habcdefghijkl"))
	s.Resize(5, 3)
	if got := rowText(s, 0); got != "abcde" {
		t.Fatalf("alternate row 0 = %q", got)
	}
	if got := rowText(s, 1); got != "kl" {
		t.Fatalf("alternate row 1 = %q", got)
	}
}
