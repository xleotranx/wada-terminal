// Package vt emulates the screen of a terminal: it takes the bytes a program
// writes to its pseudo-terminal and answers the cells to paint, the cursor,
// the title and the working directory the program set.
//
// A Screen belongs to one goroutine: the window feeds it and paints it on the
// UI thread, and a worker posts what it read there rather than writing it
// itself. Write keeps what an escape sequence left open across calls, so a
// sequence split between two reads is still understood.
package vt

// ColorKind is what a Color names.
type ColorKind uint8

const (
	// ColorDefault is the terminal's own foreground or background, as the
	// program left it.
	ColorDefault ColorKind = iota
	// ColorANSI is one of the 256 colors of the ANSI palette.
	ColorANSI
	// ColorRGB is a color a program set with three components.
	ColorRGB
)

// Color is a cell's foreground or background.
type Color struct {
	Kind  ColorKind
	Index uint8 // ColorANSI's index: 0-15 are the palette, 16-255 the cube
	R     uint8 // ColorRGB's components
	G     uint8
	B     uint8
}

// Default is the terminal's own color.
var Default = Color{}

// ANSI is one of the 256 colors of the palette.
func ANSI(index uint8) Color { return Color{Kind: ColorANSI, Index: index} }

// RGB is a color with three components.
func RGB(r, g, b uint8) Color { return Color{Kind: ColorRGB, R: r, G: g, B: b} }

// Style is how a cell paints. Reverse swaps its colors as it paints, as a
// block cursor does.
type Style struct {
	FG, BG    Color
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
	Blink     bool
	Reverse   bool
	Strike    bool
}

// Cell is one box of the screen.
//
// A wide character (a CJK ideograph, an emoji) fills two cells: the first
// holds its Text with Width 2, the next is a continuation with Width 0 and no
// text, which a painter skips.
type Cell struct {
	Text  string // the grapheme cluster, "" where there is none
	Style Style
	Width uint8 // 1 for a character, 2 for a wide one, 0 for the second half
	// Link is the screen's number of the hyperlink the cell belongs to, 0
	// for none; Screen.LinkOf gives its URL.
	Link uint16
}

// MouseState is how a program wants the mouse reported, as it set the DEC
// private modes: not at all, on a press (X10), on presses and releases, or
// with the motions of a button or of any pointer.
type MouseState struct {
	// Mode is 0 for none, 9 for X10, or 1000, 1002 and 1003 for the
	// tracking modes.
	Mode  int
	SGR   bool // 1006: <b;x;y with M and m, which holds past column 223
	URXVT bool // 1015: the b;x;y form as well, without '<'
}

// Cursor is where the program's cursor is.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   Style // the cell's style, as the cursor inverts it
}

// Screen is a terminal's screen: its scrollback, its grid, and the parser
// that keeps them as the program writes.
type Screen struct {
	cols, rows int

	mainGrid  [][]Cell
	altGrid   [][]Cell
	alternate bool
	// mainWrap and altWrap mark the lines that continue on the next one,
	// which resizing re-wraps: a line ends at a newline, not at the margin.
	mainWrap []bool
	altWrap  []bool

	// replies are the answers to what the program asked since the last
	// TakeResponses: device attributes, the cursor's position, the colors.
	replies []string
	// links is the table of hyperlinks cells point into, and currentLink
	// the one a program opened with OSC 8, until it closes it.
	links       []string
	linkIDs     map[string]uint16
	currentLink uint16
	// fgRGB and bgRGB are the terminal's own colors, which a program reads
	// with OSC 10 and 11.
	fgRGB, bgRGB [3]uint8

	curX, curY   int
	style        Style
	wrapNext     bool
	lastX, lastY int
	lastValid    bool

	cursorVisible bool
	autoWrap      bool
	originMode    bool

	appCursor    bool
	bracketPaste bool
	mouseMode    int
	mouseSGR     bool
	mouseURXVT   bool

	scrollTop, scrollBottom int
	tabs                    []bool

	saved       cursorState
	altSaved    cursorState
	altSavedSet bool

	title, cwd string
	bellFlag   bool

	// status is the program's own OSC 7501 record, statuses the records it
	// reported for parts of its work, reported whether it ever sent one, and
	// pendingStatus the reports the window has not taken yet.
	status        *Status
	statuses      map[string]Status
	reported      bool
	pendingStatus []Status

	// history keeps the lines that scrolled off the main screen, oldest
	// first. It is a ring: once it holds historyLimit lines, historyHead is
	// the oldest and a new line overwrites it, so a scroll never shifts the
	// whole scrollback.
	history     [][]Cell
	historyHead int
	// freeRows are blank rows a scroll reuses instead of allocating cells
	// again, so a program that streams output does not churn the heap.
	freeRows   [][]Cell
	viewOffset int

	ps parser
}

// New makes a screen of cols by rows cells, with the cursor at its home.
func New(cols, rows int) *Screen { return newScreen(cols, rows) }

// Write applies what the program wrote, parsing escapes and text that a
// previous Write left incomplete.
func (s *Screen) Write(p []byte) { s.write(p) }

// Resize changes the grid. The main screen wraps the lines the program wrote
// again at the new width, and lines the new height has no room for scroll into
// the history, so the cursor stays on show. The alternate screen, which a
// full-screen program redraws, keeps its top-left and crops or pads instead.
func (s *Screen) Resize(cols, rows int) { s.resize(cols, rows) }

// Size is the grid's size in cells.
func (s *Screen) Size() (cols, rows int) { return s.cols, s.rows }

// Cell is the cell at x, y, seen with the current scroll offset; the zero
// Cell outside the grid.
func (s *Screen) Cell(x, y int) Cell { return s.cellAt(x, y) }

// Row returns y's cells, seen with the current scroll offset. The slice
// belongs to the screen: read it, do not keep it.
func (s *Screen) Row(y int) []Cell { return s.rowAt(y) }

// BottomRow returns the line y of the live screen, whether or not the view is
// scrolled back: detection reads what the program last drew, not what the user
// scrolled to.
func (s *Screen) BottomRow(y int) []Cell { return s.rowAtFrom(y, 0) }

// Cursor is where the program put the cursor, seen with the current scroll
// offset: it shows only while the view is at the bottom.
func (s *Screen) Cursor() Cursor { return s.cursor() }

// Title is what the program named its window, from OSC 0 or 2.
func (s *Screen) Title() string { return s.title }

// Cwd is the working directory the program reported with OSC 7, if any.
func (s *Screen) Cwd() string { return s.cwd }

// SetColors tells the screen the terminal's own foreground and background, as
// rgb, which it reports to a program that asks with OSC 10 or 11. Both are
// black until set.
func (s *Screen) SetColors(fg, bg [3]uint8) {
	s.fgRGB, s.bgRGB = fg, bg
}

// TakeResponses returns the replies the program asked for since the last call
// — device attributes, the cursor's position, the terminal's colors — and
// forgets them. The terminal writes each back to the program.
func (s *Screen) TakeResponses() []string {
	if len(s.replies) == 0 {
		return nil
	}
	out := s.replies
	s.replies = nil
	return out
}

// reply records an answer to a program's query, keeping a runaway program
// from growing the screen without bound.
func (s *Screen) reply(answer string) {
	if len(s.replies) < 64 {
		s.replies = append(s.replies, answer)
	}
}

// Mouse is how the program wants the mouse reported.
func (s *Screen) Mouse() MouseState {
	return MouseState{Mode: s.mouseMode, SGR: s.mouseSGR, URXVT: s.mouseURXVT}
}

// LinkOf is the URL a cell links to, empty when it links to nothing.
func (s *Screen) LinkOf(c Cell) string {
	if c.Link == 0 || int(c.Link) > len(s.links) {
		return ""
	}
	return s.links[c.Link-1]
}

// internLink numbers a hyperlink of the screen, reusing the number of a URL
// it already holds.
func (s *Screen) internLink(url string) uint16 {
	if url == "" {
		return 0
	}
	if s.linkIDs == nil {
		s.linkIDs = map[string]uint16{}
	}
	if id, ok := s.linkIDs[url]; ok {
		return id
	}
	if len(s.links) >= 1<<15 {
		return 0
	}
	s.links = append(s.links, url)
	id := uint16(len(s.links)) // one-based: 0 is no link
	s.linkIDs[url] = id
	return id
}

// Bell reports whether the program rang the bell since the last call, and
// forgets it.
func (s *Screen) Bell() bool {
	b := s.bellFlag
	s.bellFlag = false
	return b
}

// Alternate reports whether the program shows its alternate screen, as a
// full-screen program does.
func (s *Screen) Alternate() bool { return s.alternate }

// History is how many lines scrolled off the top of the main screen.
func (s *Screen) History() int { return len(s.history) }

// Scroll moves the view into the history by delta lines, positive going
// back; the view stops at the oldest line and at the bottom.
func (s *Screen) Scroll(delta int) { s.scroll(delta) }

// Scrolled is how many lines the view is above the bottom, 0 at the bottom.
func (s *Screen) Scrolled() int { return s.viewOffset }
