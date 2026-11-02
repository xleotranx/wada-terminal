package vt

// historyLimit is how many lines the main screen keeps above the grid.
const historyLimit = 4000

// cursorState is a saved cursor and rendition, for ESC 7/8, CSI s/u and the
// cursor modes of DECSET.
type cursorState struct {
	x, y  int
	style Style
	valid bool
}

func clampSize(cols, rows int) (int, int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

func newScreen(cols, rows int) *Screen {
	cols, rows = clampSize(cols, rows)
	s := &Screen{
		cols:          cols,
		rows:          rows,
		mainGrid:      blankGrid(cols, rows),
		altGrid:       blankGrid(cols, rows),
		mainWrap:      make([]bool, rows),
		altWrap:       make([]bool, rows),
		cursorVisible: true,
		autoWrap:      true,
		scrollBottom:  rows - 1,
	}
	s.saved.valid = true
	s.resetTabs()
	return s
}

func (s *Screen) write(p []byte) {
	for _, b := range p {
		s.feed(b)
	}
}

// resize gives the screen a new size. The main screen re-wraps its lines, so
// text keeps the width it was written at; the alternate screen, which a
// full-screen program redraws, keeps its top-left and crops or pads, as a
// terminal without reflow does.
func (s *Screen) resize(cols, rows int) {
	cols, rows = clampSize(cols, rows)
	if cols == s.cols && rows == s.rows {
		return
	}
	if !s.alternate {
		s.reflow(cols, rows)
		return
	}
	s.resizeCrop(cols, rows)
}

// resizeCrop keeps the top-left of both buffers, padding and cropping.
func (s *Screen) resizeCrop(cols, rows int) {
	oldCols, oldRows := s.cols, s.rows
	wasFullMargin := s.scrollTop == 0 && s.scrollBottom == oldRows-1
	s.mainGrid = resizeGrid(s.mainGrid, oldCols, oldRows, cols, rows)
	s.altGrid = resizeGrid(s.altGrid, oldCols, oldRows, cols, rows)
	s.mainWrap = resizeFlags(s.mainWrap, oldRows, rows)
	s.altWrap = resizeFlags(s.altWrap, oldRows, rows)
	s.cols, s.rows = cols, rows
	s.resizeTabs(oldCols)

	s.curX = clampInt(s.curX, 0, s.cols-1)
	s.curY = clampInt(s.curY, 0, s.rows-1)
	s.wrapNext = false
	s.lastValid = false

	if s.rows == 1 {
		s.scrollTop, s.scrollBottom = 0, 0
	} else if wasFullMargin {
		s.scrollTop, s.scrollBottom = 0, s.rows-1
	} else {
		s.scrollTop = clampInt(s.scrollTop, 0, s.rows-1)
		s.scrollBottom = clampInt(s.scrollBottom, 0, s.rows-1)
		if s.scrollTop >= s.scrollBottom {
			s.scrollTop, s.scrollBottom = 0, s.rows-1
		}
	}
	if s.viewOffset > len(s.history) {
		s.viewOffset = len(s.history)
	}
}

func (s *Screen) resetTabs() {
	s.tabs = make([]bool, s.cols)
	for x := 8; x < s.cols; x += 8 {
		s.tabs[x] = true
	}
}

func (s *Screen) resizeTabs(oldCols int) {
	tabs := make([]bool, s.cols)
	for x := range tabs {
		if x < oldCols && x < len(s.tabs) {
			tabs[x] = s.tabs[x]
		} else {
			tabs[x] = x > 0 && x%8 == 0
		}
	}
	s.tabs = tabs
}

func (s *Screen) cellAt(x, y int) Cell {
	row := s.rowAt(y)
	if x < 0 || x >= len(row) {
		return Cell{}
	}
	return row[x]
}

// rowAt picks the line the view shows at y. At the bottom (viewOffset 0) it
// is the active grid; above the bottom it comes from the history first and
// the grid afterwards.
func (s *Screen) rowAt(y int) []Cell { return s.rowAtFrom(y, s.viewOffset) }

// rowAtFrom picks the line y of a view offset lines above the bottom; offset
// 0 is what the program last drew, whatever the view shows.
func (s *Screen) rowAtFrom(y, offset int) []Cell {
	if y < 0 || y >= s.rows {
		return nil
	}
	start := len(s.history) - offset
	index := start + y
	if index < len(s.history) {
		if index < 0 {
			return nil
		}
		return s.history[index]
	}
	gridY := index - len(s.history)
	if gridY < 0 || gridY >= s.rows {
		return nil
	}
	return s.gridLine(gridY)
}

func (s *Screen) cursor() Cursor {
	return Cursor{
		X:       s.curX,
		Y:       s.curY,
		Visible: s.cursorVisible && s.viewOffset == 0,
		Style:   s.style,
	}
}

func (s *Screen) scroll(delta int) {
	offset := s.viewOffset + delta
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.history) {
		offset = len(s.history)
	}
	s.viewOffset = offset
}

func (s *Screen) homeCursor() {
	s.curX = 0
	if s.originMode {
		s.curY = s.scrollTop
	} else {
		s.curY = 0
	}
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) saveCursor() {
	s.saved = cursorState{x: s.curX, y: s.curY, style: s.style, valid: true}
}

func (s *Screen) restoreCursor() {
	if s.saved.valid {
		s.restoreCursorState(s.saved)
	}
}

func (s *Screen) restoreCursorState(st cursorState) {
	s.curX = clampInt(st.x, 0, s.cols-1)
	s.curY = clampInt(st.y, 0, s.rows-1)
	s.style = st.style
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) backspace() {
	if s.curX > 0 {
		s.curX--
	}
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) carriageReturn() {
	s.curX = 0
	s.wrapNext = false
	s.lastValid = false
}

// lineFeed moves down, scrolling at the bottom margin. A linefeed while a
// wrap is pending first returns to column 0, so the wrap and the newline do
// not move down twice (the BAK/xterm behaviour).
func (s *Screen) lineFeed() {
	if s.wrapNext {
		s.wrapNext = false
		s.curX = 0
	}
	s.endLine()
	s.index()
	s.lastValid = false
}

// nextLine is ESC E: carriage return and line feed.
func (s *Screen) nextLine() {
	s.wrapNext = false
	s.curX = 0
	s.endLine()
	s.index()
	s.lastValid = false
}

// index is ESC D and the LF half of lineFeed: move down one line, scrolling
// the margin if the cursor is at its bottom.
func (s *Screen) index() {
	if s.curY == s.scrollBottom {
		s.scrollUp(s.scrollTop, s.scrollBottom, 1, true)
	} else if s.curY < s.rows-1 {
		s.curY++
	}
	s.wrapNext = false
}

// reverseIndex is ESC M: move up one line, scrolling the margin down if the
// cursor is at its top.
func (s *Screen) reverseIndex() {
	if s.curY == s.scrollTop {
		s.scrollDown(s.scrollTop, s.scrollBottom, 1)
	} else if s.curY > 0 {
		s.curY--
	}
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) tabForward() {
	x := s.curX + 1
	for x < s.cols && !s.tabs[x] {
		x++
	}
	if x >= s.cols {
		x = s.cols - 1
	}
	s.curX = x
	s.wrapNext = false
	s.lastValid = false
}

// setScrollRegion is CSI r. With no parameters it clears the margin to the
// whole grid; an inverted or empty margin is ignored, as xterm ignores it.
func (s *Screen) setScrollRegion(params []int) {
	top, bottom := 1, s.rows
	if len(params) > 0 {
		top = params[0]
	}
	if len(params) > 1 {
		bottom = params[1]
	}
	if top == 0 {
		top = 1
	}
	if bottom == 0 {
		bottom = s.rows
	}
	if top < 1 {
		top = 1
	}
	if bottom > s.rows {
		bottom = s.rows
	}
	if top >= bottom {
		if s.rows != 1 || top != 1 || bottom != 1 {
			return
		}
	}
	s.scrollTop = top - 1
	s.scrollBottom = bottom - 1
	s.homeCursor()
}

func (s *Screen) scrollUp(top, bottom, n int, pushHistory bool) {
	height := bottom - top + 1
	if height <= 0 || n <= 0 {
		return
	}
	if n > height {
		n = height
	}
	if pushHistory && top == 0 && bottom == s.rows-1 && !s.alternate {
		for i := 0; i < n; i++ {
			s.pushHistory(s.gridLine(top + i))
		}
	}
	flags := s.wrapFlags()
	for y := top; y <= bottom-n; y++ {
		copy(s.gridLine(y), s.gridLine(y+n))
		flags[y] = flags[y+n]
	}
	for y := bottom - n + 1; y <= bottom; y++ {
		s.clearGridLine(y)
		flags[y] = false
	}
}

func (s *Screen) scrollDown(top, bottom, n int) {
	height := bottom - top + 1
	if height <= 0 || n <= 0 {
		return
	}
	if n > height {
		n = height
	}
	flags := s.wrapFlags()
	for y := bottom; y >= top+n; y-- {
		copy(s.gridLine(y), s.gridLine(y-n))
		flags[y] = flags[y-n]
	}
	for y := top; y < top+n; y++ {
		s.clearGridLine(y)
		flags[y] = false
	}
}

func (s *Screen) pushHistory(line []Cell) {
	cp := make([]Cell, len(line))
	copy(cp, line)
	s.history = append(s.history, cp)
	if len(s.history) > historyLimit {
		drop := len(s.history) - historyLimit
		copy(s.history, s.history[drop:])
		s.history = s.history[:historyLimit]
	}
	if s.viewOffset > 0 {
		s.viewOffset++
		if s.viewOffset > len(s.history) {
			s.viewOffset = len(s.history)
		}
	}
}

func (s *Screen) clearHistory() {
	s.history = nil
	s.viewOffset = 0
}

// moveUp, moveDown, moveLeft and moveRight are the relative cursor motions.
// They clamp to the grid and cancel a pending wrap.
func (s *Screen) moveUp(n int) {
	s.curY = clampInt(s.curY-n, 0, s.rows-1)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) moveDown(n int) {
	s.curY = clampInt(s.curY+n, 0, s.rows-1)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) moveLeft(n int) {
	s.curX = clampInt(s.curX-n, 0, s.cols-1)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) moveRight(n int) {
	s.curX = clampInt(s.curX+n, 0, s.cols-1)
	s.wrapNext = false
	s.lastValid = false
}

// cup is CUP/HVP, and shares its row arithmetic with VPA. In origin mode the
// row is relative to the top margin and stops at the bottom margin.
func (s *Screen) cup(row, col int) {
	row--
	if row < 0 {
		row = 0
	}
	if s.originMode {
		row += s.scrollTop
		if row > s.scrollBottom {
			row = s.scrollBottom
		}
	} else if row >= s.rows {
		row = s.rows - 1
	}
	col--
	if col < 0 {
		col = 0
	}
	if col >= s.cols {
		col = s.cols - 1
	}
	s.curX, s.curY = col, row
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) vpa(row int) {
	s.cup(row, s.curX+1)
}

func (s *Screen) hpa(col int) {
	s.curX = clampInt(col-1, 0, s.cols-1)
	s.wrapNext = false
	s.lastValid = false
}

// nextLineCursor is CNL/CPL: a vertical move that also goes to column 0.
func (s *Screen) nextLineCursor(n int) {
	s.curY = clampInt(s.curY+n, 0, s.rows-1)
	s.curX = 0
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) prevLineCursor(n int) {
	s.curY = clampInt(s.curY-n, 0, s.rows-1)
	s.curX = 0
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseCells(s.curY, s.curX, s.cols-1)
		for y := s.curY + 1; y < s.rows; y++ {
			s.clearGridLine(y)
		}
	case 1:
		for y := 0; y < s.curY; y++ {
			s.clearGridLine(y)
		}
		s.eraseCells(s.curY, 0, s.curX)
	case 2:
		s.clearGrid()
	case 3:
		s.clearGrid()
		s.clearHistory()
	default:
		return
	}
	s.lastValid = false
}

func (s *Screen) eraseLine(mode int) {
	switch mode {
	case 0:
		s.eraseCells(s.curY, s.curX, s.cols-1)
	case 1:
		s.eraseCells(s.curY, 0, s.curX)
	case 2:
		s.eraseCells(s.curY, 0, s.cols-1)
	default:
		return
	}
	s.lastValid = false
}

func (s *Screen) insertCells(n int) {
	if n <= 0 {
		n = 1
	}
	if n > s.cols-s.curX {
		n = s.cols - s.curX
	}
	if n <= 0 {
		return
	}
	line := s.gridLine(s.curY)
	copy(line[s.curX+n:], line[s.curX:s.cols-n])
	blank := s.blank()
	for x := s.curX; x < s.curX+n; x++ {
		line[x] = blank
	}
	sanitizeWideLine(line)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) deleteCells(n int) {
	if n <= 0 {
		n = 1
	}
	if n > s.cols-s.curX {
		n = s.cols - s.curX
	}
	if n <= 0 {
		return
	}
	line := s.gridLine(s.curY)
	copy(line[s.curX:], line[s.curX+n:])
	blank := s.blank()
	for x := s.cols - n; x < s.cols; x++ {
		line[x] = blank
	}
	sanitizeWideLine(line)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) eraseChars(n int) {
	if n <= 0 {
		n = 1
	}
	if n > s.cols-s.curX {
		n = s.cols - s.curX
	}
	s.eraseCells(s.curY, s.curX, s.curX+n-1)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) insertLines(n int) {
	if s.curY < s.scrollTop || s.curY > s.scrollBottom {
		return
	}
	height := s.scrollBottom - s.curY + 1
	if n <= 0 {
		n = 1
	}
	if n > height {
		n = height
	}
	flags := s.wrapFlags()
	for y := s.scrollBottom; y >= s.curY+n; y-- {
		copy(s.gridLine(y), s.gridLine(y-n))
		flags[y] = flags[y-n]
	}
	for y := s.curY; y < s.curY+n; y++ {
		s.clearGridLine(y)
		flags[y] = false
	}
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) deleteLines(n int) {
	if s.curY < s.scrollTop || s.curY > s.scrollBottom {
		return
	}
	height := s.scrollBottom - s.curY + 1
	if n <= 0 {
		n = 1
	}
	if n > height {
		n = height
	}
	flags := s.wrapFlags()
	for y := s.curY; y <= s.scrollBottom-n; y++ {
		copy(s.gridLine(y), s.gridLine(y+n))
		flags[y] = flags[y+n]
	}
	for y := s.scrollBottom - n + 1; y <= s.scrollBottom; y++ {
		s.clearGridLine(y)
		flags[y] = false
	}
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) scrollUpLines(n int) {
	if n <= 0 {
		n = 1
	}
	s.scrollUp(s.scrollTop, s.scrollBottom, n, false)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) scrollDownLines(n int) {
	if n <= 0 {
		n = 1
	}
	s.scrollDown(s.scrollTop, s.scrollBottom, n)
	s.wrapNext = false
	s.lastValid = false
}

func (s *Screen) setPrivateMode(mode int, on bool) {
	switch mode {
	case 1:
		s.appCursor = on
	case 3:
		if on {
			s.clearHistory()
		}
	case 6:
		s.originMode = on
		s.homeCursor()
	case 7:
		s.autoWrap = on
		if !on {
			s.wrapNext = false
		}
	case 12:
		// Cursor blink is ignored.
	case 25:
		s.cursorVisible = on
	case 47, 1047:
		s.switchAlternate(on, false)
	case 1048:
		if on {
			s.saveCursor()
		} else {
			s.restoreCursor()
		}
	case 1049:
		s.switchAlternate(on, true)
	case 9:
		if on {
			s.mouseMode = 9
		} else if s.mouseMode == 9 {
			s.mouseMode = 0
		}
	case 1000, 1002, 1003:
		if on {
			s.mouseMode = mode
		} else if s.mouseMode == mode {
			s.mouseMode = 0
		}
	case 1004:
		// Focus reporting is ignored.
	case 1006:
		s.mouseSGR = on
	case 1015:
		s.mouseURXVT = on
	case 2004:
		s.bracketPaste = on
	}
}

func (s *Screen) switchAlternate(on, save bool) {
	if on {
		if s.alternate {
			return
		}
		if save {
			s.saveCursor()
		} else {
			s.altSaved = cursorState{x: s.curX, y: s.curY, style: s.style, valid: true}
			s.altSavedSet = true
		}
		s.alternate = true
		s.clearAlternate()
		if save {
			s.curX, s.curY = 0, 0
			s.wrapNext = false
			s.lastValid = false
		}
		return
	}
	if !s.alternate {
		return
	}
	s.alternate = false
	if save {
		s.restoreCursor()
	} else if s.altSavedSet {
		s.restoreCursorState(s.altSaved)
	}
	s.wrapNext = false
	s.lastValid = false
}

// softReset is CSI ! p: default rendition, home cursor, a blank active grid,
// the whole grid as margin, and the usual modes. The history, the alternate
// buffer and the title survive, as they are not terminal state that DECSTR
// resets.
func (s *Screen) softReset() {
	s.style = Style{}
	s.curX, s.curY = 0, 0
	s.wrapNext = false
	s.lastValid = false
	s.cursorVisible = true
	s.autoWrap = true
	s.originMode = false
	s.appCursor = false
	s.bracketPaste = false
	s.mouseMode = 0
	s.mouseSGR = false
	s.mouseURXVT = false
	s.scrollTop, s.scrollBottom = 0, s.rows-1
	s.clearGrid()
	if f := s.wrapFlags(); f != nil {
		clear(f)
	}
	s.resetParser()
}

// fullReset is ESC c. It is a soft reset plus a return to the main screen,
// an empty alternate buffer, empty scrollback and default tab stops. The
// title and the working directory are window state, not screen state, and
// are kept.
func (s *Screen) fullReset() {
	s.softReset()
	s.alternate = false
	s.mainGrid = blankGrid(s.cols, s.rows)
	s.altGrid = blankGrid(s.cols, s.rows)
	s.mainWrap = make([]bool, s.rows)
	s.altWrap = make([]bool, s.rows)
	s.clearHistory()
	s.altSavedSet = false
	s.saved = cursorState{valid: true}
	s.resetTabs()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
