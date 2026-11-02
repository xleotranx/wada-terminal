package terminal

import (
	"testing"

	"wada-terminal/internal/vt"

	"github.com/egoist/mygo/ui"
)

// TestMouseReport checks the three forms a terminal reports a pointer in.
func TestMouseReport(t *testing.T) {
	sgr := vt.MouseState{Mode: 1000, SGR: true}
	urxvt := vt.MouseState{Mode: 1000, URXVT: true}
	x10 := vt.MouseState{Mode: 1000}
	for _, tt := range []struct {
		name    string
		m       vt.MouseState
		button  int
		x, y    int
		mods    int
		motion  bool
		release bool
		want    string
	}{
		{"sgr press", sgr, mouseLeft, 0, 0, 0, false, false, "\x1b[<0;1;1M"},
		{"sgr release", sgr, mouseLeft, 4, 2, 0, false, true, "\x1b[<0;5;3m"},
		{"sgr motion with button", sgr, mouseLeft, 5, 2, 0, true, false, "\x1b[<32;6;3M"},
		{"sgr motion alone", sgr, 3, 5, 2, 0, true, false, "\x1b[<35;6;3M"},
		{"sgr ctrl wheel", sgr, mouseWheelUp, 3, 4, 16, false, false, "\x1b[<80;4;5M"},
		{"urxvt press middle", urxvt, mouseMiddle, 1, 1, 0, false, false, "\x1b[33;2;2M"},
		{"urxvt release", urxvt, mouseLeft, 1, 1, 0, false, true, "\x1b[35;2;2M"},
		{"x10 press right", x10, mouseRight, 2, 1, 0, false, false, "\x1b[M\"#\""},
		{"x10 release", x10, mouseLeft, 2, 1, 0, false, true, "\x1b[M##\""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := string(mouseReport(tt.m, tt.button, tt.x, tt.y, tt.mods, tt.motion, tt.release))
			if got != tt.want {
				t.Fatalf("report = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMouseReportWideColumns checks that the classic form carries a column
// past 95 as UTF-8 rather than a raw byte.
func TestMouseReportWideColumns(t *testing.T) {
	got := mouseReport(vt.MouseState{Mode: 1000}, mouseLeft, 200, 0, 0, false, false)
	// The button is 32, column 201 is 233 (é in UTF-8), row 1 is 33.
	want := "\x1b[M " + string([]byte{0xC3, 0xA9}) + "!"
	if string(got) != want {
		t.Fatalf("report = %q, want %q", got, want)
	}
}

// TestXtermButton checks the mapping from the window's buttons to a
// terminal's.
func TestXtermButton(t *testing.T) {
	for in, want := range map[int]int{0: mouseLeft, 1: mouseRight, 2: mouseMiddle} {
		if got := xtermButton(in); got != want {
			t.Errorf("xtermButton(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestMouseMods checks that Shift does not reach a report.
func TestMouseMods(t *testing.T) {
	if got := mouseMods(ui.Shift | ui.Alt | ui.Ctrl); got != 8|16 {
		t.Fatalf("mods = %d, want %d", got, 8|16)
	}
}

func TestEndedSessionDoesNotReportMouse(t *testing.T) {
	s := &session{screen: vt.New(8, 4)}
	s.screen.Write([]byte("\x1b[?1003h\x1b[?1006h"))
	if _, on := s.mouseReporting(ui.InputEvent{}); on {
		t.Fatal("ended session still sends hover reports")
	}
	s.alive = true
	if _, on := s.mouseReporting(ui.InputEvent{}); !on {
		t.Fatal("live program lost requested mouse reporting")
	}
}
