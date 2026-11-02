package vt

import "testing"

// TestLineText reads lines by global index, including history, and copies a
// range.
func TestLineText(t *testing.T) {
	s := New(20, 3)
	s.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\n"))
	if got := s.LineText(s.LastContentLine()); got != "five" {
		t.Fatalf("last = %q", got)
	}
	top := s.VisibleTop()
	if got := s.LineBlock(top, top+1); got == "" {
		t.Fatalf("block from visible top %d is empty", top)
	}
	if got := s.LineText(-1); got != "" {
		t.Fatalf("negative line = %q", got)
	}
}

// TestTextBlock copies a cell range, including a partial first and last line.
func TestTextBlock(t *testing.T) {
	s := New(20, 4)
	s.Write([]byte("alpha bravo\r\ncharlie delta\r\necho\r\n"))
	if got := s.TextBlock(0, 6, 0, 11); got != "bravo" {
		t.Fatalf("one line = %q", got)
	}
	if got := s.TextBlock(0, 6, 1, 7); got != "bravo\ncharlie" {
		t.Fatalf("two lines = %q", got)
	}
	if got := s.TextBlock(1, 7, 0, 6); got != "bravo\ncharlie" {
		t.Fatalf("reversed = %q", got)
	}
}
