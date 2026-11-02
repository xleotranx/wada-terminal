package vt

import (
	"strings"
	"testing"
)

// TestTailReadsScrollbackAndGrid checks that Tail is the last lines of what the
// program wrote, with the scrollback before the grid.
func TestTailReadsScrollbackAndGrid(t *testing.T) {
	s := New(20, 4)
	for i := 0; i < 10; i++ {
		s.Write([]byte("line " + string(rune('0'+i)) + "\r\n"))
	}
	got := s.Tail(3)
	want := []string{"line 7", "line 8", "line 9"}
	if len(got) != len(want) {
		t.Fatalf("Tail(3) = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tail(3) = %q, want %q", got, want)
		}
	}
	if got := s.Tail(100); len(got) != 10 {
		t.Fatalf("Tail(100) has %d lines, want 10: %q", len(got), got)
	}
	if got := s.Tail(0); got != nil {
		t.Fatalf("Tail(0) = %q, want nil", got)
	}
}

// TestTailIgnoresAScrolledView checks that a viewer scrolling back does not
// change what a program last printed.
func TestTailIgnoresAScrolledView(t *testing.T) {
	s := New(20, 4)
	for i := 0; i < 10; i++ {
		s.Write([]byte("line " + string(rune('0'+i)) + "\r\n"))
	}
	s.Scroll(4)
	if s.Scrolled() == 0 {
		t.Fatal("the test did not scroll the view")
	}
	if got := s.Tail(1); len(got) != 1 || got[0] != "line 9" {
		t.Fatalf("Tail(1) = %q, want the live bottom", got)
	}
}

// TestTailKeepsWideCharacters checks that a wide character is one string, not
// a glyph and a blank.
func TestTailKeepsWideCharacters(t *testing.T) {
	s := New(10, 2)
	s.Write([]byte("界 ok"))
	if got := s.Tail(1); len(got) != 1 || !strings.HasPrefix(got[0], "界 ok") {
		t.Fatalf("Tail(1) = %q", got)
	}
}
