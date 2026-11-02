package vt

import "unicode"

// runeWidth is how many cells r takes: 0 for a combining mark or other
// zero-width code point, 2 for a wide one, and 1 for everything else.
//
// The ranges are the common ones: Hangul, kana, CJK ideographs and their
// extensions, fullwidth forms, and the pictographic blocks terminals render
// as emoji.
func runeWidth(r rune) int {
	// Printable ASCII, which is most of what a terminal draws, is one cell
	// and takes neither table below.
	if r >= 0x20 && r < 0x7f {
		return 1
	}
	if r == 0 || isZeroWidth(r) {
		return 0
	}
	switch {
	case r < 0x20, r >= 0x7f && r < 0xa0:
		return 0
	case r >= 0x1100 && r <= 0x115f, // Hangul Jamo
		r >= 0x2329 && r <= 0x232a,
		r >= 0x2600 && r <= 0x27bf,   // misc symbols and dingbats (emoji)
		r >= 0x2e80 && r <= 0x303e,   // CJK radicals, Kangxi, symbols
		r >= 0x3041 && r <= 0x33ff,   // kana, Bopomofo, CJK compatibility
		r >= 0x3400 && r <= 0x4dbf,   // CJK unified ideographs extension A
		r >= 0x4e00 && r <= 0x9fff,   // CJK unified ideographs
		r >= 0xa000 && r <= 0xa4cf,   // Yi
		r >= 0xa960 && r <= 0xa97f,   // Hangul Jamo extended A
		r >= 0xac00 && r <= 0xd7a3,   // Hangul syllables
		r >= 0xf900 && r <= 0xfaff,   // CJK compatibility ideographs
		r >= 0xfe10 && r <= 0xfe19,   // vertical forms
		r >= 0xfe30 && r <= 0xfe6f,   // CJK compatibility forms
		r >= 0xff01 && r <= 0xff60,   // fullwidth forms
		r >= 0xffe0 && r <= 0xffe6,   // fullwidth signs
		r >= 0x1b000 && r <= 0x1b2ff, // kana supplement and extended
		r >= 0x1f300 && r <= 0x1faff, // pictographs, emoticons, transport
		r >= 0x20000 && r <= 0x3fffd: // CJK extensions B and later
		return 2
	}
	return 1
}

// isZeroWidth reports whether r is a combining mark or another code point
// that a terminal draws in the cell of the character before it.
func isZeroWidth(r rune) bool {
	// No combining mark or other zero-width code point is below U+0300, so
	// the Unicode tables are only searched past the ASCII range.
	if r < 0x300 {
		return r == 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return true
	}
	switch r {
	case 0x200b, // zero width space
		0x200c, // zero width non-joiner
		0x200d, // zero width joiner
		0x200e, // left-to-right mark
		0x200f, // right-to-left mark
		0x2060, // word joiner
		0xfeff: // zero width no-break space / BOM
		return true
	}
	if r >= 0xfe00 && r <= 0xfe0f { // variation selectors
		return true
	}
	if r >= 0xe0100 && r <= 0xe01ef { // variation selectors supplement
		return true
	}
	if r >= 0xe0020 && r <= 0xe007f { // emoji tag characters
		return true
	}
	return false
}
