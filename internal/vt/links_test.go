package vt

import "testing"

// TestHyperlinks checks that cells written while OSC 8 is open carry the
// link, and that closing it stops.
func TestHyperlinks(t *testing.T) {
	s := New(20, 3)
	s.Write([]byte("\x1b]8;;https://example.com\x07link\x1b]8;;\x07 plain"))
	row := s.Row(0)
	if got := s.LinkOf(row[0]); got != "https://example.com" {
		t.Fatalf("cell 0 links to %q", got)
	}
	if row[0].Link != row[3].Link {
		t.Fatal("the cells of one link do not share it")
	}
	if got := s.LinkOf(row[4]); got != "" {
		t.Fatalf("the space after the link links to %q", got)
	}
	if got := s.LinkOf(row[5]); got != "" {
		t.Fatalf("text after the link links to %q", got)
	}
	// Two links keep their own numbers.
	s.Write([]byte("\x1b]8;;https://a.example\x07A\x1b]8;;\x07\x1b]8;;https://b.example\x07B\x1b]8;;\x07"))
	line := s.Row(0)
	if row[0].Link == line[10].Link {
		t.Fatal("two links share a number")
	}
	if got := s.LinkOf(line[10]); got != "https://a.example" {
		t.Fatalf("the link after the text is %q", got)
	}
	if got := s.LinkOf(line[11]); got != "https://b.example" {
		t.Fatalf("the second link is %q", got)
	}
}

// TestHyperlinkParams checks that the parameters of OSC 8 are skipped.
func TestHyperlinkParams(t *testing.T) {
	s := New(20, 3)
	s.Write([]byte("\x1b]8;id=42;https://example.com/x\x07X\x1b]8;;\x07"))
	if got := s.LinkOf(s.Row(0)[0]); got != "https://example.com/x" {
		t.Fatalf("link with params = %q", got)
	}
}

// TestMouseState checks what the private modes report.
func TestMouseState(t *testing.T) {
	s := New(20, 3)
	if m := s.Mouse(); m.Mode != 0 || m.SGR || m.URXVT {
		t.Fatalf("a fresh screen reports %+v", m)
	}
	s.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	if m := s.Mouse(); m.Mode != 1002 || !m.SGR {
		t.Fatalf("1002 with SGR = %+v", m)
	}
	s.Write([]byte("\x1b[?1002l"))
	if m := s.Mouse(); m.Mode != 0 || !m.SGR {
		t.Fatalf("after turning 1002 off = %+v", m)
	}
	s.Write([]byte("\x1b[?9h\x1b[?1015h"))
	if m := s.Mouse(); m.Mode != 9 || !m.URXVT {
		t.Fatalf("X10 with urxvt = %+v", m)
	}
	s.Write([]byte("\x1b[?1003h"))
	if m := s.Mouse(); m.Mode != 1003 {
		t.Fatalf("1003 replaced 9? %+v", m)
	}
}

// TestLinks lists linked runs, including in history.
func TestLinks(t *testing.T) {
	s := New(20, 4)
	s.Write([]byte("\x1b]8;;https://a.example\x07A\x07\x1b]8;;\x07 plain \x1b]8;;https://b.example\x07B\x07\x1b]8;;\x07\n"))
	links := s.Links()
	if len(links) != 2 || links[0].URL != "https://a.example" || links[1].URL != "https://b.example" {
		t.Fatalf("links = %+v", links)
	}
	if links[0].Start != 0 || links[0].End != 1 || links[1].Start != 8 {
		t.Fatalf("positions = %+v", links)
	}
}
