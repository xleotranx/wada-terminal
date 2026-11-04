package terminal

import (
	"wada-terminal/internal/config"

	"github.com/xleotranx/wada-kit/native"

	mterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// surface is the palette a terminal paints with. A program chooses its own
// colors, so the cells keep the terminal's conventions (a black background, a
// light gray foreground, the sixteen ANSI hues) in both appearances rather
// than the window's palette; the chrome around them follows the palette.
type surface struct {
	bg, fg ui.Color
	cursor ui.Color
	border ui.Color
	selBg  ui.Color
	selFg  ui.Color
	link   ui.Color
	ansi   [16]ui.Color
}

var darkSurface = surface{
	bg:     ui.Hex("#0b0b0d"),
	fg:     ui.Hex("#cfcfd6"),
	cursor: ui.Hex("#e9e9ef"),
	border: ui.RGBA(255, 255, 255, 0.07),
	selBg:  ui.Hex("#264f78"),
	selFg:  ui.Hex("#f2f7ff"),
	link:   ui.Hex("#6cb6ff"),
	ansi: [16]ui.Color{
		ui.Hex("#1c1d22"), // black
		ui.Hex("#e06c75"), // red
		ui.Hex("#98c379"), // green
		ui.Hex("#e5c07b"), // yellow
		ui.Hex("#61afef"), // blue
		ui.Hex("#c678dd"), // magenta
		ui.Hex("#56b6c2"), // cyan
		ui.Hex("#c8ccd4"), // white
		ui.Hex("#5c6370"), // bright black
		ui.Hex("#ef7d86"), // bright red
		ui.Hex("#a9d48a"), // bright green
		ui.Hex("#f0d197"), // bright yellow
		ui.Hex("#7cc0f7"), // bright blue
		ui.Hex("#d79ae8"), // bright magenta
		ui.Hex("#6fd0da"), // bright cyan
		ui.Hex("#ffffff"), // bright white
	},
}

var lightSurface = surface{
	bg:     ui.Hex("#fbfbfc"),
	fg:     ui.Hex("#23232a"),
	cursor: ui.Hex("#1b1b20"),
	border: ui.RGBA(0, 0, 0, 0.09),
	selBg:  ui.Hex("#b6d7ff"),
	selFg:  ui.Hex("#12233a"),
	link:   ui.Hex("#1a5fb4"),
	ansi: [16]ui.Color{
		ui.Hex("#23232a"), // black
		ui.Hex("#c4344a"), // red
		ui.Hex("#3f7f3a"), // green
		ui.Hex("#8a6a00"), // yellow
		ui.Hex("#2f5fd0"), // blue
		ui.Hex("#8a3fb8"), // magenta
		ui.Hex("#127b86"), // cyan
		ui.Hex("#6b6b74"), // white
		ui.Hex("#8a8a93"), // bright black
		ui.Hex("#e0556b"), // bright red
		ui.Hex("#5a9e52"), // bright green
		ui.Hex("#a88600"), // bright yellow
		ui.Hex("#4a7fe8"), // bright blue
		ui.Hex("#a45fd0"), // bright magenta
		ui.Hex("#2a98a4"), // bright cyan
		ui.Hex("#b4b4bc"), // bright white
	},
}

// surfaceFor picks the terminal palette of the appearance on show.
func surfaceFor(c *ui.Context) surface {
	if c.Theme().Dark {
		return darkSurface
	}
	return lightSurface
}

// terminalSurface is the palette a pane paints with: a theme file's when one
// is selected, else the window's own light or dark one.
func (t *Plugin) terminalSurface(c *ui.Context) surface {
	if t.theme != nil {
		return *t.theme
	}
	return surfaceFor(c)
}

// surfaceFromTheme turns herdr-app's theme format into the palette the cells
// paint with. config.LoadTheme has already checked the colors and the sixteen
// ANSI entries.
func surfaceFromTheme(theme *config.Theme) surface {
	sf := darkSurface
	bg, fg := ui.Hex(theme.Background), ui.Hex(theme.Foreground)
	sf.bg, sf.fg = bg, fg
	sf.cursor = fg
	if theme.Cursor != "" {
		sf.cursor = ui.Hex(theme.Cursor)
	}
	if theme.Selection != "" {
		sf.selBg = ui.Hex(theme.Selection)
	} else if brightness(bg) < 128 {
		sf.selBg = ui.RGBA(255, 255, 255, 0.18)
	} else {
		sf.selBg = ui.RGBA(0, 0, 0, 0.15)
	}
	sf.selFg = fg
	for i, color := range theme.ANSI {
		sf.ansi[i] = ui.Hex(color)
	}
	sf.link = sf.ansi[4]
	if brightness(bg) < 128 {
		sf.border = ui.RGBA(255, 255, 255, 0.07)
	} else {
		sf.border = ui.RGBA(0, 0, 0, 0.09)
	}
	return sf
}

// brightness is a color's perceived lightness, for picking a border or a
// selection that shows on it.
func brightness(c ui.Color) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

// themeOf is a surface as the terminal view paints it.
func themeOf(sf surface) *mterm.Theme {
	return &mterm.Theme{
		Foreground: sf.fg, Background: sf.bg,
		Cursor: sf.cursor, Selection: sf.selBg, SelectionText: sf.selFg,
		Palette: sf.ansi,
	}
}

// terminalThemes are the colors a terminal shows: a theme file's in both
// appearances when one is selected, else the light and dark surfaces, which
// the view picks between as the window's appearance changes.
func (t *Plugin) terminalThemes() (light, dark *mterm.Theme) {
	if t.theme != nil {
		return themeOf(*t.theme), nil
	}
	return themeOf(lightSurface), themeOf(darkSurface)
}

// terminalTheme is the theme of the appearance the window shows now, which a
// program starting in the core is told until a window says its own.
func (t *Plugin) terminalTheme() *mterm.Theme { return themeOf(t.surface()) }

// refreshSessionThemes gives every terminal the colors of a theme file just
// loaded or dropped.
func (t *Plugin) refreshSessionThemes() {
	light, dark := t.terminalThemes()
	for _, s := range t.allSessions() {
		if s.term != nil {
			s.term.SetTheme(light, dark)
		}
	}
}

// accent is the color of a session's icon and its focus ring.
func accentFor(k kind, dark bool) ui.Color {
	switch k {
	case kindClaude:
		if dark {
			return ui.Hex("#d97757")
		}
		return ui.Hex("#bc5233")
	case kindCodex:
		if dark {
			return ui.Hex("#b9bec7")
		}
		return ui.Hex("#4a4f57")
	case kindOpenCode:
		if dark {
			return ui.Hex("#9aa4b2")
		}
		return ui.Hex("#31353c")
	default: // shell
		if dark {
			return ui.Hex("#7fb08a")
		}
		return ui.Hex("#3f7f5a")
	}
}

// chrome is the palette the page paints around the terminals.
type chrome struct {
	pal  native.Palette
	dark bool
}

func chromeFor(c *ui.Context) chrome {
	return chrome{pal: native.PaletteFor(c), dark: c.Theme().Dark}
}

// sessionStateColor is the color of a session's dot or badge: a shell runs
// rather than idling, so it keeps the green a program in a pane used to have.
func sessionStateColor(ch chrome, s *session) ui.Color {
	if !s.isAgent() {
		switch s.state() {
		case "stopped":
			return ch.pal.Warning
		case "failed":
			return ch.pal.Danger
		}
		return ch.pal.GreenBadge
	}
	return stateColor(ch, s.agentState())
}

// stateColor is the color of an agent's state dot or badge.
func stateColor(ch chrome, st agentState) ui.Color {
	switch st {
	case agentWorking:
		return ch.pal.GreenBadge
	case agentBlocked:
		return ch.pal.Waiting
	case agentDone:
		return ch.pal.Link
	case agentStopped:
		return ch.pal.Warning
	default:
		return ch.pal.Faint
	}
}
