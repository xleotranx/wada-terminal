package vt

import (
	"strings"
	"testing"
)

// rowText renders a row as text, ignoring the continuation half of wide
// characters and trailing blank cells.
func rowText(s *Screen, y int) string {
	var b strings.Builder
	for _, c := range s.Row(y) {
		if c.Width == 0 {
			continue
		}
		if c.Text == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.Text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func assertRows(t *testing.T, s *Screen, want []string) {
	t.Helper()
	for y := range want {
		if got := rowText(s, y); got != want[y] {
			t.Errorf("row %d = %q, want %q", y, got, want[y])
		}
	}
}

func TestPlainTextAndNewline(t *testing.T) {
	tests := []struct {
		name   string
		cols   int
		rows   int
		writes []string
		want   []string
		x, y   int
	}{
		{
			name: "plain text", cols: 10, rows: 3,
			writes: []string{"hello"},
			want:   []string{"hello", "", ""}, x: 5, y: 0,
		},
		{
			name: "crlf starts a new line at column zero", cols: 10, rows: 3,
			writes: []string{"hello\r\nworld"},
			want:   []string{"hello", "world", ""}, x: 5, y: 1,
		},
		{
			name: "lf alone keeps the column", cols: 10, rows: 2,
			writes: []string{"ab\ncd"},
			want:   []string{"ab", "  cd"}, x: 4, y: 1,
		},
		{
			name: "vertical tab indexes", cols: 10, rows: 2,
			writes: []string{"ab\vcd"},
			want:   []string{"ab", "  cd"}, x: 4, y: 1,
		},
		{
			name: "form feed indexes", cols: 10, rows: 2,
			writes: []string{"ab\fcd"},
			want:   []string{"ab", "  cd"}, x: 4, y: 1,
		},
		{
			name: "line feed at the bottom scrolls", cols: 5, rows: 2,
			writes: []string{"a\r\nb\r\nc"},
			want:   []string{"b", "c"}, x: 1, y: 1,
		},
		{
			name: "carriage return only", cols: 5, rows: 2,
			writes: []string{"ab\rX"},
			want:   []string{"Xb", ""}, x: 1, y: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cols, tt.rows)
			for _, w := range tt.writes {
				s.Write([]byte(w))
			}
			assertRows(t, s, tt.want)
			cur := s.Cursor()
			if cur.X != tt.x || cur.Y != tt.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", cur.X, cur.Y, tt.x, tt.y)
			}
		})
	}

	t.Run("bell", func(t *testing.T) {
		s := New(2, 2)
		if s.Bell() {
			t.Fatal("new screen rang the bell")
		}
		s.Write([]byte("\a"))
		if !s.Bell() {
			t.Fatal("Bell() = false after BEL")
		}
		if s.Bell() {
			t.Fatal("Bell() = true after being read")
		}
	})
}

func TestCarriageReturnOverwrite(t *testing.T) {
	tests := []struct {
		name string
		cols int
		rows int
		in   string
		want []string
	}{
		{"overwrite from column zero", 8, 2, "abc\rXY", []string{"XYc", ""}},
		{"overwrite at column zero", 8, 2, "abc\rX", []string{"Xbc", ""}},
		{"crlf after writing", 8, 2, "abc\r\nz", []string{"abc", "z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cols, tt.rows)
			s.Write([]byte(tt.in))
			assertRows(t, s, tt.want)
		})
	}
}

func TestBackspaceAndTabs(t *testing.T) {
	tests := []struct {
		name  string
		cols  int
		rows  int
		input string
		want  []string
		x, y  int
	}{
		{
			name: "backspace moves left", cols: 10, rows: 2,
			input: "abc\bX", want: []string{"abX", ""}, x: 3, y: 0,
		},
		{
			name: "backspace clamps at column zero", cols: 10, rows: 2,
			input: "ab\b\b\bX", want: []string{"Xb", ""}, x: 1, y: 0,
		},
		{
			name: "tab goes to the next eight column stop", cols: 20, rows: 2,
			input: "a\tb", want: []string{"a" + strings.Repeat(" ", 7) + "b", ""}, x: 9, y: 0,
		},
		{
			name: "tab at the last column stays", cols: 5, rows: 2,
			input: "abcd\tX", want: []string{"abcdX", ""}, x: 4, y: 0,
		},
		{
			name: "custom tab stop", cols: 10, rows: 2,
			input: "\x1b[1;3H\x1bH\r\tX", want: []string{"  X", ""}, x: 3, y: 0,
		},
		{
			name: "tab into an empty short line clamps", cols: 5, rows: 2,
			input: "\tX", want: []string{"    X", ""}, x: 4, y: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cols, tt.rows)
			s.Write([]byte(tt.input))
			assertRows(t, s, tt.want)
			cur := s.Cursor()
			if cur.X != tt.x || cur.Y != tt.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", cur.X, cur.Y, tt.x, tt.y)
			}
		})
	}
}

func TestCursorMovementAndClamping(t *testing.T) {
	tests := []struct {
		name  string
		input string
		x, y  int
	}{
		{"cup", "\x1b[2;3H", 2, 1},
		{"hvp", "\x1b[2;3f", 2, 1},
		{"cup clamps to the grid", "\x1b[99;99H", 9, 4},
		{"cup zero means one", "\x1b[3;3H\x1b[0;0H", 0, 0},
		{"cuu", "\x1b[3;3H\x1b[10A", 2, 0},
		{"cuu default", "\x1b[3;3H\x1b[A", 2, 1},
		{"cud", "\x1b[3;3H\x1b[10B", 2, 4},
		{"cuf", "\x1b[3;3H\x1b[10C", 9, 2},
		{"cub", "\x1b[3;3H\x1b[10D", 0, 2},
		{"cnl", "\x1b[1;1H\x1b[99E", 0, 4},
		{"cpl", "\x1b[5;5H\x1b[99F", 0, 0},
		{"cha", "\x1b[3;3H\x1b[5G", 4, 2},
		{"vpa", "\x1b[3;3H\x1b[5d", 2, 4},
		{"hpa", "\x1b[3;3H\x1b[5`", 4, 2},
		{"cha zero means one", "\x1b[3;3H\x1b[0G", 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(10, 5)
			s.Write([]byte(tt.input))
			cur := s.Cursor()
			if cur.X != tt.x || cur.Y != tt.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", cur.X, cur.Y, tt.x, tt.y)
			}
		})
	}
}

func TestEraseDisplayAndLine(t *testing.T) {
	fill := func() *Screen {
		s := New(4, 3)
		s.Write([]byte("abcd\r\nefgh\r\nijkl"))
		return s
	}

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "ed 0 cursor to end", input: "\x1b[2;2H\x1b[0J",
			want: []string{"abcd", "e", ""},
		},
		{
			name: "ed 1 start to cursor", input: "\x1b[2;2H\x1b[1J",
			want: []string{"", "  gh", "ijkl"},
		},
		{
			name: "ed 2 whole screen", input: "\x1b[2;2H\x1b[2J",
			want: []string{"", "", ""},
		},
		{
			name: "ed default is zero", input: "\x1b[2;2H\x1b[J",
			want: []string{"abcd", "e", ""},
		},
		{
			name: "ed unknown is ignored", input: "\x1b[2;2H\x1b[9J",
			want: []string{"abcd", "efgh", "ijkl"},
		},
		{
			name: "el 0 cursor to end", input: "\x1b[2;2H\x1b[0K",
			want: []string{"abcd", "e", "ijkl"},
		},
		{
			name: "el 1 start to cursor", input: "\x1b[2;2H\x1b[1K",
			want: []string{"abcd", "  gh", "ijkl"},
		},
		{
			name: "el 2 whole line", input: "\x1b[2;2H\x1b[2K",
			want: []string{"abcd", "", "ijkl"},
		},
		{
			name: "el default is zero", input: "\x1b[2;2H\x1b[K",
			want: []string{"abcd", "e", "ijkl"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fill()
			s.Write([]byte(tt.input))
			assertRows(t, s, tt.want)
		})
	}
}

func TestInsertDeleteAndEraseCharacters(t *testing.T) {
	fill := func() *Screen {
		s := New(6, 3)
		s.Write([]byte("abcdef\r\nghijkl\r\nmnopqr"))
		return s
	}

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "ich shifts right", input: "\x1b[2;3H\x1b[2@",
			want: []string{"abcdef", "gh  ij", "mnopqr"},
		},
		{
			name: "ich default one", input: "\x1b[2;3H\x1b[@",
			want: []string{"abcdef", "gh ijk", "mnopqr"},
		},
		{
			name: "ich clamps to the line", input: "\x1b[2;2H\x1b[99@",
			want: []string{"abcdef", "g", "mnopqr"},
		},
		{
			name: "dch shifts left", input: "\x1b[2;3H\x1b[2P",
			want: []string{"abcdef", "ghkl", "mnopqr"},
		},
		{
			name: "dch clamps to the line", input: "\x1b[2;2H\x1b[99P",
			want: []string{"abcdef", "g", "mnopqr"},
		},
		{
			name: "ech erases without shifting", input: "\x1b[2;3H\x1b[2X",
			want: []string{"abcdef", "gh  kl", "mnopqr"},
		},
		{
			name: "ech clamps to the line", input: "\x1b[2;2H\x1b[99X",
			want: []string{"abcdef", "g", "mnopqr"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fill()
			s.Write([]byte(tt.input))
			assertRows(t, s, tt.want)
		})
	}
}

func TestInsertDeleteLines(t *testing.T) {
	fill := func(cols, rows int) *Screen {
		s := New(cols, rows)
		lines := []string{"aaa", "bbb", "ccc", "ddd", "eee"}
		s.Write([]byte(strings.Join(lines[:rows], "\r\n")))
		return s
	}

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "insert line", input: "\x1b[2;1H\x1b[1L",
			want: []string{"aaa", "", "bbb", "ccc"},
		},
		{
			name: "insert lines clamped to region", input: "\x1b[2;1H\x1b[99L",
			want: []string{"aaa", "", "", ""},
		},
		{
			name: "delete line", input: "\x1b[2;1H\x1b[1M",
			want: []string{"aaa", "ccc", "ddd", ""},
		},
		{
			name: "delete lines clamped to region", input: "\x1b[2;1H\x1b[99M",
			want: []string{"aaa", "", "", ""},
		},
		{
			name: "insert ignores a cursor outside the margin", input: "\x1b[2;3r\x1b[1;1H\x1b[1L",
			want: []string{"aaa", "bbb", "ccc", "ddd"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fill(3, 4)
			s.Write([]byte(tt.input))
			assertRows(t, s, tt.want)
		})
	}
}

func TestScrollRegion(t *testing.T) {
	fill := func() *Screen {
		s := New(3, 5)
		s.Write([]byte("aaa\r\nbbb\r\nccc\r\nddd\r\neee"))
		return s
	}

	t.Run("lf at the bottom margin scrolls only the margin", func(t *testing.T) {
		s := fill()
		s.Write([]byte("\x1b[2;4r\x1b[4;1H\nX"))
		assertRows(t, s, []string{"aaa", "ccc", "ddd", "X", "eee"})
		if got := s.History(); got != 0 {
			t.Errorf("History() = %d, want 0 for a partial margin", got)
		}
	})

	t.Run("reverse index at the top margin scrolls down", func(t *testing.T) {
		s := fill()
		s.Write([]byte("\x1b[2;4r\x1b[2;1H\x1bMX"))
		assertRows(t, s, []string{"aaa", "X", "bbb", "ccc", "eee"})
	})

	t.Run("su scrolls the margin up", func(t *testing.T) {
		s := fill()
		s.Write([]byte("\x1b[2;4r\x1b[1S"))
		assertRows(t, s, []string{"aaa", "ccc", "ddd", "", "eee"})
	})

	t.Run("sd scrolls the margin down", func(t *testing.T) {
		s := fill()
		s.Write([]byte("\x1b[2;4r\x1b[1T"))
		assertRows(t, s, []string{"aaa", "", "bbb", "ccc", "eee"})
	})

	t.Run("csi r clears the margin", func(t *testing.T) {
		s := fill()
		s.Write([]byte("\x1b[2;4r\x1b[r\x1b[5;1H\nX"))
		assertRows(t, s, []string{"bbb", "ccc", "ddd", "eee", "X"})
		if got := s.History(); got != 1 {
			t.Errorf("History() = %d, want 1 after a full-screen scroll", got)
		}
	})
}

func TestScrollbackAndView(t *testing.T) {
	s := New(5, 2)
	s.Write([]byte("a\r\nb\r\nc\r\nd\r\n"))

	if got := s.History(); got != 3 {
		t.Fatalf("History() = %d, want 3", got)
	}
	assertRows(t, s, []string{"d", ""})
	if !s.Cursor().Visible {
		t.Fatal("cursor is hidden at the bottom")
	}
	if s.Scrolled() != 0 {
		t.Fatalf("Scrolled() = %d, want 0", s.Scrolled())
	}

	steps := []struct {
		name     string
		delta    int
		scrolled int
		want     []string
	}{
		{"one line up", 1, 1, []string{"c", "d"}},
		{"two lines up", 1, 2, []string{"b", "c"}},
		{"clamps at the oldest line", 100, 3, []string{"a", "b"}},
		{"clamps at the bottom", -100, 0, []string{"d", ""}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			s.Scroll(step.delta)
			if got := s.Scrolled(); got != step.scrolled {
				t.Fatalf("Scrolled() = %d, want %d", got, step.scrolled)
			}
			assertRows(t, s, step.want)
			if step.scrolled == 0 && !s.Cursor().Visible {
				t.Error("cursor is hidden at the bottom")
			}
			if step.scrolled != 0 && s.Cursor().Visible {
				t.Error("cursor is visible while scrolled back")
			}
		})
	}

	// Scrolling up again starts from the bottom.
	s.Scroll(100)
	if got := s.Cell(0, 0); got.Text != "a" {
		t.Errorf("Cell(0,0).Text = %q, want %q", got.Text, "a")
	}
	if got := s.Cell(-1, 0); got != (Cell{}) {
		t.Errorf("Cell(-1,0) = %+v, want zero", got)
	}
	if got := s.Cell(99, 0); got != (Cell{}) {
		t.Errorf("Cell(99,0) = %+v, want zero", got)
	}
	if s.Row(-1) != nil || s.Row(2) != nil {
		t.Error("Row outside the viewport is not nil")
	}
}

func TestScrollbackLimit(t *testing.T) {
	s := New(1, 1)
	for i := 0; i < historyLimit+25; i++ {
		s.Write([]byte("x\r\n"))
	}
	if got := s.History(); got != historyLimit {
		t.Errorf("History() = %d, want %d", got, historyLimit)
	}
	s.Scroll(historyLimit)
	if got := rowText(s, 0); got != "x" {
		t.Errorf("oldest visible line = %q, want %q", got, "x")
	}
}

func TestAlternateScreen(t *testing.T) {
	for _, mode := range []string{"47", "1047", "1049"} {
		t.Run("mode "+mode, func(t *testing.T) {
			s := New(10, 3)
			s.Write([]byte("main"))
			before := s.Cursor()
			s.Write([]byte("\x1b[?" + mode + "h"))
			if !s.Alternate() {
				t.Fatal("Alternate() = false after entering")
			}
			if got := strings.TrimSpace(rowText(s, 0)); got != "" {
				t.Errorf("alternate screen is not clear: %q", got)
			}
			s.Write([]byte("alt"))
			s.Write([]byte("\x1b[?" + mode + "l"))
			if s.Alternate() {
				t.Fatal("Alternate() = true after leaving")
			}
			if got := rowText(s, 0); got != "main" {
				t.Errorf("main row = %q, want %q", got, "main")
			}
			if mode == "1049" {
				if got := s.Cursor(); got.X != before.X || got.Y != before.Y {
					t.Errorf("cursor after 1049 = (%d,%d), want (%d,%d)", got.X, got.Y, before.X, before.Y)
				}
			}
		})
	}

	t.Run("1048 saves and restores the cursor", func(t *testing.T) {
		s := New(5, 2)
		s.Write([]byte("ab\x1b[?1048h\x1b[1;1H"))
		if got := s.Cursor(); got.X != 0 || got.Y != 0 {
			t.Fatalf("cursor after moving = (%d,%d), want (0,0)", got.X, got.Y)
		}
		s.Write([]byte("\x1b[?1048l"))
		if got := s.Cursor(); got.X != 2 || got.Y != 0 {
			t.Errorf("cursor after 1048 restore = (%d,%d), want (2,0)", got.X, got.Y)
		}
	})

	t.Run("alternate screen does not push history", func(t *testing.T) {
		s := New(2, 2)
		s.Write([]byte("\x1b[?1049h"))
		s.Write([]byte("a\r\nb\r\nc"))
		if got := s.History(); got != 0 {
			t.Errorf("History() = %d while on the alternate screen, want 0", got)
		}
		s.Write([]byte("\x1b[?1049l"))
		if got := s.History(); got != 0 {
			t.Errorf("History() = %d after leaving, want 0", got)
		}
	})
}

func TestOriginModeAndMargins(t *testing.T) {
	s := New(5, 5)
	s.Write([]byte("\x1b[2;4r"))
	if got := s.Cursor(); got.X != 0 || got.Y != 0 {
		t.Fatalf("cursor after DECSTBM = (%d,%d), want (0,0)", got.X, got.Y)
	}
	s.Write([]byte("\x1b[?6h"))
	if got := s.Cursor(); got.X != 0 || got.Y != 1 {
		t.Fatalf("cursor after origin mode = (%d,%d), want (0,1)", got.X, got.Y)
	}

	tests := []struct {
		name  string
		input string
		x, y  int
	}{
		{"home is the top margin", "\x1b[1;1H", 0, 1},
		{"rows are relative to the margin", "\x1b[3;2H", 1, 3},
		{"rows stop at the bottom margin", "\x1b[99;99H", 4, 3},
		{"zero row is one", "\x1b[0;0H", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.Write([]byte(tt.input))
			if got := s.Cursor(); got.X != tt.x || got.Y != tt.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", got.X, got.Y, tt.x, tt.y)
			}
		})
	}

	s.Write([]byte("\x1b[?6l"))
	if got := s.Cursor(); got.X != 0 || got.Y != 0 {
		t.Errorf("cursor after leaving origin mode = (%d,%d), want (0,0)", got.X, got.Y)
	}
}

func TestSaveRestoreCursor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		x, y  int
		fg    Color
	}{
		{
			name:  "esc 7 and esc 8",
			input: "\x1b[3;4H\x1b[1;31m\x1b7\x1b[1;1H\x1b8",
			x:     3, y: 2, fg: ANSI(1),
		},
		{
			name:  "csi s and csi u",
			input: "\x1b[3;4H\x1b[1;31m\x1b[s\x1b[1;1H\x1b[u",
			x:     3, y: 2, fg: ANSI(1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(10, 5)
			s.Write([]byte(tt.input))
			cur := s.Cursor()
			if cur.X != tt.x || cur.Y != tt.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", cur.X, cur.Y, tt.x, tt.y)
			}
			if cur.Style.FG != tt.fg {
				t.Errorf("style foreground = %+v, want %+v", cur.Style.FG, tt.fg)
			}
		})
	}
}

func TestSGR(t *testing.T) {
	tests := []struct {
		name string
		seq  string
		want Style
	}{
		{"reset", "\x1b[1;31;44m\x1b[0m", Style{}},
		{"empty parameter resets", "\x1b[1m\x1b[m", Style{}},
		{
			"attributes",
			"\x1b[1;2;3;4;5;7;9m",
			Style{Bold: true, Dim: true, Italic: true, Underline: true, Blink: true, Reverse: true, Strike: true},
		},
		{"21 normal intensity", "\x1b[1;2m\x1b[21m", Style{}},
		{"22 normal intensity", "\x1b[1;2m\x1b[22m", Style{}},
		{
			"attribute offs",
			"\x1b[1;2;3;4;5;7;9m\x1b[23;24;25;27;29m",
			Style{Bold: true, Dim: true},
		},
		{"basic foreground and background", "\x1b[31;44m", Style{FG: ANSI(1), BG: ANSI(4)}},
		{"bright foreground", "\x1b[91m", Style{FG: ANSI(9)}},
		{"bright background", "\x1b[104m", Style{BG: ANSI(12)}},
		{"default colors", "\x1b[31;44m\x1b[39;49m", Style{}},
		{"256 foreground", "\x1b[38;5;196m", Style{FG: ANSI(196)}},
		{"256 background", "\x1b[48;5;21m", Style{BG: ANSI(21)}},
		{"truecolor foreground", "\x1b[38;2;12;34;56m", Style{FG: RGB(12, 34, 56)}},
		{"truecolor background", "\x1b[48;2;1;2;3m", Style{BG: RGB(1, 2, 3)}},
		{"colon 256 foreground", "\x1b[38:5:196m", Style{FG: ANSI(196)}},
		{"colon truecolor foreground", "\x1b[38:2::12:34:56m", Style{FG: RGB(12, 34, 56)}},
		{"colon truecolor background", "\x1b[48:2::1:2:3m", Style{BG: RGB(1, 2, 3)}},
		{"reset then color", "\x1b[1;31m\x1b[0;32m", Style{FG: ANSI(2)}},
		{"reverse", "\x1b[7m", Style{Reverse: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(10, 2)
			s.Write([]byte(tt.seq))
			if got := s.Cursor().Style; got != tt.want {
				t.Errorf("style = %+v, want %+v", got, tt.want)
			}
			s.Write([]byte("X"))
			if got := s.Cell(0, 0).Style; got != tt.want {
				t.Errorf("cell style = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("style applies to later cells only", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte("\x1b[1mA\x1b[22mB"))
		if !s.Cell(0, 0).Style.Bold {
			t.Error("first cell is not bold")
		}
		if s.Cell(1, 0).Style.Bold {
			t.Error("second cell is bold")
		}
	})
}

func TestAutowrap(t *testing.T) {
	t.Run("deferred wrap until the next printable", func(t *testing.T) {
		s := New(5, 2)
		s.Write([]byte("abcde"))
		assertRows(t, s, []string{"abcde", ""})
		if got := s.Cursor(); got.X != 4 || got.Y != 0 {
			t.Fatalf("cursor after a full line = (%d,%d), want (4,0)", got.X, got.Y)
		}
		s.Write([]byte("f"))
		assertRows(t, s, []string{"abcde", "f"})
		if got := s.Cursor(); got.X != 1 || got.Y != 1 {
			t.Errorf("cursor after wrapping = (%d,%d), want (1,1)", got.X, got.Y)
		}
	})

	t.Run("carriage return cancels the pending wrap", func(t *testing.T) {
		s := New(5, 2)
		s.Write([]byte("abcde\rX"))
		assertRows(t, s, []string{"Xbcde", ""})
	})

	t.Run("backspace cancels the pending wrap", func(t *testing.T) {
		s := New(5, 2)
		s.Write([]byte("abcde\bX"))
		assertRows(t, s, []string{"abcXe", ""})
	})

	t.Run("lf while pending does not add a line", func(t *testing.T) {
		s := New(5, 3)
		s.Write([]byte("abcde\nX"))
		assertRows(t, s, []string{"abcde", "X", ""})
		if got := s.Cursor(); got.X != 1 || got.Y != 1 {
			t.Errorf("cursor = (%d,%d), want (1,1)", got.X, got.Y)
		}
	})

	t.Run("lf while pending at the bottom scrolls once", func(t *testing.T) {
		s := New(2, 2)
		s.Write([]byte("ab\r\ncd\nX"))
		assertRows(t, s, []string{"cd", "X"})
		if got := s.History(); got != 1 {
			t.Errorf("History() = %d, want 1", got)
		}
	})

	t.Run("autowrap off overwrites the last column", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte("\x1b[?7labcdef"))
		if got := rowText(s, 0); got != "abcf" {
			t.Errorf("row = %q, want %q", got, "abcf")
		}
		if got := s.Cursor(); got.X != 3 || got.Y != 0 {
			t.Errorf("cursor = (%d,%d), want (3,0)", got.X, got.Y)
		}
	})
}

func TestWideRunesAndCombining(t *testing.T) {
	t.Run("wide rune fills two cells", func(t *testing.T) {
		s := New(6, 2)
		s.Write([]byte("日x"))
		if got := s.Cell(0, 0); got.Text != "日" || got.Width != 2 {
			t.Errorf("cell 0 = %+v, want text 日 width 2", got)
		}
		if got := s.Cell(1, 0); got.Text != "" || got.Width != 0 {
			t.Errorf("continuation cell = %+v, want empty width 0", got)
		}
		if got := s.Cell(2, 0); got.Text != "x" || got.Width != 1 {
			t.Errorf("cell 2 = %+v, want text x width 1", got)
		}
		if got := rowText(s, 0); got != "日x" {
			t.Errorf("row = %q, want %q", got, "日x")
		}
	})

	t.Run("wide rune wraps when it does not fit", func(t *testing.T) {
		s := New(3, 2)
		s.Write([]byte("ab日"))
		assertRows(t, s, []string{"ab", "日"})
		if got := s.Cell(0, 1); got.Width != 2 || got.Text != "日" {
			t.Errorf("wide cell = %+v, want 日 width 2", got)
		}
	})

	t.Run("wide rune at the right edge defers the wrap", func(t *testing.T) {
		s := New(2, 2)
		s.Write([]byte("日X"))
		assertRows(t, s, []string{"日", "X"})
		if got := s.Cursor(); got.X != 1 || got.Y != 1 {
			t.Errorf("cursor = (%d,%d), want (1,1)", got.X, got.Y)
		}
	})

	t.Run("overwriting half a wide rune clears the other half", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte("日\x1b[1;2HX"))
		if got := rowText(s, 0); got != " X" {
			t.Errorf("row = %q, want %q", got, " X")
		}
		if got := s.Cell(0, 0); got.Width != 1 || got.Text != "" {
			t.Errorf("left half = %+v, want blank width 1", got)
		}
	})

	marks := []struct {
		name string
		mark string
	}{
		{"combining acute", "\u0301"},
		{"enclosing mark", "\u20dd"},
		{"spacing combining mark", "\u0903"},
		{"zero width space", "\u200b"},
		{"zero width joiner", "\u200d"},
	}
	for _, tt := range marks {
		t.Run("combining "+tt.name, func(t *testing.T) {
			s := New(4, 1)
			s.Write([]byte("a"))
			s.Write([]byte(tt.mark))
			cell := s.Cell(0, 0)
			if cell.Text != "a"+tt.mark {
				t.Errorf("cell text = %q, want %q", cell.Text, "a"+tt.mark)
			}
			if cell.Width != 1 {
				t.Errorf("cell width = %d, want 1", cell.Width)
			}
			if got := s.Cursor(); got.X != 1 || got.Y != 0 {
				t.Errorf("cursor = (%d,%d), want (1,0)", got.X, got.Y)
			}
		})
	}

	t.Run("combining mark joins a wide rune", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte("日\u0301"))
		if got := s.Cell(0, 0); got.Text != "日\u0301" || got.Width != 2 {
			t.Errorf("cell = %+v, want 日+combining width 2", got)
		}
		if got := s.Cell(1, 0); got.Width != 0 {
			t.Errorf("continuation width = %d, want 0", got.Width)
		}
	})

	t.Run("combining mark with no previous cell is dropped", func(t *testing.T) {
		s := New(2, 1)
		s.Write([]byte("\u0301"))
		if got := rowText(s, 0); got != "" {
			t.Errorf("row = %q, want empty", got)
		}
		if got := s.Cursor(); got.X != 0 {
			t.Errorf("cursor X = %d, want 0", got.X)
		}
	})
}

func TestRuneWidths(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want int
	}{
		{"ascii", 'a', 1},
		{"latin", 'é', 1},
		{"cjk", '日', 2},
		{"hangul", '가', 2},
		{"hiragana", 'あ', 2},
		{"katakana", 'カ', 2},
		{"fullwidth", 'Ａ', 2},
		{"emoji", '😀', 2},
		{"dingbat emoji", '☀', 2},
		{"combining", '\u0301', 0},
		{"zero width joiner", '\u200d', 0},
		{"variation selector", '\ufe0f', 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runeWidth(tt.r); got != tt.want {
				t.Errorf("runeWidth(%U) = %d, want %d", tt.r, got, tt.want)
			}
		})
	}
}

func TestResize(t *testing.T) {
	// What a resize does to a program's lines is in reflow_test.go: the main
	// screen wraps them again, while the alternate screen cropped here keeps
	// its top-left.
	t.Run("keeps history title cwd and alternate state", func(t *testing.T) {
		s := New(3, 2)
		s.Write([]byte("a\r\nb\r\nc"))
		s.Write([]byte("\x1b]0;title\x07\x1b]7;file://host/tmp%20dir\x07"))
		s.Write([]byte("\x1b[?1049h"))
		s.Write([]byte("alt"))
		s.Resize(2, 3)

		if got := s.History(); got != 1 {
			t.Errorf("History() = %d, want 1", got)
		}
		if got := s.Title(); got != "title" {
			t.Errorf("Title() = %q, want %q", got, "title")
		}
		if got := s.Cwd(); got != "/tmp dir" {
			t.Errorf("Cwd() = %q, want %q", got, "/tmp dir")
		}
		if !s.Alternate() {
			t.Error("Alternate() = false after resize")
		}
		if got := rowText(s, 0); got != "al" {
			t.Errorf("alternate row = %q, want %q", got, "al")
		}
		s.Write([]byte("\x1b[?1049l"))
		if got := rowText(s, 0); got != "b" {
			t.Errorf("main row after resize and leaving alt = %q, want %q", got, "b")
		}
	})

	t.Run("zero or negative sizes clamp to one", func(t *testing.T) {
		s := New(0, -2)
		if cols, rows := s.Size(); cols != 1 || rows != 1 {
			t.Errorf("Size() = (%d,%d), want (1,1)", cols, rows)
		}
		s.Resize(-5, 0)
		if cols, rows := s.Size(); cols != 1 || rows != 1 {
			t.Errorf("Size() after bad resize = (%d,%d), want (1,1)", cols, rows)
		}
		if got := s.Cursor(); got.X != 0 || got.Y != 0 || !got.Visible {
			t.Errorf("cursor = %+v, want visible at (0,0)", got)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("soft reset", func(t *testing.T) {
		s := New(6, 3)
		s.Write([]byte("hello\r\nworld"))
		s.Write([]byte("\x1b[1;31m\x1b[2;5r\x1b[?25l\x1b[?7l\x1b[?6h"))
		s.Write([]byte("\x1b]0;title\x07"))
		s.Write([]byte("\x1b[!p"))

		assertRows(t, s, []string{"", "", ""})
		cur := s.Cursor()
		if cur.X != 0 || cur.Y != 0 || !cur.Visible || cur.Style != (Style{}) {
			t.Errorf("cursor after soft reset = %+v, want visible (0,0) with default style", cur)
		}
		if s.scrollTop != 0 || s.scrollBottom != 2 {
			t.Errorf("margin after soft reset = [%d,%d], want [0,2]", s.scrollTop, s.scrollBottom)
		}
		if !s.autoWrap || s.originMode {
			t.Errorf("modes after soft reset: autowrap=%v origin=%v, want true and false", s.autoWrap, s.originMode)
		}
		if got := s.Title(); got != "title" {
			t.Errorf("Title() = %q, want %q", got, "title")
		}
	})

	t.Run("full reset", func(t *testing.T) {
		s := New(4, 2)
		s.Write([]byte("a\r\nb\r\nc"))
		if got := s.History(); got != 1 {
			t.Fatalf("History() before reset = %d, want 1", got)
		}
		s.Write([]byte("\x1b[?1049h"))
		s.Write([]byte("alt"))
		s.Write([]byte("\x1bc"))

		if s.Alternate() {
			t.Error("Alternate() = true after full reset")
		}
		assertRows(t, s, []string{"", ""})
		if got := s.History(); got != 0 {
			t.Errorf("History() after full reset = %d, want 0", got)
		}
		cur := s.Cursor()
		if cur.X != 0 || cur.Y != 0 || !cur.Visible || cur.Style != (Style{}) {
			t.Errorf("cursor after full reset = %+v, want visible (0,0) with default style", cur)
		}

		s.Write([]byte("abcdef"))
		assertRows(t, s, []string{"abcd", "ef"})
	})
}

func TestModesAreStored(t *testing.T) {
	s := New(2, 1)
	s.Write([]byte("\x1b[?1h\x1b[?1002h\x1b[?1006h\x1b[?1015h\x1b[?2004h"))
	if !s.appCursor {
		t.Error("application cursor mode was not stored")
	}
	if s.mouseMode != 1002 {
		t.Errorf("mouse mode = %d, want 1002", s.mouseMode)
	}
	if !s.mouseSGR || !s.mouseURXVT || !s.bracketPaste {
		t.Error("mouse or paste flags were not stored")
	}

	s.Write([]byte("\x1b[?1l\x1b[?1002l\x1b[?1006l\x1b[?1015l\x1b[?2004l"))
	if s.appCursor || s.mouseMode != 0 || s.mouseSGR || s.mouseURXVT || s.bracketPaste {
		t.Error("modes were not cleared")
	}

	s.Write([]byte("\x1b[?25l"))
	if s.Cursor().Visible {
		t.Error("cursor is visible after ?25l")
	}
	s.Write([]byte("\x1b[?25h"))
	if !s.Cursor().Visible {
		t.Error("cursor is hidden after ?25h")
	}
}

func TestClearScrollback(t *testing.T) {
	t.Run("ed 3 clears screen and history", func(t *testing.T) {
		s := New(3, 2)
		s.Write([]byte("a\r\nb\r\nc"))
		if got := s.History(); got != 1 {
			t.Fatalf("History() = %d, want 1", got)
		}
		s.Scroll(1)
		s.Write([]byte("\x1b[1;1H\x1b[3J"))
		if got := s.History(); got != 0 {
			t.Errorf("History() after ED 3 = %d, want 0", got)
		}
		if got := s.Scrolled(); got != 0 {
			t.Errorf("Scrolled() after ED 3 = %d, want 0", got)
		}
		assertRows(t, s, []string{"", ""})
	})

	t.Run("decset 3 clears history only", func(t *testing.T) {
		s := New(3, 2)
		s.Write([]byte("a\r\nb\r\nc"))
		assertRows(t, s, []string{"b", "c"})
		s.Write([]byte("\x1b[?3h"))
		if got := s.History(); got != 0 {
			t.Errorf("History() after ?3h = %d, want 0", got)
		}
		assertRows(t, s, []string{"b", "c"})
	})
}
