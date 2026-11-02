package vt

import "strings"

// runeTexts are the one-character strings of the code points a terminal
// draws: Latin-1, and the box drawing, block and geometric blocks a TUI
// paints its frames with. A cell takes its string from here, so a screenful
// of text costs no allocation per character.
var runeTexts = func() [512]string {
	var t [512]string
	for r := range t {
		t[r] = string(rune(r))
	}
	for r := 0; r < 256; r++ {
		t[256+r] = string(rune(0x2500 + r))
	}
	return t
}()

// runeText is the string of one code point, from the interned table for the
// blocks it covers and a fresh one otherwise.
func runeText(r rune) string {
	if r >= 0 && r < 256 {
		return runeTexts[r]
	}
	if r >= 0x2500 && r < 0x2600 {
		return runeTexts[256+r-0x2500]
	}
	return string(r)
}

// LineText is one line as text, spaces where cells are empty and trailing
// blanks trimmed. Line counts from the oldest history line.
func (s *Screen) LineText(line int) string {
	cells := s.lineAt(line)
	if len(cells) == 0 {
		return ""
	}
	var b strings.Builder
	for _, cell := range cells {
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

// LineBlock is lines start through end, inclusive, joined by newlines.
func (s *Screen) LineBlock(start, end int) string {
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= s.TotalLines() {
		end = s.TotalLines() - 1
	}
	var lines []string
	for line := start; line <= end; line++ {
		lines = append(lines, s.LineText(line))
	}
	return strings.Join(lines, "\n")
}

// VisibleTop is the first line of the viewport, counted from the oldest
// history line.
func (s *Screen) VisibleTop() int { return len(s.history) - s.viewOffset }

// LastContentLine is the last line with any text, or 0 on a blank screen.
func (s *Screen) LastContentLine() int {
	for line := s.TotalLines() - 1; line >= 0; line-- {
		if s.LineText(line) != "" {
			return line
		}
	}
	return 0
}

// TextBlock is the cells from (fromLine, fromCol) through (toLine, toCol),
// end exclusive, joined by newlines. Columns are cell columns; the two ends
// may come in either order.
func (s *Screen) TextBlock(fromLine, fromCol, toLine, toCol int) string {
	if fromLine > toLine || (fromLine == toLine && fromCol > toCol) {
		fromLine, toLine = toLine, fromLine
		fromCol, toCol = toCol, fromCol
	}
	if fromLine < 0 {
		fromLine, fromCol = 0, 0
	}
	if toLine >= s.TotalLines() {
		toLine = s.TotalLines() - 1
	}
	var lines []string
	for line := fromLine; line <= toLine; line++ {
		cells := s.lineAt(line)
		start, end := 0, len(cells)
		if line == fromLine {
			start = fromCol
		}
		if line == toLine {
			end = toCol
		}
		var b strings.Builder
		for x, cell := range cells {
			if cell.Width == 0 || x < start || x >= end {
				continue
			}
			if cell.Text == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(cell.Text)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}
