package terminal

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// input is what a pane does with the window's input: keys and text go to the
// program, the wheel and Shift+PageUp read the scrollback, and a press gives
// the pane the keyboard.
func (t *Plugin) input(s *session, ev ui.InputEvent) bool {
	if s.scrollMode {
		return t.scrollKey(s, ev)
	}
	switch ev.Kind {
	case ui.InputKeyDown:
		s.clearSelection()
		if ev.Mods&ui.Shift != 0 {
			switch ev.Key {
			case ui.KeyPageUp:
				s.screen.Scroll(10)
				return true
			case ui.KeyPageDown:
				s.screen.Scroll(-10)
				return true
			}
		}
		if b, ok := encodeKey(ev); ok {
			t.prompted(s)
			s.write(b)
			return true
		}
		return false
	case ui.InputText:
		if isTypedByAKey(ev.Text) {
			return true // Enter, Tab, Backspace and Escape came as keys
		}
		s.clearSelection()
		t.prompted(s)
		s.write([]byte(ev.Text))
		return true
	case ui.InputCompose:
		return false
	case ui.InputCommand:
		switch ev.Text {
		case "copy":
			t.copySelection()
			return true
		case "selectAll":
			t.selectAll()
			return true
		case "paste":
			text := t.services.ReadClipboard()
			if text != "" {
				s.clearSelection()
				t.prompted(s)
				s.write([]byte(strings.ReplaceAll(text, "\r\n", "\r")))
			}
			return true
		}
		return false
	case ui.InputScroll:
		if m, on := s.mouseReporting(ev); on {
			code := mouseWheelDown
			switch {
			case ev.DX < 0:
				code = mouseWheelLeft
			case ev.DX > 0:
				code = mouseWheelRight
			case ev.DY < 0:
				code = mouseWheelUp
			}
			x, y := s.cellAt(ev, t.cellW, t.cellH)
			s.write(mouseReport(m, code, x, y, mouseMods(ev.Mods), false, false))
			return true
		}
		s.clearSelection()
		lines := int(ev.DY / max(t.cellH, 1))
		if lines == 0 && ev.DY != 0 {
			lines = 1
		}
		// Scrolling down the content moves the view toward the bottom, and
		// the view's offset counts the lines it is above it.
		s.screen.Scroll(-lines)
		return true
	case ui.InputPointerDown:
		t.focusOn(s)
		if m, on := s.mouseReporting(ev); on {
			s.mouseBtn = ev.Button
			x, y := s.cellAt(ev, t.cellW, t.cellH)
			s.write(mouseReport(m, xtermButton(ev.Button), x, y, mouseMods(ev.Mods), false, false))
			return true
		}
		x, y := s.cellAt(ev, t.cellW, t.cellH)
		switch {
		case ev.Clicks >= 3:
			s.selectLineAt(y)
		case ev.Clicks == 2:
			s.selectWordAt(x, y)
		default:
			s.begin(x, y)
		}
		return true
	case ui.InputPointerMove:
		if m, on := s.mouseReporting(ev); on {
			x, y := s.cellAt(ev, t.cellW, t.cellH)
			mods := mouseMods(ev.Mods)
			switch m.Mode {
			case 1002: // motion only while a button is held
				if s.mouseBtn >= 0 {
					s.write(mouseReport(m, xtermButton(s.mouseBtn), x, y, mods, true, false))
				}
			case 1003: // any motion, button 3 for none
				button := 3
				if s.mouseBtn >= 0 {
					button = xtermButton(s.mouseBtn)
				}
				s.write(mouseReport(m, button, x, y, mods, true, false))
			}
			return true
		}
		if s.sel.dragging {
			s.extend(s.cellAt(ev, t.cellW, t.cellH))
			return true
		}
		// A pointer over a link says so with its shape.
		if ev.Button < 0 {
			x, y := s.cellAt(ev, t.cellW, t.cellH)
			if linked := s.linkAt(x, y); linked != s.hoverURL {
				s.hoverURL = linked
				return true
			}
		}
	case ui.InputPointerUp:
		if m, on := s.mouseReporting(ev); on {
			if s.mouseBtn >= 0 {
				button := s.mouseBtn
				s.mouseBtn = -1
				if m.Mode != 9 { // X10 reports presses alone
					x, y := s.cellAt(ev, t.cellW, t.cellH)
					s.write(mouseReport(m, xtermButton(button), x, y, mouseMods(ev.Mods), false, true))
				}
			}
			return true
		}
		if s.sel.dragging {
			s.finish()
			if !s.sel.on { // a click, not a drag: a link under it opens
				x, y := s.cellAt(ev, t.cellW, t.cellH)
				if url := s.linkAt(x, y); url != "" {
					t.host.OpenURL(url)
				}
			}
			return true
		}
	}
	return false
}

// cellAt is the cell a pointer position lies on, inside the pane's padding,
// clamped to the grid.
func (s *session) cellAt(ev ui.InputEvent, cellW, cellH float32) (int, int) {
	cols, rows := s.screen.Size()
	x, y := 0, 0
	if cellW > 0 && cellH > 0 {
		x = int((ev.X - bodyPadX) / cellW)
		y = int((ev.Y - bodyPadY) / cellH)
	}
	return min(max(x, 0), max(cols-1, 0)), min(max(y, 0), max(rows-1, 0))
}

// isTypedByAKey reports text that an element's key events already handled, so
// a terminal does not take it twice.
func isTypedByAKey(text string) bool {
	switch text {
	case "\r", "\n", "\t", "\x7f", "\x08":
		return true
	}
	return false
}

// encodeKey turns a key press into what a terminal sends for it. The zero
// answer says the key types text, which comes as an InputText event instead.
func encodeKey(ev ui.InputEvent) ([]byte, bool) {
	mods := ev.Mods
	if mods&ui.Super != 0 {
		return nil, false // the window's own shortcuts
	}
	alt := mods&ui.Alt != 0
	mod := 1
	if mods&ui.Shift != 0 {
		mod += 1
	}
	if alt {
		mod += 2
	}
	if mods&ui.Ctrl != 0 {
		mod += 4
	}
	if mods&ui.Ctrl != 0 {
		if b, ok := controlByte(ev); ok {
			if alt {
				b = append([]byte{0x1b}, b...)
			}
			return b, true
		}
	}
	switch ev.Key {
	case ui.KeyEnter:
		return []byte("\r"), true
	case ui.KeyTab:
		if mods&ui.Shift != 0 {
			return []byte("\x1b[Z"), true
		}
		return []byte("\t"), true
	case ui.KeyEscape:
		return []byte{0x1b}, true
	case ui.KeyBackspace:
		return []byte{0x7f}, true
	case ui.KeyUp:
		return withMods("A", mod, mods), true
	case ui.KeyDown:
		return withMods("B", mod, mods), true
	case ui.KeyRight:
		return withMods("C", mod, mods), true
	case ui.KeyLeft:
		return withMods("D", mod, mods), true
	case ui.KeyHome:
		return withMods("H", mod, mods), true
	case ui.KeyEnd:
		return withMods("F", mod, mods), true
	case ui.KeyInsert:
		return withTilde(2, mod, mods), true
	case ui.KeyDelete:
		return withTilde(3, mod, mods), true
	case ui.KeyPageUp:
		return withTilde(5, mod, mods), true
	case ui.KeyPageDown:
		return withTilde(6, mod, mods), true
	case ui.KeyF1:
		return withSS3("P", mod, mods), true
	case ui.KeyF2:
		return withSS3("Q", mod, mods), true
	case ui.KeyF3:
		return withSS3("R", mod, mods), true
	case ui.KeyF4:
		return withSS3("S", mod, mods), true
	case ui.KeyF5:
		return withTilde(15, mod, mods), true
	case ui.KeyF6:
		return withTilde(17, mod, mods), true
	case ui.KeyF7:
		return withTilde(18, mod, mods), true
	case ui.KeyF8:
		return withTilde(19, mod, mods), true
	case ui.KeyF9:
		return withTilde(20, mod, mods), true
	case ui.KeyF10:
		return withTilde(21, mod, mods), true
	case ui.KeyF11:
		return withTilde(23, mod, mods), true
	case ui.KeyF12:
		return withTilde(24, mod, mods), true
	}
	return nil, false
}

// withMods encodes an arrow or Home/End key: a plain sequence without
// modifiers, and the CSI 1;m form with them.
func withMods(final string, mod int, mods ui.Modifiers) []byte {
	if mods&(ui.Shift|ui.Alt|ui.Ctrl) == 0 {
		return []byte("\x1b[" + final)
	}
	return []byte("\x1b[1;" + strconv.Itoa(mod) + final)
}

// withTilde encodes an editing key whose code ends with ~.
func withTilde(code, mod int, mods ui.Modifiers) []byte {
	if mods&(ui.Shift|ui.Alt|ui.Ctrl) == 0 {
		return []byte("\x1b[" + strconv.Itoa(code) + "~")
	}
	return []byte("\x1b[" + strconv.Itoa(code) + ";" + strconv.Itoa(mod) + "~")
}

// withSS3 encodes the first four function keys, which xterm sends as SS3.
func withSS3(final string, mod int, mods ui.Modifiers) []byte {
	if mods&(ui.Shift|ui.Alt|ui.Ctrl) == 0 {
		return []byte("\x1bO" + final)
	}
	return []byte("\x1b[1;" + strconv.Itoa(mod) + final)
}

// controlByte is what Ctrl and a key send: the control character of a letter,
// or of one of the punctuation keys that have one.
func controlByte(ev ui.InputEvent) ([]byte, bool) {
	switch ev.Key {
	case ui.KeySpace:
		return []byte{0x00}, true
	case ui.KeyBackslash:
		return []byte{0x1c}, true
	case ui.KeyBracketRight:
		return []byte{0x1d}, true
	case ui.KeyMinus:
		if ev.Mods&ui.Shift != 0 {
			return []byte{0x1f}, true // Ctrl+_
		}
	case ui.Key6:
		if ev.Mods&ui.Shift != 0 {
			return []byte{0x1e}, true // Ctrl+^
		}
	case ui.KeyBracketLeft:
		return []byte{0x1b}, true // Ctrl+[
	}
	if ev.Key >= ui.KeyA && ev.Key <= ui.KeyZ {
		return []byte{byte(ev.Key-ui.KeyA) + 1}, true
	}
	return nil, false
}

// toggleScrollMode keeps the program's keyboard out of the way while a person
// moves through its history with vi-like keys.
func (t *Plugin) toggleScrollMode() {
	s := t.currentFocus()
	if s == nil {
		return
	}
	s.scrollMode = !s.scrollMode
	s.viVisual = false
	if s.scrollMode {
		s.viCursor = s.screen.VisibleTop()
		s.viAnchor = s.viCursor
		s.viAnchorCol, s.viCursorCol = 0, 0
	} else {
		s.screen.Scroll(-s.screen.History())
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// scrollKey handles the keys of scroll mode. It is deliberately a small vi
// subset: j/k and arrows move, g/G go to the ends, q or Escape leaves, and /
// opens find.
func (t *Plugin) scrollKey(s *session, ev ui.InputEvent) bool {
	if ev.Kind == ui.InputText {
		return true
	}
	if ev.Kind != ui.InputKeyDown {
		return ev.Kind == ui.InputKeyUp
	}
	switch ev.Key {
	case ui.KeyEscape:
		if s.viVisual {
			s.viVisual = false
		} else {
			s.scrollMode = false
			s.screen.Scroll(-s.screen.History())
		}
	case ui.KeyQ:
		s.scrollMode = false
		s.viVisual = false
		s.screen.Scroll(-s.screen.History())
	case ui.KeyV:
		if s.viVisual {
			s.viVisual = false
		} else {
			s.viVisual = true
			s.viLinewise = ev.Mods&ui.Shift != 0
			s.viAnchor = s.viCursor
			s.viAnchorCol = s.viCursorCol
		}
	case ui.KeyH, ui.KeyLeft:
		if s.viVisual && !s.viLinewise {
			t.moveVisualCol(s, -1)
		}
	case ui.KeyL, ui.KeyRight:
		if s.viVisual && !s.viLinewise {
			t.moveVisualCol(s, 1)
		}
	case ui.KeyW:
		if s.viVisual && !s.viLinewise {
			if line, col, ok := s.screen.WordStart(s.viCursor, s.viCursorCol); ok {
				t.setVisualCursor(s, line, col)
			}
		}
	case ui.KeyB:
		if s.viVisual && !s.viLinewise {
			if line, col, ok := s.screen.WordBack(s.viCursor, s.viCursorCol); ok {
				t.setVisualCursor(s, line, col)
			}
		}
	case ui.KeyE:
		if s.viVisual && !s.viLinewise {
			if line, col, ok := s.screen.WordEnd(s.viCursor, s.viCursorCol); ok {
				t.setVisualCursor(s, line, col)
			}
		}
	case ui.Key0:
		if s.viVisual && !s.viLinewise {
			s.viCursorCol = 0
		}
	case ui.Key4:
		if s.viVisual && !s.viLinewise && ev.Mods&ui.Shift != 0 {
			s.viCursorCol = s.screen.LineEnd(s.viCursor)
		}
	case ui.KeyD:
		if s.viVisual {
			s.viVisual = false
		}
	case ui.KeyY:
		if s.viVisual {
			t.copyVisual(s)
		}
	case ui.KeyP:
		if text := t.services.ReadClipboard(); text != "" {
			s.scrollMode = false
			s.viVisual = false
			s.screen.Scroll(-s.screen.History())
			t.prompted(s)
			s.write([]byte(text))
		}
	case ui.KeyJ, ui.KeyDown:
		if s.viVisual {
			t.moveVisual(s, 1)
		} else {
			s.screen.Scroll(-1)
		}
	case ui.KeyK, ui.KeyUp:
		if s.viVisual {
			t.moveVisual(s, -1)
		} else {
			s.screen.Scroll(1)
		}
	case ui.KeyG:
		if s.viVisual {
			if ev.Mods&ui.Shift != 0 {
				t.moveVisualTo(s, s.screen.TotalLines()-1)
			} else {
				t.moveVisualTo(s, 0)
			}
		} else if ev.Mods&ui.Shift != 0 {
			s.screen.Scroll(-s.screen.History())
		} else {
			s.screen.Scroll(s.screen.History())
		}
	case ui.KeyPageUp:
		if s.viVisual {
			t.moveVisual(s, -10)
		} else {
			s.screen.Scroll(10)
		}
	case ui.KeyPageDown:
		if s.viVisual {
			t.moveVisual(s, 10)
		} else {
			s.screen.Scroll(-10)
		}
	case ui.KeyHome:
		if s.viVisual {
			t.moveVisualTo(s, 0)
		} else {
			s.screen.Scroll(s.screen.History())
		}
	case ui.KeyEnd:
		if s.viVisual {
			t.moveVisualTo(s, s.screen.TotalLines()-1)
		} else {
			s.screen.Scroll(-s.screen.History())
		}
	case ui.KeySlash:
		t.openSearch()
	default:
		return true
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
	return true
}

// moveVisual moves the visual cursor and keeps it in view.
func (t *Plugin) moveVisual(s *session, delta int) {
	t.moveVisualTo(s, s.viCursor+delta)
}

// moveVisualTo moves the visual cursor to a line and centers it.
func (t *Plugin) moveVisualTo(s *session, line int) {
	if line < 0 {
		line = 0
	}
	if last := s.screen.TotalLines() - 1; line > last {
		line = last
	}
	s.viCursor = line
	s.viCursorCol = clampVisualCol(s, line, s.viCursorCol)
	s.screen.ScrollToLine(line)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// moveVisualCol moves the character cursor within the line.
func (t *Plugin) moveVisualCol(s *session, delta int) {
	s.viCursorCol = clampVisualCol(s, s.viCursor, s.viCursorCol+delta)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// clampVisualCol keeps a column inside the line's cells.
func clampVisualCol(s *session, line, col int) int {
	if col < 0 {
		return 0
	}
	if width := len(s.screen.LineText(line)); col >= width {
		if width > 0 {
			return width - 1
		}
		return 0
	}
	return col
}

// copyVisual puts the selected lines on the clipboard and ends the selection.
func (t *Plugin) copyVisual(s *session) {
	start, end := s.viAnchor, s.viCursor
	if start > end {
		start, end = end, start
	}
	lines := end - start + 1
	text := s.screen.LineBlock(start, end)
	if !s.viLinewise {
		fromLine, fromCol, toLine, toCol := s.viAnchor, s.viAnchorCol, s.viCursor, s.viCursorCol
		text = s.screen.TextBlock(fromLine, fromCol, toLine, toCol)
	}
	if text != "" {
		t.services.WriteClipboard(text)
	}
	s.viVisual = false
	t.host.Toast(ui.Toast{Title: "Copied", Description: strconv.Itoa(lines) + " lines"})
}

// setVisualCursor moves the character cursor to a line and column.
func (t *Plugin) setVisualCursor(s *session, line, col int) {
	s.viCursor = line
	s.viCursorCol = clampVisualCol(s, line, col)
	s.screen.ScrollToLine(line)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}
