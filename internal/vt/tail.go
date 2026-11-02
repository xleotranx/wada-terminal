package vt

import "strings"

// Tail returns the last lines of the screen's live bottom, top first: the
// scrollback and the grid, not the part a viewer has scrolled to. It is what a
// client reads when it wants to know what a program last printed, and what the
// window reads to decide whether an agent waits on an approval.
func (s *Screen) Tail(lines int) []string {
	if lines <= 0 {
		return nil
	}
	total := len(s.history) + s.rows
	if total <= 0 {
		return nil
	}
	all := make([]string, 0, total)
	for index := 0; index < total; index++ {
		var row []Cell
		if index < len(s.history) {
			row = s.history[index]
		} else {
			row = s.BottomRow(index - len(s.history))
		}
		var b strings.Builder
		for _, cell := range row {
			if cell.Width == 0 {
				continue // the second half of a wide character
			}
			if cell.Text == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(cell.Text)
			}
		}
		all = append(all, strings.TrimRight(b.String(), " "))
	}
	// A screen's cursor line and the rest of its grid are usually blank;
	// they do not end what the program printed.
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return all
}
