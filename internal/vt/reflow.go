package vt

// wrapFlags are the wrap marks of the grid on show: true on a line that
// continues on the next one, because the program filled its width.
func (s *Screen) wrapFlags() []bool {
	if s.alternate {
		return s.altWrap
	}
	return s.mainWrap
}

// endLine records that the line of the cursor ends here, at a newline rather
// than at the margin, so a resize does not join it with the next one.
func (s *Screen) endLine() {
	if f := s.wrapFlags(); s.curY >= 0 && s.curY < len(f) {
		f[s.curY] = false
	}
}

// resizeFlags keeps the marks at the top, padding and cropping.
func resizeFlags(old []bool, oldRows, rows int) []bool {
	out := make([]bool, rows)
	copy(out, old[:min(len(old), rows)])
	return out
}

// trimEnd drops the blank cells that end a line: a line wrapped at the margin
// has none, and a line ended by a newline must not carry blanks into the lines
// a narrower screen wraps it into. A cell that paints a background is content,
// and stays.
func trimEnd(cells []Cell) []Cell {
	for len(cells) > 0 {
		c := cells[len(cells)-1]
		if c.Width != 1 || c.Text != "" || c.Style.BG.Kind != ColorDefault {
			break
		}
		cells = cells[:len(cells)-1]
	}
	return cells
}

// blankRow reports whether a row holds nothing, not even a background.
func blankRow(cells []Cell) bool {
	for _, c := range cells {
		if c.Width != 1 || c.Text != "" || c.Style.BG.Kind != ColorDefault {
			return false
		}
	}
	return true
}

// reflow gives the main screen a new size: each line the program wrote is
// wrapped again at the new width, and lines a shorter screen has no room for
// scroll into the history, so the cursor stays on show. The cursor keeps its
// place in its line, not its place on the screen.
func (s *Screen) reflow(cols, rows int) {
	oldCols, oldRows := s.cols, s.rows
	cx, cy := s.curX, s.curY

	// The program's lines: rows joined while the line before wrapped.
	type logical struct {
		cells []Cell
		start int // the first grid row it takes
		end   int // the last
	}
	lines := make([]logical, 0, oldRows)
	for y := 0; y < oldRows; {
		ln := logical{start: y}
		for {
			ln.cells = append(ln.cells, s.mainGrid[y*oldCols:(y+1)*oldCols]...)
			if y == oldRows-1 || !s.mainWrap[y] {
				break
			}
			y++
		}
		ln.end = y
		ln.cells = trimEnd(ln.cells)
		lines = append(lines, ln)
		y++
	}

	// The cursor's line, and how far into it the cursor sits.
	cursorLine, cursorOffset := 0, 0
	for i, ln := range lines {
		if cy >= ln.start && cy <= ln.end {
			cursorLine = i
			cursorOffset = (cy-ln.start)*oldCols + cx
			break
		}
	}

	// Wrap them again.
	var grid []Cell
	var wrap []bool
	starts := make([]int, len(lines))
	for i, ln := range lines {
		starts[i] = len(wrap)
		if len(ln.cells) == 0 {
			grid = append(grid, blankGrid(cols, 1)...)
			wrap = append(wrap, false)
			continue
		}
		for off := 0; off < len(ln.cells); off += cols {
			end := min(off+cols, len(ln.cells))
			line := blankGrid(cols, 1)
			copy(line, ln.cells[off:end])
			sanitizeWideLine(line)
			grid = append(grid, line...)
			wrap = append(wrap, end < len(ln.cells))
		}
	}

	// Where the cursor lands: at the end of a line that fills its width it
	// stands at the start of the next row, as it did.
	ln := lines[cursorLine]
	row, col := 0, 0
	start := starts[cursorLine]
	if len(ln.cells) > 0 && cursorOffset >= len(ln.cells) && len(ln.cells)%cols == 0 {
		row, col = start+len(ln.cells)/cols, 0
	} else {
		row, col = start+cursorOffset/cols, cursorOffset%cols
	}
	for len(wrap) <= row {
		grid = append(grid, blankGrid(cols, 1)...)
		wrap = append(wrap, false)
	}
	// Blank lines below the cursor are padding, not content: they must not
	// push a line the cursor is not on out of the screen.
	for len(wrap) > row+1 && blankRow(grid[(len(wrap)-1)*cols:]) {
		grid = grid[:(len(wrap)-1)*cols]
		wrap = wrap[:len(wrap)-1]
	}

	// Lines above what the screen can show go to the history.
	if over := len(wrap) - rows; over > 0 {
		for i := 0; i < over; i++ {
			s.pushHistory(grid[i*cols : (i+1)*cols])
		}
		grid = grid[over*cols:]
		wrap = wrap[over:]
		row -= over
	}
	for len(wrap) < rows {
		grid = append(grid, blankGrid(cols, 1)...)
		wrap = append(wrap, false)
	}

	s.cols, s.rows = cols, rows
	s.mainGrid = grid
	s.mainWrap = wrap
	s.altGrid = resizeGrid(s.altGrid, oldCols, oldRows, cols, rows)
	s.altWrap = resizeFlags(s.altWrap, oldRows, rows)
	s.curX = clampInt(col, 0, cols-1)
	s.curY = clampInt(row, 0, rows-1)
	s.wrapNext = false
	s.lastValid = false
	s.scrollTop, s.scrollBottom = 0, rows-1
	s.viewOffset = 0
	s.resizeTabs(oldCols)
}
