package terminal

import "testing"

// TestBoxArms checks the arms of a few box-drawing characters against their
// Unicode names.
func TestBoxArms(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want arms
	}{
		{"light horizontal", '─', arms{Left: armLight, Right: armLight}},
		{"light vertical", '│', arms{Up: armLight, Down: armLight}},
		{"light down and right", '┌', arms{Down: armLight, Right: armLight}},
		{"light up and left", '┘', arms{Up: armLight, Left: armLight}},
		{"light vertical and right", '├', arms{Up: armLight, Down: armLight, Right: armLight}},
		{"light vertical and horizontal", '┼', arms{Up: armLight, Down: armLight, Left: armLight, Right: armLight}},
		{"double down and right", '╔', arms{Down: armDouble, Right: armDouble}},
		{"double vertical and horizontal", '╬', arms{Up: armDouble, Down: armDouble, Left: armDouble, Right: armDouble}},
		{"down single and right double", '╒', arms{Down: armLight, Right: armDouble}},
		{"vertical single and horizontal double", '╪', arms{Up: armLight, Down: armLight, Left: armDouble, Right: armDouble}},
		{"light arc down and right", '╭', arms{Down: armLight, Right: armLight}},
		{"light left", '╴', arms{Left: armLight}},
		{"heavy vertical and horizontal", '╋', arms{Up: armHeavy, Down: armHeavy, Left: armHeavy, Right: armHeavy}},
	}
	for _, tt := range tests {
		got, ok := boxArms(tt.r)
		if !ok {
			t.Errorf("%s (%U): not drawn by the window", tt.name, tt.r)
			continue
		}
		if got != tt.want {
			t.Errorf("%s (%U) = %+v, want %+v", tt.name, tt.r, got, tt.want)
		}
	}
}

// TestBoxArmsMixedCrosses checks U+2543 through U+2546. Their names pair two
// heavy arms with two light ones, and the Unicode code chart draws all four
// arms: they are crosses, not corners. The heavy pair is the first two
// directions in the name and the light pair the last two.
func TestBoxArmsMixedCrosses(t *testing.T) {
	tests := []struct {
		r    rune
		want arms
	}{
		{'╃', arms{Up: armHeavy, Down: armLight, Left: armHeavy, Right: armLight}}, // LEFT UP HEAVY AND RIGHT DOWN LIGHT
		{'╄', arms{Up: armHeavy, Down: armLight, Left: armLight, Right: armHeavy}}, // RIGHT UP HEAVY AND LEFT DOWN LIGHT
		{'╅', arms{Up: armLight, Down: armHeavy, Left: armHeavy, Right: armLight}}, // LEFT DOWN HEAVY AND RIGHT UP LIGHT
		{'╆', arms{Up: armLight, Down: armHeavy, Left: armLight, Right: armHeavy}}, // RIGHT DOWN HEAVY AND LEFT UP LIGHT
	}
	for _, tt := range tests {
		got, ok := boxArms(tt.r)
		if !ok {
			t.Errorf("boxArms(%U): not drawn by the window", tt.r)
			continue
		}
		if got != tt.want {
			t.Errorf("boxArms(%U) = %+v, want %+v", tt.r, got, tt.want)
		}
	}
}

// TestBoxArmsBlock sweeps the whole block: every character but the three
// diagonals is drawn, and nothing outside the block is.
func TestBoxArmsBlock(t *testing.T) {
	for r := '\u2500'; r <= '\u257F'; r++ {
		_, ok := boxArms(r)
		if !ok && r != '╱' && r != '╲' && r != '╳' {
			t.Errorf("boxArms(%U) = false, want drawn", r)
		}
		if ok && (r == '╱' || r == '╲' || r == '╳') {
			t.Errorf("boxArms(%U) = true, want left to the font", r)
		}
	}
	for _, r := range []rune{'a', '█', '日', ' ', '\u24FF', '\u2580'} {
		if _, ok := boxArms(r); ok {
			t.Errorf("boxArms(%U) = true, want false outside the block", r)
		}
	}
}

// TestBoxArmsNonEmpty checks that every drawn character joins at least one
// edge, and that no arm has a weight outside the four the table defines.
func TestBoxArmsNonEmpty(t *testing.T) {
	for r := '\u2500'; r <= '\u257F'; r++ {
		a, ok := boxArms(r)
		if !ok {
			continue
		}
		if a == (arms{}) {
			t.Errorf("boxArms(%U) has no arms", r)
		}
		for _, weight := range [...]uint8{a.Up, a.Down, a.Left, a.Right} {
			if weight > armDouble {
				t.Errorf("boxArms(%U) has weight %d, want at most armDouble", r, weight)
			}
		}
	}
}
