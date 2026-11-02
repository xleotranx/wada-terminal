package vt

import (
	"testing"
)

func TestUTF8SplitAcrossWrites(t *testing.T) {
	input := []byte("a日é😀")
	for split := 0; split <= len(input); split++ {
		t.Run("split "+itoa(split), func(t *testing.T) {
			s := New(10, 2)
			s.Write(input[:split])
			s.Write(input[split:])
			if got := rowText(s, 0); got != "a日é😀" {
				t.Errorf("row = %q, want %q", got, "a日é😀")
			}
			if got := s.Cursor().X; got != 6 {
				t.Errorf("cursor X = %d, want 6", got)
			}
		})
	}

	// A combining mark split from its bytes and from its base character still
	// joins the cell before it.
	mark := []byte("\u0301")
	for split := 0; split <= len(mark); split++ {
		t.Run("combining split "+itoa(split), func(t *testing.T) {
			s := New(4, 1)
			s.Write([]byte("e"))
			s.Write(mark[:split])
			s.Write(mark[split:])
			if got := s.Cell(0, 0).Text; got != "e\u0301" {
				t.Errorf("cell = %q, want %q", got, "e\u0301")
			}
		})
	}
}

func TestCSISplitAcrossWrites(t *testing.T) {
	seq := []byte("\x1b[2;3HX")
	for split := 0; split <= len(seq); split++ {
		t.Run("split "+itoa(split), func(t *testing.T) {
			s := New(10, 5)
			s.Write(seq[:split])
			s.Write(seq[split:])
			if got := s.Cursor(); got.X != 3 || got.Y != 1 {
				t.Errorf("cursor = (%d,%d), want (3,1)", got.X, got.Y)
			}
			if got := rowText(s, 1); got != "  X" {
				t.Errorf("row 1 = %q, want %q", got, "  X")
			}
		})
	}

	t.Run("split sgr", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte("\x1b[1;3"))
		s.Write([]byte("8;5;196mX"))
		if got := s.Cell(0, 0).Style; got.FG != ANSI(196) || !got.Bold {
			t.Errorf("style = %+v, want bold 256-color 196", got)
		}
	})
}

func TestOSCSplitAcrossWrites(t *testing.T) {
	bel := []byte("\x1b]0;split title\x07")
	for split := 0; split <= len(bel); split++ {
		t.Run("bel split "+itoa(split), func(t *testing.T) {
			s := New(2, 2)
			s.Write(bel[:split])
			s.Write(bel[split:])
			if got := s.Title(); got != "split title" {
				t.Errorf("Title() = %q, want %q", got, "split title")
			}
		})
	}

	st := []byte("\x1b]2;st title\x1b\\")
	for split := 0; split <= len(st); split++ {
		t.Run("st split "+itoa(split), func(t *testing.T) {
			s := New(2, 2)
			s.Write(st[:split])
			s.Write(st[split:])
			if got := s.Title(); got != "st title" {
				t.Errorf("Title() = %q, want %q", got, "st title")
			}
		})
	}

	t.Run("cwd split", func(t *testing.T) {
		s := New(2, 2)
		s.Write([]byte("\x1b]7;file://host/tmp/"))
		s.Write([]byte("a%20b"))
		s.Write([]byte("\x1b"))
		s.Write([]byte("\\"))
		if got := s.Cwd(); got != "/tmp/a b" {
			t.Errorf("Cwd() = %q, want %q", got, "/tmp/a b")
		}
	})
}

func TestOSCTitleAndCwd(t *testing.T) {
	tests := []struct {
		name      string
		writes    []string
		wantTitle string
		wantCwd   string
	}{
		{
			name: "osc 0 title terminated by bel", writes: []string{"\x1b]0;hello\x07"},
			wantTitle: "hello",
		},
		{
			name: "osc 2 title terminated by st", writes: []string{"\x1b]2;world\x1b\\"},
			wantTitle: "world",
		},
		{
			name: "title may contain semicolons", writes: []string{"\x1b]0;a;b;c\x07"},
			wantTitle: "a;b;c",
		},
		{
			name: "last title wins", writes: []string{"\x1b]0;one\x07", "\x1b]2;two\x07"},
			wantTitle: "two",
		},
		{
			name: "osc 1 is not a title", writes: []string{"\x1b]1;icon\x07"},
		},
		{
			name: "osc 4 is ignored", writes: []string{"\x1b]4;1;rgb:00/00/00\x07"},
		},
		{
			name: "osc 8 is ignored", writes: []string{"\x1b]8;;https://example.com\x07"},
		},
		{
			name: "cwd from a file url", writes: []string{"\x1b]7;file://host/tmp/a%20b\x07"},
			wantCwd: "/tmp/a b",
		},
		{
			name: "cwd from an empty host", writes: []string{"\x1b]7;file:///tmp/a%20b\x07"},
			wantCwd: "/tmp/a b",
		},
		{
			name: "cwd percent-decodes utf8", writes: []string{"\x1b]7;file://host/tmp/%E6%97%A5\x07"},
			wantCwd: "/tmp/日",
		},
		{
			name: "last cwd wins", writes: []string{"\x1b]7;file://host/one\x07", "\x1b]7;file://host/two\x07"},
			wantCwd: "/two",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(4, 2)
			for _, w := range tt.writes {
				s.Write([]byte(w))
			}
			if got := s.Title(); got != tt.wantTitle {
				t.Errorf("Title() = %q, want %q", got, tt.wantTitle)
			}
			if got := s.Cwd(); got != tt.wantCwd {
				t.Errorf("Cwd() = %q, want %q", got, tt.wantCwd)
			}
			if s.Bell() {
				t.Error("an OSC terminator rang the bell")
			}
		})
	}
}

func TestDCSAndOtherStringsAreIgnored(t *testing.T) {
	for _, intro := range []string{"P", "_", "^", "X"} {
		t.Run("intro "+intro, func(t *testing.T) {
			s := New(10, 2)
			s.Write([]byte("\x1b" + intro + "ignored \x1b[31m text\x1b\\visible"))
			if got := rowText(s, 0); got != "visible" {
				t.Errorf("row = %q, want %q", got, "visible")
			}
		})
	}

	t.Run("bel inside dcs is ignored", func(t *testing.T) {
		s := New(10, 2)
		s.Write([]byte("\x1bP\x07still ignored\x1b\\x"))
		if got := rowText(s, 0); got != "x" {
			t.Errorf("row = %q, want %q", got, "x")
		}
		if s.Bell() {
			t.Error("BEL inside DCS rang the bell")
		}
	})

	t.Run("st split across writes", func(t *testing.T) {
		s := New(10, 2)
		s.Write([]byte("\x1bPignored"))
		s.Write([]byte("\x1b"))
		s.Write([]byte("\\ok"))
		if got := rowText(s, 0); got != "ok" {
			t.Errorf("row = %q, want %q", got, "ok")
		}
	})
}

func TestCharsetAndKeypadDesignationsAreIgnored(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"charset g0", "\x1b(Bvisible"},
		{"charset g1", "\x1b)0visible"},
		{"charset with two intermediates", "\x1b$)Cvisible"},
		{"screen alignment", "\x1b#8visible"},
		{"character set selection", "\x1b%Gvisible"},
		{"keypad application", "\x1b=visible"},
		{"keypad numeric", "\x1b>visible"},
		{"graphics charset", "\x1bGvisible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(10, 2)
			s.Write([]byte(tt.input))
			if got := rowText(s, 0); got != "visible" {
				t.Errorf("row = %q, want %q", got, "visible")
			}
		})
	}
}

func TestEightBitControls(t *testing.T) {
	t.Run("c1 csi", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte{0x9b, '3', '1', 'm', 'X'})
		if got := s.Cell(0, 0).Style.FG; got != ANSI(1) {
			t.Errorf("foreground = %+v, want %+v", got, ANSI(1))
		}
		if got := rowText(s, 0); got != "X" {
			t.Errorf("row = %q, want %q", got, "X")
		}
	})

	t.Run("c1 osc", func(t *testing.T) {
		s := New(4, 1)
		s.Write([]byte{0x9d, '0', ';', 'c', '1', 0x07})
		if got := s.Title(); got != "c1" {
			t.Errorf("Title() = %q, want %q", got, "c1")
		}
	})
}

// itoa is a tiny helper that keeps subtest names readable without pulling
// strconv into every test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
