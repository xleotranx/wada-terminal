package vt

// Snapshot is a screen at one moment: everything a new viewer needs to show it
// without replaying the bytes that made it. A terminal in the core sends one
// to a window that attaches, then streams what the program writes next.
//
// It is a value, not a live screen: its slices are the screen's own copies.
type Snapshot struct {
	Cols, Rows int
	Main, Alt  []Cell
	Alternate  bool
	MainWrap   []bool
	AltWrap    []bool

	Links       []string
	CurrentLink uint16
	FgRGB       [3]uint8
	BgRGB       [3]uint8

	CurX, CurY int
	Style      Style
	WrapNext   bool
	LastX      int
	LastY      int
	LastValid  bool

	CursorVisible bool
	AutoWrap      bool
	OriginMode    bool
	AppCursor     bool
	BracketPaste  bool
	MouseMode     int
	MouseSGR      bool
	MouseURXVT    bool

	ScrollTop    int
	ScrollBottom int
	Tabs         []bool

	Saved       SnapshotCursor
	AltSaved    SnapshotCursor
	AltSavedSet bool

	Title string
	Cwd   string
	Bell  bool

	Status   *Status
	Statuses map[string]Status
	Reported bool

	History    [][]Cell
	ViewOffset int
}

// SnapshotCursor is a cursor saved by a program with DECSC.
type SnapshotCursor struct {
	X, Y  int
	Style Style
	Valid bool
}

// Snapshot copies the screen and the records its program reported.
func (s *Screen) Snapshot() Snapshot {
	snap := Snapshot{
		Cols: s.cols, Rows: s.rows, Alternate: s.alternate,
		Main: copyGrid(s.mainGrid), Alt: copyGrid(s.altGrid),
		MainWrap: append([]bool(nil), s.mainWrap...), AltWrap: append([]bool(nil), s.altWrap...),
		Links: append([]string(nil), s.links...), CurrentLink: s.currentLink,
		FgRGB: s.fgRGB, BgRGB: s.bgRGB,
		CurX: s.curX, CurY: s.curY, Style: s.style, WrapNext: s.wrapNext,
		LastX: s.lastX, LastY: s.lastY, LastValid: s.lastValid,
		CursorVisible: s.cursorVisible, AutoWrap: s.autoWrap, OriginMode: s.originMode,
		AppCursor: s.appCursor, BracketPaste: s.bracketPaste,
		MouseMode: s.mouseMode, MouseSGR: s.mouseSGR, MouseURXVT: s.mouseURXVT,
		ScrollTop: s.scrollTop, ScrollBottom: s.scrollBottom,
		Tabs:        append([]bool(nil), s.tabs...),
		Saved:       SnapshotCursor{s.saved.x, s.saved.y, s.saved.style, s.saved.valid},
		AltSaved:    SnapshotCursor{s.altSaved.x, s.altSaved.y, s.altSaved.style, s.altSaved.valid},
		AltSavedSet: s.altSavedSet,
		Title:       s.title, Cwd: s.cwd, Bell: s.bellFlag,
		Reported: s.reported, ViewOffset: s.viewOffset,
	}
	if s.status != nil {
		status := *s.status
		snap.Status = &status
	}
	if len(s.statuses) > 0 {
		snap.Statuses = make(map[string]Status, len(s.statuses))
		for id, status := range s.statuses {
			snap.Statuses[id] = status
		}
	}
	snap.History = make([][]Cell, len(s.history))
	for i, line := range s.history {
		snap.History[i] = append([]Cell(nil), line...)
	}
	return snap
}

// Restore replaces the screen with a snapshot. It is used when a window
// attaches to a terminal the core already runs, so the screen is exactly what
// the program last drew.
func (s *Screen) Restore(snap Snapshot) {
	cols, rows := clampSize(snap.Cols, snap.Rows)
	s.cols, s.rows = cols, rows
	s.alternate = snap.Alternate
	s.mainGrid = restoredGrid(snap.Main, cols, rows)
	s.altGrid = restoredGrid(snap.Alt, cols, rows)
	s.mainWrap = restoredFlags(snap.MainWrap, rows)
	s.altWrap = restoredFlags(snap.AltWrap, rows)

	s.links = append([]string(nil), snap.Links...)
	s.currentLink = snap.CurrentLink
	if int(s.currentLink) >= len(s.links) {
		s.currentLink = 0
	}
	s.linkIDs = make(map[string]uint16, len(s.links))
	for i, link := range s.links {
		s.linkIDs[link] = uint16(i)
	}
	s.fgRGB, s.bgRGB = snap.FgRGB, snap.BgRGB

	s.curX, s.curY = min(max(snap.CurX, 0), cols-1), min(max(snap.CurY, 0), rows-1)
	s.style = snap.Style
	s.wrapNext = snap.WrapNext
	s.lastX, s.lastY, s.lastValid = snap.LastX, snap.LastY, snap.LastValid
	s.lastX, s.lastY = min(max(s.lastX, 0), cols-1), min(max(s.lastY, 0), rows-1)

	s.cursorVisible, s.autoWrap, s.originMode = snap.CursorVisible, snap.AutoWrap, snap.OriginMode
	s.appCursor, s.bracketPaste = snap.AppCursor, snap.BracketPaste
	s.mouseMode, s.mouseSGR, s.mouseURXVT = snap.MouseMode, snap.MouseSGR, snap.MouseURXVT

	s.scrollTop = min(max(snap.ScrollTop, 0), rows-1)
	s.scrollBottom = min(max(snap.ScrollBottom, 0), rows-1)
	if s.scrollBottom < s.scrollTop {
		s.scrollTop, s.scrollBottom = 0, rows-1
	}
	s.tabs = append([]bool(nil), snap.Tabs...)
	if len(s.tabs) != cols {
		s.tabs = nil
		s.resetTabs()
	}

	s.saved = cursorState{x: snap.Saved.X, y: snap.Saved.Y, style: snap.Saved.Style, valid: snap.Saved.Valid}
	s.altSaved = cursorState{x: snap.AltSaved.X, y: snap.AltSaved.Y, style: snap.AltSaved.Style, valid: snap.AltSaved.Valid}
	s.altSavedSet = snap.AltSavedSet

	s.title, s.cwd = snap.Title, snap.Cwd
	s.bellFlag = snap.Bell

	s.status = nil
	if snap.Status != nil {
		status := *snap.Status
		s.status = &status
	}
	s.statuses = make(map[string]Status, len(snap.Statuses))
	for id, status := range snap.Statuses {
		s.statuses[id] = status
	}
	s.reported = snap.Reported

	s.history = make([][]Cell, 0, len(snap.History))
	for _, line := range snap.History {
		s.history = append(s.history, append([]Cell(nil), line...))
	}
	if len(s.history) > historyLimit {
		s.history = s.history[len(s.history)-historyLimit:]
	}
	s.viewOffset = min(max(snap.ViewOffset, 0), len(s.history))

	s.replies = nil
	s.pendingStatus = nil
	s.ps = parser{}
}

// copyGrid copies a grid so a snapshot never shares cells with the screen.
func copyGrid(grid []Cell) []Cell {
	return append([]Cell(nil), grid...)
}

// restoredGrid is a snapshot grid at the snapshot's size, padded or cropped
// and repaired where a wide character lost its other half.
func restoredGrid(grid []Cell, cols, rows int) []Cell {
	out := blankGrid(cols, rows)
	if len(grid) == cols*rows {
		copy(out, grid)
		for y := 0; y < rows; y++ {
			sanitizeWideLine(out[y*cols : (y+1)*cols])
		}
		return out
	}
	return out
}

// restoredFlags is a wrap-flag slice at the new height.
func restoredFlags(flags []bool, rows int) []bool {
	out := make([]bool, rows)
	copy(out, flags)
	return out
}
