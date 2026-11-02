package vt

import "testing"

// take returns the replies a screen has for the program, as a slice.
func take(s *Screen) []string {
	r := s.TakeResponses()
	if r == nil {
		return []string{}
	}
	return r
}

// TestDeviceAttributes checks the answers to DA1 and DA2.
func TestDeviceAttributes(t *testing.T) {
	s := New(20, 5)
	s.Write([]byte("\x1b[c"))
	if got := take(s); len(got) != 1 || got[0] != "\x1b[?62;22c" {
		t.Fatalf("DA1 = %q", got)
	}
	s.Write([]byte("\x1b[>c"))
	if got := take(s); len(got) != 1 || got[0] != "\x1b[>0;95;0c" {
		t.Fatalf("DA2 = %q", got)
	}
}

// TestCursorPositionReport checks the CPR and DECXCPR replies against the
// cursor the program set, one-based.
func TestCursorPositionReport(t *testing.T) {
	for _, tt := range []struct {
		name string
		feed string
		ask  string
		want string
	}{
		{"home", "", "\x1b[6n", "\x1b[1;1R"},
		{"moved", "ab\r\ncd", "\x1b[6n", "\x1b[2;3R"},
		{"decxcpr", "ab\r\ncd", "\x1b[?6n", "\x1b[?2;3R"},
		{"status ok", "", "\x1b[5n", "\x1b[0n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := New(20, 5)
			s.Write([]byte(tt.feed))
			s.Write([]byte(tt.ask))
			got := take(s)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("reply = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestColorQueries checks that OSC 10 and 11 report the colors the window
// told the screen about, as xterm prints them.
func TestColorQueries(t *testing.T) {
	s := New(20, 5)
	s.SetColors([3]uint8{255, 255, 255}, [3]uint8{11, 11, 13})
	s.Write([]byte("\x1b]11;?\x07"))
	got := take(s)
	if len(got) != 1 || got[0] != "\x1b]11;rgb:0b0b/0b0b/0d0d\x1b\\" {
		t.Fatalf("OSC 11 = %q", got)
	}
	s.Write([]byte("\x1b]10;?\x1b\\"))
	got = take(s)
	if len(got) != 1 || got[0] != "\x1b]10;rgb:ffff/ffff/ffff\x1b\\" {
		t.Fatalf("OSC 10 = %q", got)
	}
}

// TestTakeResponsesForgets checks that replies are handed over once.
func TestTakeResponsesForgets(t *testing.T) {
	s := New(20, 5)
	s.Write([]byte("\x1b[c\x1b[c"))
	if got := take(s); len(got) != 2 {
		t.Fatalf("two queries gave %d replies", len(got))
	}
	if got := take(s); len(got) != 0 {
		t.Fatalf("replies came twice: %q", got)
	}
}
