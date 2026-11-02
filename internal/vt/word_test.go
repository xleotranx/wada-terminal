package vt

import "testing"

// TestWordMotions moves by words and line ends.
func TestWordMotions(t *testing.T) {
	s := New(30, 3)
	s.Write([]byte("alpha beta\r\ngamma delta\r\n"))
	if line, col, ok := s.WordStart(0, 0); !ok || line != 0 || col != 6 {
		t.Fatalf("next word = %d,%d,%v", line, col, ok)
	}
	if line, col, ok := s.WordStart(0, 6); !ok || line != 1 || col != 0 {
		t.Fatalf("wrapped word = %d,%d,%v", line, col, ok)
	}
	if line, col, ok := s.WordBack(1, 0); !ok || line != 0 || col != 6 {
		t.Fatalf("previous word = %d,%d,%v", line, col, ok)
	}
	if line, col, ok := s.WordEnd(0, 0); !ok || line != 0 || col != 4 {
		t.Fatalf("word end = %d,%d,%v", line, col, ok)
	}
	if got := s.LineEnd(0); got != 9 {
		t.Fatalf("line end = %d", got)
	}
}
