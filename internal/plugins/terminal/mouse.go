package terminal

import (
	"strconv"

	"wada-terminal/internal/vt"

	"github.com/egoist/mygo/ui"
)

// The button codes a terminal reports: xterm numbers the buttons, adds 32 to
// a motion, and takes the wheel as two more buttons.
const (
	mouseLeft       = 0
	mouseMiddle     = 1
	mouseRight      = 2
	mouseWheelUp    = 64
	mouseWheelDown  = 65
	mouseWheelLeft  = 66
	mouseWheelRight = 67
)

// xtermButton maps the window's button numbers (0 primary, 1 secondary,
// 2 middle) to a terminal's (0 left, 1 middle, 2 right).
func xtermButton(button int) int {
	switch button {
	case 0:
		return mouseLeft
	case 1:
		return mouseRight
	case 2:
		return mouseMiddle
	}
	return 0
}

// mouseMods are the modifiers a report carries: Shift is not one of them,
// since it turns reporting off and selects text here.
func mouseMods(mods ui.Modifiers) int {
	n := 0
	if mods&ui.Alt != 0 {
		n |= 8
	}
	if mods&ui.Ctrl != 0 {
		n |= 16
	}
	return n
}

// mouseReport is what a program is told of a pointer event: a button at a
// cell, a motion, or a release, in the form its tracking mode asked for.
// button is the terminal's number; x and y are cells of the screen.
func mouseReport(m vt.MouseState, button, x, y, mods int, motion, release bool) []byte {
	code := button
	if motion {
		code += 32
	}
	code += mods
	x++
	y++
	switch {
	case m.SGR:
		final := "M"
		if release {
			final = "m"
		}
		return []byte("\x1b[<" + strconv.Itoa(code) + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + final)
	case m.URXVT:
		if release {
			code = 3 + mods
		}
		return []byte("\x1b[" + strconv.Itoa(code+32) + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + "M")
	default: // X10 and the UTF-8 form xterm uses by default
		if release {
			code = 3 + mods
		}
		out := []byte("\x1b[M")
		out = append(out, mouseRun(code+32)...)
		out = append(out, mouseRun(x+32)...)
		return append(out, mouseRun(y+32)...)
	}
}

// mouseRun encodes one value of the three-byte form, which carries past
// column 95 as UTF-8.
func mouseRun(v int) []byte {
	switch {
	case v < 128:
		return []byte{byte(v)}
	case v < 2048:
		return []byte{byte(0xC0 | v>>6), byte(0x80 | v&0x3F)}
	default:
		return []byte{byte(0xE0 | v>>12), byte(0x80 | (v>>6)&0x3F), byte(0x80 | v&0x3F)}
	}
}
