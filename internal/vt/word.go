package vt

import "unicode"

// WordStart is the first cell of the next word after (line, col), crossing
// into later lines. It reports false at the end of the screen.
func (s *Screen) WordStart(line, col int) (int, int, bool) {
	text := []rune(s.LineText(line))
	// Finish the current word, then skip the space after it.
	for col++; col < len(text) && !unicode.IsSpace(text[col]); col++ {
	}
	for {
		for col < len(text) && unicode.IsSpace(text[col]) {
			col++
		}
		if col < len(text) {
			return line, col, true
		}
		line, col = line+1, 0
		if line >= s.TotalLines() {
			return 0, 0, false
		}
		text = []rune(s.LineText(line))
	}
}

// WordBack is the first cell of the word before (line, col).
func (s *Screen) WordBack(line, col int) (int, int, bool) {
	text := []rune(s.LineText(line))
	for {
		for col > 0 && unicode.IsSpace(at(text, col-1)) {
			col--
		}
		if col > 0 {
			for col > 0 && !unicode.IsSpace(at(text, col-1)) {
				col--
			}
			return line, col, true
		}
		line--
		if line < 0 {
			return 0, 0, false
		}
		text = []rune(s.LineText(line))
		col = len(text)
	}
}

// WordEnd is the last cell of the word at or after (line, col).
func (s *Screen) WordEnd(line, col int) (int, int, bool) {
	text := []rune(s.LineText(line))
	for {
		for col < len(text) && unicode.IsSpace(text[col]) {
			col++
		}
		for col < len(text) && !unicode.IsSpace(text[col]) {
			col++
		}
		if col > 0 && col <= len(text) {
			return line, col - 1, true
		}
		line, col = line+1, 0
		if line >= s.TotalLines() {
			return 0, 0, false
		}
		text = []rune(s.LineText(line))
	}
}

// LineEnd is the last non-space cell of the line.
func (s *Screen) LineEnd(line int) int {
	text := []rune(s.LineText(line))
	if len(text) == 0 {
		return 0
	}
	return len(text) - 1
}

func at(text []rune, i int) rune {
	if i < 0 || i >= len(text) {
		return ' '
	}
	return text[i]
}
