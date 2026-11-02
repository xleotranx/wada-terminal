package vt

import (
	"fmt"
	"strings"
	"testing"
)

// TestScrollbackRingOrder checks the ring that keeps the scrollback: once it
// holds historyLimit lines, a new one overwrites the oldest, and the lines it
// holds stay in the order the program wrote them.
func TestScrollbackRingOrder(t *testing.T) {
	const total = historyLimit + 25
	s := New(12, 2)
	var b strings.Builder
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "L%05d\r\n", i)
	}
	s.Write([]byte(b.String()))

	if got := s.History(); got != historyLimit {
		t.Fatalf("History() = %d, want %d", got, historyLimit)
	}
	// Writing N lines pushes the first N-1 of them; the last one is on the
	// grid. The scrollback keeps the newest historyLimit of those.
	first := total - 1 - historyLimit
	want := func(line int) string { return fmt.Sprintf("L%05d", line) }
	if got := s.LineText(0); got != want(first) {
		t.Errorf("oldest line = %q, want %q", got, want(first))
	}
	if got := s.LineText(historyLimit - 1); got != want(total-2) {
		t.Errorf("newest history line = %q, want %q", got, want(total-2))
	}
	if got := s.LineText(historyLimit); got != want(total-1) {
		t.Errorf("first grid line = %q, want %q", got, want(total-1))
	}
}

// TestScrolledViewKeepsItsLines checks that lines scrolling in while the view
// is back in the scrollback do not move the lines the view shows.
func TestScrolledViewKeepsItsLines(t *testing.T) {
	s := New(8, 3)
	s.Write([]byte("a\r\nb\r\nc\r\nd\r\ne\r\nf\r\n"))
	s.Scroll(2)
	before := []string{rowText(s, 0), rowText(s, 1), rowText(s, 2)}

	s.Write([]byte("g\r\nh\r\n"))
	after := []string{rowText(s, 0), rowText(s, 1), rowText(s, 2)}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row %d = %q after output, want %q", i, after[i], before[i])
		}
	}
}

// TestScrollingReusesRows checks that a long stream of output does not keep
// taking rows from the heap: the rows a scroll empties come back for the next
// one.
func TestScrollingReusesRows(t *testing.T) {
	s := New(10, 4)
	s.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\n"))
	for i := 0; i < 3; i++ {
		s.Write([]byte("x\r\n"))
	}
	allocs := testing.AllocsPerRun(50, func() {
		s.Write([]byte("y\r\n"))
	})
	// A scroll that pushes a line into a full scrollback reuses the row it
	// drops, so the steady state costs no per-line row allocation.
	if allocs > 2 {
		t.Errorf("allocs per scrolled line = %.1f, want at most 2", allocs)
	}
}

// TestAlternateScreenScroll checks the full-screen scroll of the alternate
// buffer, which keeps no scrollback.
func TestAlternateScreenScroll(t *testing.T) {
	s := New(6, 3)
	s.Write([]byte("\x1b[?1049h"))
	s.Write([]byte("a\r\nb\r\nc\r\nd\r\n"))
	assertRows(t, s, []string{"c", "d", ""})
	if got := s.History(); got != 0 {
		t.Errorf("History() = %d on the alternate screen, want 0", got)
	}
	s.Write([]byte("\x1b[?1049l"))
	if got := s.History(); got != 0 {
		t.Errorf("History() = %d after leaving, want 0", got)
	}
}

// BenchmarkScreenStream measures a program's output through the emulator at
// the size of a pane, which is the hot path of a terminal under a flood.
func BenchmarkScreenStream(b *testing.B) {
	var chunk []byte
	line := []byte("the quick brown fox jumps over the lazy dog 0123456789\r\n")
	for len(chunk) < 64<<10 {
		chunk = append(chunk, line...)
	}
	b.SetBytes(int64(len(chunk)))
	s := New(114, 42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Write(chunk)
	}
}
