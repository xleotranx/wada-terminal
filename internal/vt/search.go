package vt

import "strings"

// Match is one occurrence of a search query in the screen's history and grid.
// Line counts from the oldest history line; Start and End are cell columns,
// End exclusive.
type Match struct {
	Line       int
	Start, End int
}

// TotalLines is the history followed by the grid.
func (s *Screen) TotalLines() int { return len(s.history) + s.rows }

// Search finds every occurrence of query, ignoring case. It reads the
// scrollback and the live grid, not the view offset.
func (s *Screen) Search(query string) []Match {
	query = strings.ToLower(query)
	if query == "" || s.alternate {
		return nil
	}
	var matches []Match
	for line := 0; line < s.TotalLines(); line++ {
		cells := s.lineAt(line)
		if len(cells) == 0 {
			continue
		}
		var text []rune
		cols := make([]int, 0, len(cells))
		for x, cell := range cells {
			if cell.Width == 0 {
				continue
			}
			grapheme := cell.Text
			if grapheme == "" {
				grapheme = " "
			}
			for _, r := range grapheme {
				text = append(text, r)
				cols = append(cols, x)
			}
		}
		lower := []rune(strings.ToLower(string(text)))
		needle := []rune(query)
		for i := 0; i+len(needle) <= len(lower); i++ {
			if string(lower[i:i+len(needle)]) != query {
				continue
			}
			start := cols[i]
			end := cols[i+len(needle)-1] + 1
			matches = append(matches, Match{Line: line, Start: start, End: end})
			i += len(needle) - 1
		}
		if len(matches) > 10000 {
			break
		}
	}
	return matches
}

// lineAt reads history first, then the live grid.
func (s *Screen) lineAt(line int) []Cell {
	if line < 0 || line >= s.TotalLines() {
		return nil
	}
	if line < len(s.history) {
		return s.historyLine(line)
	}
	return s.BottomRow(line - len(s.history))
}

// ScrollToLine moves the view so line is near the middle when it can be.
func (s *Screen) ScrollToLine(line int) {
	if line < 0 {
		line = 0
	}
	if line >= s.TotalLines() {
		line = s.TotalLines() - 1
	}
	offset := len(s.history) - line + s.rows/2
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.history) {
		offset = len(s.history)
	}
	s.viewOffset = offset
}

// SearchHighlight reports whether a cell at x on the line shown at row y is
// inside one of the matches.
func (s *Screen) SearchHighlight(matches []Match, y, x int) bool {
	if len(matches) == 0 {
		return false
	}
	line := len(s.history) - s.viewOffset + y
	for _, match := range matches {
		if match.Line == line && x >= match.Start && x < match.End {
			return true
		}
	}
	return false
}
