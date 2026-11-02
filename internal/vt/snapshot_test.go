package vt

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"
)

// TestSnapshotRestoresTheScreen checks that a snapshot carries the cells, the
// colors, the scrollback, the cursor, the title and the status records, and
// that restoring it paints the same screen.
func TestSnapshotRestoresTheScreen(t *testing.T) {
	s := New(20, 5)
	s.SetColors([3]uint8{0xee, 0xee, 0xee}, [3]uint8{0x11, 0x11, 0x11})
	s.Write([]byte("\x1b[31mred\x1b[0m \x1b[1;44mbold blue\x1b[0m\r\n\r\n"))
	s.Write([]byte("界 wide\r\n"))
	for i := 0; i < 8; i++ {
		s.Write([]byte("line\r\n"))
	}
	s.Write([]byte("\x1b]2;A title\x07\x1b]7;file:///tmp\x07"))
	s.Write([]byte("\x1b]7501;state=working:app=cargo:progress=40\x1b\\"))
	s.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	s.Write([]byte("\x1b[2;3H"))

	snap := s.Snapshot()
	restored := New(1, 1)
	restored.Restore(snap)

	cols, rows := s.Size()
	gotCols, gotRows := restored.Size()
	if gotCols != cols || gotRows != rows {
		t.Fatalf("restored size = %dx%d, want %dx%d", gotCols, gotRows, cols, rows)
	}
	if restored.Title() != "A title" || restored.Cwd() != "/tmp" {
		t.Fatalf("title/cwd = %q/%q", restored.Title(), restored.Cwd())
	}
	if got, want := restored.Cursor(), s.Cursor(); got.X != want.X || got.Y != want.Y || got.Visible != want.Visible {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	if got, want := restored.Mouse(), s.Mouse(); got != want {
		t.Fatalf("mouse = %+v, want %+v", got, want)
	}
	if st, ok := restored.Status(); !ok || st.State != "working" || st.Progress != 40 || st.App != "cargo" {
		t.Fatalf("status = %+v, ok=%v", st, ok)
	}
	if !restored.Reported() {
		t.Fatal("restored screen forgot that the program reports")
	}
	if restored.History() != s.History() {
		t.Fatalf("history = %d lines, want %d", restored.History(), s.History())
	}
	for y := 0; y < rows; y++ {
		got, want := restored.Row(y), s.Row(y)
		if len(got) != len(want) {
			t.Fatalf("row %d has %d cells, want %d", y, len(got), len(want))
		}
		for x := range want {
			if got[x] != want[x] {
				t.Fatalf("cell %d,%d = %+v, want %+v", x, y, got[x], want[x])
			}
		}
	}
}

// TestSnapshotIsIndependent checks that changing a screen after taking its
// snapshot does not change the snapshot, and that the cells are copies.
func TestSnapshotIsIndependent(t *testing.T) {
	s := New(10, 3)
	s.Write([]byte("hello"))
	snap := s.Snapshot()
	s.Write([]byte(" world"))
	if cell := stringCell(snap.Main[0]); cell != "h" {
		t.Fatalf("snapshot changed with the screen: first cell %q", cell)
	}
	restored := New(10, 3)
	restored.Restore(snap)
	if got := cellsText(restored.Row(0)); got != "hello" {
		t.Fatalf("restored row = %q, want hello", got)
	}
}

// TestSnapshotGobRoundTrip checks the encoding the core uses on the wire.
func TestSnapshotGobRoundTrip(t *testing.T) {
	s := New(12, 4)
	s.Write([]byte("\x1b[?1049h\x1b[38;5;200malt\x1b[0m"))
	snap := s.Snapshot()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snap); err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
		t.Fatal(err)
	}
	restored := New(1, 1)
	restored.Restore(out)
	if !restored.Alternate() {
		t.Fatal("the alternate screen was lost")
	}
	if got := cellsText(restored.Row(0)); got != "alt" {
		t.Fatalf("restored row = %q, want alt", got)
	}
	if cell := restored.Row(0)[0]; cell.Style.FG != ANSI(200) {
		t.Fatalf("cell color = %+v, want ANSI 200", cell.Style.FG)
	}
}

// stringCell is the text of a cell, for the tests above.
func stringCell(c Cell) string { return c.Text }

// rowText is a row's text without trailing blanks.
func cellsText(row []Cell) string {
	var b strings.Builder
	for _, cell := range row {
		if cell.Width == 0 {
			continue
		}
		if cell.Text == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(cell.Text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}
