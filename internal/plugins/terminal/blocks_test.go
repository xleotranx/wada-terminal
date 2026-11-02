package terminal

import "testing"

// TestBlockArt checks the geometry of the block characters a terminal draws
// its art with.
func TestBlockArt(t *testing.T) {
	full := [4]float32{0, 0, 1, 1}
	tests := []struct {
		r    rune
		want [][4]float32
	}{
		{'█', [][4]float32{full}},
		{'▀', [][4]float32{{0, 0, 1, .5}}},
		{'▄', [][4]float32{{0, .5, 1, .5}}},
		{'▌', [][4]float32{{0, 0, .5, 1}}},
		{'▐', [][4]float32{{.5, 0, .5, 1}}},
		{'▖', [][4]float32{{0, .5, .5, .5}}},
		{'▘', [][4]float32{{0, 0, .5, .5}}},
		{'▙', [][4]float32{{0, 0, .5, 1}, {.5, .5, .5, .5}}},
		{'▞', [][4]float32{{.5, 0, .5, .5}, {0, .5, .5, .5}}},
		{'▟', [][4]float32{{0, .5, 1, .5}, {.5, 0, .5, .5}}},
		{'▁', [][4]float32{{0, .875, 1, .125}}},
		{'▇', [][4]float32{{0, .125, 1, .875}}},
		{'░', [][4]float32{full}},
	}
	for _, tt := range tests {
		got := blockArt(tt.r)
		if len(got) != len(tt.want) {
			t.Fatalf("%c: %d rectangles, want %d", tt.r, len(got), len(tt.want))
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("%c: rectangle %d = %v, want %v", tt.r, i, got[i], tt.want[i])
			}
		}
	}
	for _, r := range []rune{'a', '│', '─', '✳', '日'} {
		if got := blockArt(r); got != nil {
			t.Errorf("%c should be left to the font, got %v", r, got)
		}
	}
}

// TestShadeAlpha checks how much of its color a shade paints.
func TestShadeAlpha(t *testing.T) {
	for r, want := range map[rune]float32{'░': .25, '▒': .5, '▓': .75, '█': 1} {
		if got := shadeAlpha(r); got != want {
			t.Errorf("shadeAlpha(%c) = %v, want %v", r, got, want)
		}
	}
}

// TestSingleRune checks that only a cell of exactly one rune is block art.
func TestSingleRune(t *testing.T) {
	if r, ok := singleRune("█"); !ok || r != '█' {
		t.Fatalf("singleRune(█) = %c, %v", r, ok)
	}
	if _, ok := singleRune("e\u0301"); ok {
		t.Fatal("a grapheme cluster is not a single rune")
	}
}
