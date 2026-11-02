package terminal

// blockArt is the geometry of the characters terminals draw their block art
// with: the rectangles of a cell, as fractions of its width and height, that
// a character paints. A glyph of a code font leaves seams between cells, so a
// pane paints these itself, as terminal emulators do.
//
// A nil answer is a character with no block geometry: the font draws it.
func blockArt(r rune) [][4]float32 {
	eighth := float32(1) / 8
	switch {
	case r == '▀': // upper half
		return [][4]float32{{0, 0, 1, .5}}
	case r >= '▁' && r <= '▇': // lower eighths, one to seven
		n := float32(r-'▁'+1) * eighth
		return [][4]float32{{0, 1 - n, 1, n}}
	case r == '█':
		return [][4]float32{{0, 0, 1, 1}}
	case r >= '▉' && r <= '▏': // left eighths, seven to one
		n := float32('▉'+7-r) * eighth
		return [][4]float32{{0, 0, n, 1}}
	case r == '▐': // right half
		return [][4]float32{{.5, 0, .5, 1}}
	case r == '░' || r == '▒' || r == '▓': // shades, painted as a veil
		return [][4]float32{{0, 0, 1, 1}}
	case r == '▔': // upper eighth
		return [][4]float32{{0, 0, 1, eighth}}
	case r == '▕': // right eighth
		return [][4]float32{{1 - eighth, 0, eighth, 1}}
	case r == '▖':
		return [][4]float32{{0, .5, .5, .5}}
	case r == '▗':
		return [][4]float32{{.5, .5, .5, .5}}
	case r == '▘':
		return [][4]float32{{0, 0, .5, .5}}
	case r == '▙': // upper left, both lower
		return [][4]float32{{0, 0, .5, 1}, {.5, .5, .5, .5}}
	case r == '▚': // upper left, lower right
		return [][4]float32{{0, 0, .5, .5}, {.5, .5, .5, .5}}
	case r == '▛': // upper both, lower left
		return [][4]float32{{0, 0, 1, .5}, {0, .5, .5, .5}}
	case r == '▜': // upper both, lower right
		return [][4]float32{{0, 0, 1, .5}, {.5, .5, .5, .5}}
	case r == '▝':
		return [][4]float32{{.5, 0, .5, .5}}
	case r == '▞': // upper right, lower left
		return [][4]float32{{.5, 0, .5, .5}, {0, .5, .5, .5}}
	case r == '▟': // lower both, upper right
		return [][4]float32{{0, .5, 1, .5}, {.5, 0, .5, .5}}
	}
	return nil
}

// shadeAlpha is how much of its color a shade character paints: light, medium
// and dark.
func shadeAlpha(r rune) float32 {
	switch r {
	case '░':
		return .25
	case '▒':
		return .5
	case '▓':
		return .75
	}
	return 1
}

// iconMark is the small mark a private-use character stands for when no icon
// font draws it: an icon's codepoint, drawn as a dot rather than the empty
// box the system font shows.
func iconMark() [][4]float32 {
	return [][4]float32{{.36, .28, .28, .44}}
}

// privateUse reports whether a rune is an icon of a private-use area, which a
// Nerd Font and similar fonts carry.
func privateUse(r rune) bool {
	return (r >= 0xE000 && r <= 0xF8FF) || (r >= 0xF0000 && r <= 0x10FFFD)
}
