package vt

// blankGrid makes rows of cols empty cells. A blank cell has Width 1 so that
// painting a row fills every column; only the second half of a wide
// character has Width 0.
func blankGrid(cols, rows int) [][]Cell {
	g := make([][]Cell, rows)
	for y := range g {
		g[y] = blankLine(cols)
	}
	return g
}

// blankLine makes one row of cols empty cells.
func blankLine(cols int) []Cell {
	line := make([]Cell, cols)
	for i := range line {
		line[i].Width = 1
	}
	return line
}

// resizeGrid keeps the top-left of a grid, padding the new cells with blanks
// and cropping the right and bottom. It does not reflow.
func resizeGrid(old [][]Cell, oldCols, oldRows, cols, rows int) [][]Cell {
	g := blankGrid(cols, rows)
	keepRows := min(oldRows, rows, len(old))
	keepCols := min(oldCols, cols)
	for y := 0; y < keepRows; y++ {
		copy(g[y][:keepCols], old[y][:min(keepCols, len(old[y]))])
		sanitizeWideLine(g[y])
	}
	return g
}

// takeRow returns a blank row of the screen's width: one a scroll dropped, or
// a new one while none wait.
func (s *Screen) takeRow() []Cell {
	if n := len(s.freeRows); n > 0 {
		row := s.freeRows[n-1]
		s.freeRows = s.freeRows[:n-1]
		return s.clearedRow(row)
	}
	return s.clearedRow(nil)
}

// clearedRow fills a row with the current rendition's blanks, making one when
// the screen dropped none.
func (s *Screen) clearedRow(row []Cell) []Cell {
	if len(row) != s.cols {
		row = make([]Cell, s.cols)
	}
	c := s.blank()
	for i := range row {
		row[i] = c
	}
	return row
}

// keepRow keeps a row a scroll dropped for the next one to reuse. Rows of
// another width, or more than the grid can hold, are left to the collector.
func (s *Screen) keepRow(row []Cell) {
	if len(row) != s.cols || len(s.freeRows) >= 2*s.rows {
		return
	}
	s.freeRows = append(s.freeRows, row)
}

// sanitizeWideLine repairs a line whose wide characters may have lost one
// half, for example at the right edge of a cropped grid.
func sanitizeWideLine(line []Cell) {
	for x := range line {
		switch line[x].Width {
		case 2:
			if x+1 >= len(line) || line[x+1].Width != 0 {
				line[x] = Cell{Width: 1}
			}
		case 0:
			if x == 0 || line[x-1].Width != 2 {
				line[x] = Cell{Width: 1}
			}
		}
	}
}

// blank is the cell an erase operation writes: no text, the current
// rendition, and one column wide.
func (s *Screen) blank() Cell {
	return Cell{Style: s.style, Width: 1}
}

// grid returns the rows the program is drawing on.
func (s *Screen) grid() [][]Cell {
	if s.alternate {
		return s.altGrid
	}
	return s.mainGrid
}

// gridLine returns a line of the active buffer. y must be in the grid.
func (s *Screen) gridLine(y int) []Cell { return s.grid()[y] }

// clearGridLine blanks one line of the active buffer with the current
// rendition.
func (s *Screen) clearGridLine(y int) {
	line := s.gridLine(y)
	c := s.blank()
	for i := range line {
		line[i] = c
	}
}

// clearGrid blanks the active buffer.
func (s *Screen) clearGrid() {
	c := s.blank()
	for _, line := range s.grid() {
		for i := range line {
			line[i] = c
		}
	}
}

// clearAlternate blanks the alternate buffer with the default rendition, as
// switching to it does.
func (s *Screen) clearAlternate() {
	for _, line := range s.altGrid {
		for i := range line {
			line[i] = Cell{Width: 1}
		}
	}
}

// setCell writes one cell and detaches any wide character it overlaps, so a
// wide character is never left with only one half on screen.
func (s *Screen) setCell(x, y int, c Cell) {
	if x < 0 || x >= s.cols || y < 0 || y >= s.rows {
		return
	}
	line := s.gridLine(y)
	// Replacing one single cell with another touches no wide character
	// beside it, which is every printable character of ordinary text.
	if old := line[x]; c.Width == 1 && old.Width == 1 {
		line[x] = c
		return
	}
	if line[x].Width == 0 && x > 0 {
		line[x-1] = s.blank()
	}
	if line[x].Width == 2 && x+1 < s.cols {
		line[x+1] = s.blank()
	}
	if c.Width == 2 && x+1 >= s.cols {
		// A wide character cannot fit in the last column.
		c.Width = 1
	}
	if c.Width == 2 && x+1 < s.cols {
		if line[x+1].Width == 2 && x+2 < s.cols {
			line[x+2] = s.blank()
		}
		line[x] = c
		line[x+1] = Cell{Style: c.Style, Width: 0}
		return
	}
	line[x] = c
}

// eraseCells blanks x0 through x1 on line y, including the other half of any
// wide character that crosses either end of the range.
func (s *Screen) eraseCells(y, x0, x1 int) {
	if y < 0 || y >= s.rows {
		return
	}
	if x0 < 0 {
		x0 = 0
	}
	if x1 >= s.cols {
		x1 = s.cols - 1
	}
	if x0 > x1 {
		return
	}
	line := s.gridLine(y)
	if x0 > 0 && line[x0].Width == 0 {
		line[x0-1] = s.blank()
	}
	if x1+1 < s.cols && line[x1].Width == 2 {
		line[x1+1] = s.blank()
	}
	c := s.blank()
	for x := x0; x <= x1; x++ {
		line[x] = c
	}
}
