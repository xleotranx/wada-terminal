package core

import (
	mterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Theme is the colors a window shows a terminal in, as the core's terminal
// reports them to a program that asks (OSC 4, 10, 11 and 12, and whether
// they are dark). Each color is RGBA.
type Theme struct {
	Foreground    [4]uint8     `json:"fg"`
	Background    [4]uint8     `json:"bg"`
	Cursor        [4]uint8     `json:"cursor,omitempty"`
	CursorText    [4]uint8     `json:"cursorText,omitempty"`
	Selection     [4]uint8     `json:"selection,omitempty"`
	SelectionText [4]uint8     `json:"selectionText,omitempty"`
	Palette       [16][4]uint8 `json:"palette"`
}

// ThemeOf is a terminal theme as the core's protocol carries it.
func ThemeOf(th *mterm.Theme) *Theme {
	if th == nil {
		return nil
	}
	rgba := func(c ui.Color) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }
	out := &Theme{
		Foreground: rgba(th.Foreground), Background: rgba(th.Background),
		Cursor: rgba(th.Cursor), CursorText: rgba(th.CursorText),
		Selection: rgba(th.Selection), SelectionText: rgba(th.SelectionText),
	}
	for i, c := range th.Palette {
		out.Palette[i] = rgba(c)
	}
	return out
}

// terminalTheme is the theme the core's terminal takes.
func (t *Theme) terminalTheme() *mterm.Theme {
	if t == nil {
		return nil
	}
	color := func(c [4]uint8) ui.Color { return ui.Color{R: c[0], G: c[1], B: c[2], A: c[3]} }
	out := &mterm.Theme{
		Foreground: color(t.Foreground), Background: color(t.Background),
		Cursor: color(t.Cursor), CursorText: color(t.CursorText),
		Selection: color(t.Selection), SelectionText: color(t.SelectionText),
	}
	for i, c := range t.Palette {
		out.Palette[i] = color(c)
	}
	return out
}

// startTheme is the theme a terminal starts with, before a window says its
// own: the one the start names, else the default of its appearance.
func startTheme(start Start) *mterm.Theme {
	if th := start.Theme.terminalTheme(); th != nil {
		return th
	}
	if start.Dark {
		return mterm.DarkTheme()
	}
	return mterm.LightTheme()
}
