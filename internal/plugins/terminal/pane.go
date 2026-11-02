package terminal

import (
	"math"
	"strings"
	"unicode/utf8"

	"wada-terminal/internal/vt"

	"github.com/egoist/mygo/ui"
)

// bodyPad is the room a pane's cells keep from its border.
const (
	bodyPadX = 8
	bodyPadY = 6
)

// pane draws one session: its header, and the cells of its screen.
func (t *Plugin) pane(c *ui.Context, ch chrome, s *session) {
	sf := t.terminalSurface(c)
	focused := t.focused(s)
	line := sf.border
	if focused {
		line = accentFor(s.kind, ch.dark)
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Radius(10).Clip().Background(t.terminalSurface(c).bg).
		Border(1, line).Children(func() {
		t.header(c, ch, s)
		t.body(c, ch, s, focused)
	})
}

// header is the pane's line of identity: the program's icon and title, the
// mode it reports, where it runs, and what the pane can do.
func (t *Plugin) header(c *ui.Context, ch chrome, s *session) {
	pal := ch.pal
	sf := t.terminalSurface(c)
	ui.Column(c).FillWidth().Shrink(0).Padding(7, 10, 7, 10).Gap(3).Children(func() {
		ui.Row(c).Height(16).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
			t.sessionIcon(c, ch, s, 15)
			ui.Text(c, s.title()).FontSize(12).FontWeight(600).TextColor(sf.fg).
				SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			if badge := s.statusText(); badge != "" {
				ui.Text(c, badge).FontSize(10).TextColor(pal.Faint).SingleLine()
			}
			if s.isAgent() {
				switch st := s.agentState(); st {
				case agentWorking, agentBlocked, agentDone:
					t.stateBadge(c, ch, st, s.stateLabel())
				}
			}
			if s.scrollMode {
				label := "SCROLL"
				if s.viVisual {
					label = "VISUAL"
				}
				ui.Text(c, label).FontSize(8.5).FontWeight(600).TextColor(pal.Waiting).
					Padding(0, 4).Shrink(0)
			}
			ui.Box(c).Grow(1).MinWidth(0)
			t.paneButton(c, ch, "◫", func() { t.splitAt(s, true) })
			t.paneButton(c, ch, "⬓", func() { t.splitAt(s, false) })
			t.paneButton(c, ch, "✕", func() { t.close(s) })
		})
		ui.Row(c).Height(13).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			text, color := s.subtitle(), pal.Muted
			if text == s.title() {
				text = ""
			}
			if !s.alive {
				text, color = "stopped", pal.Warning
			}
			ui.Text(c, text).FontSize(10.5).TextColor(color).SingleLine().Ellipsis("…").
				Shrink(1).MinWidth(0).Grow(1)
			ui.Text(c, shortenPath(s.cwd(), 34)).Font("monospace").FontSize(9.5).
				TextColor(pal.Faint).SingleLine().Ellipsis("…")
		})
	})
}

// stateBadge is what an agent in the pane is doing, beside its title. The
// label comes from the session, so a protocol's own kind says what it waits for.
func (t *Plugin) stateBadge(c *ui.Context, ch chrome, st agentState, label string) {
	color := stateColor(ch, st)
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Box(c).Width(5).Height(5).Radius(2.5).Shrink(0).Background(color)
		ui.Text(c, label).FontSize(9.5).TextColor(color).SingleLine()
	})
}

// paneButton is one of a pane header's small buttons.
func (t *Plugin) paneButton(c *ui.Context, ch chrome, glyph string, fn func()) {
	pal := ch.pal
	btn := ui.Box(c).Width(18).Height(16).Shrink(0).Radius(4).Center()
	if btn.Hovered() {
		btn.Background(pal.Hover)
	}
	btn.Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, glyph).FontSize(9.5).TextColor(pal.Faint)
	})
	btn.OnClick(fn)
}

// body is the pane's terminal: the cells of the screen, typed at, scrolled,
// and sized to the room it has.
func (t *Plugin) body(c *ui.Context, ch chrome, s *session, focused bool) {
	cellW, cellH := t.cell(c)
	s.reportColors(t.terminalSurface(c))
	el := ui.Box(c).Grow(1).FillWidth().MinWidth(0).MinHeight(0).Padding(bodyPadY, bodyPadX).
		Clip().Background(t.terminalSurface(c).bg)
	el = el.Bind(&s.body)
	el = el.Focusable()
	el = el.HandleInput(func(ev ui.InputEvent) bool { return t.input(s, ev) })
	el = el.TextCaret(ui.Rect{X: 0, Y: 0, W: 1, H: 1})
	el = el.InputMethod(false)
	el = el.Cursor(ui.CursorText)
	if s.hoverURL != "" {
		el = el.Cursor(ui.CursorPointer)
	}
	if focused {
		el = el.Focus()
	}
	if !s.alive && s.status != "" {
		t.stopped(c, ch, s, el)
		return
	}
	// The room of the box comes from the frame before, as a box knows its
	// size only once it is laid out; the first frame of a pane keeps the size
	// it started with.
	r := el.Bounds()
	cols, rows := s.cols, s.rows
	if w, h := r.W-2*bodyPadX, r.H-2*bodyPadY; w > 0 && h > 0 && cellW > 0 && cellH > 0 {
		cols = max(int(w/cellW), 2)
		rows = max(int(h/cellH), 2)
		s.resize(cols, rows)
	}
	el.Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Gap(0).Children(func() {
			for y := 0; y < rows; y++ {
				t.row(c, s, y, cols, cellW, cellH)
			}
		})
		if s.hoverURL != "" {
			bar := ui.Box(c).Absolute().Left(8).Right(8).Bottom(6).Height(24).Padding(0, 8).
				Radius(6).Background(ch.pal.Raised).Cursor(ui.CursorPointer)
			bar.OnClick(func() { t.host.OpenURL(s.hoverURL) })
			bar.Children(func() {
				ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Text(c, "↗").FontSize(10).TextColor(ch.pal.Link)
					ui.Text(c, s.hoverURL).FontSize(10).TextColor(ch.pal.Text).
						SingleLine().Ellipsis("…").Grow(1)
				})
			})
		}
	})
}

// stopped draws a session whose program ended: what happened, and a way back.
func (t *Plugin) stopped(c *ui.Context, ch chrome, s *session, el ui.Element) {
	pal := ch.pal
	el.Children(func() {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, s.status).FontSize(11.5).TextColor(pal.Muted).MaxWidth(420)
			btn := ui.Box(c).Height(26).Padding(0, 12).Radius(7).Background(pal.Raised).Center()
			if btn.Hovered() {
				btn.Background(pal.Hover)
			}
			btn.Cursor(ui.CursorPointer)
			btn.Children(func() { ui.Text(c, "Restart").FontSize(12).TextColor(pal.Text) })
			btn.OnClick(func() { t.restart(s) })
		})
	})
}

// row paints one line of a screen as one run of styled text: every run of
// cells of the same style is a span, and the cursor is a block of the
// terminal's own color.
// cellArt is the block geometry of one cell, to paint over the font's glyph.
type cellArt struct {
	col   int
	quads [][4]float32
	fg    ui.Color
	bg    ui.Color
}

// row paints one line of a screen as one run of styled text: every run of
// cells of the same style is a span, the cursor is a block of the terminal's
// own color, and the block characters a program draws its art with are
// rectangles that fill their cells, as a font's glyphs do not.
func (t *Plugin) row(c *ui.Context, s *session, y, cols int, cellW, cellH float32) {
	sf := t.terminalSurface(c)
	cells := s.screen.Row(y)
	cur := s.screen.Cursor()
	cursorRow := cur.Visible && cur.Y == y && s.screen.Scrolled() == 0

	var spans []ui.Span
	var art []cellArt
	var text strings.Builder
	var style ui.Span
	open := false
	flush := func() {
		if text.Len() == 0 {
			return
		}
		sp := style
		sp.Text = text.String()
		spans = append(spans, sp)
		text.Reset()
	}
	for x := 0; x < cols; x++ {
		var cell vt.Cell
		if x < len(cells) {
			cell = cells[x]
		}
		if cell.Width == 0 {
			continue // the second half of a wide character
		}
		sp := cellSpan(sf, cell.Style)
		if s.screen.LinkOf(cell) != "" {
			sp.Color, sp.Underline = sf.link, true
		}
		txt := cellText(cell)
		if t.viHighlight(s, y, x) {
			sp = ui.Span{Color: sf.selFg, Background: sf.selBg}
		} else if s.selected(x, y) {
			sp = ui.Span{Color: sf.selFg, Background: sf.selBg}
		} else if t.searchHighlight(s, y, x) {
			sp = ui.Span{Color: sf.fg, Background: chromeFor(c).pal.Waiting.Alpha(0.28)}
		}
		if cursorRow && cur.X == x && !s.sel.on {
			// A bar at the cell's edge, as macOS's own caret: a block the
			// size of the cell shouts over a program's own colors.
			art = append(art, cellArt{col: x, quads: caretBar(cellW), fg: sf.cursor})
		}
		// A block or box-drawing character is painted as rectangles, not a
		// glyph: the font's would leave seams between cells and lines.
		if r, ok := singleRune(cell.Text); ok {
			if a, isBox := boxArms(r); isBox && hasArms(a) {
				if quads := boxQuads(a, cellW, cellH); len(quads) > 0 {
					art = append(art, cellArt{col: x, quads: quads, fg: sp.Color, bg: sp.Background})
					txt = " "
				}
			} else if quads := blockArt(r); quads != nil {
				fg := sp.Color
				if a := shadeAlpha(r); a < 1 {
					fg = fg.Alpha(a)
				}
				art = append(art, cellArt{col: x, quads: quads, fg: fg, bg: sp.Background})
				txt = " " // keeps the cell's advance
			}
		}
		if !open || sp != style {
			flush()
			style, open = sp, true
		}
		text.WriteString(txt)
	}
	flush()
	// Trailing blanks paint nothing: drop the spans they make, and the
	// spaces at the end of the last one, so a line's text is what it shows.
	for len(spans) > 0 {
		last := &spans[len(spans)-1]
		if last.Background.A != 0 {
			break
		}
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			break
		}
		spans = spans[:len(spans)-1]
	}
	// A background run of only blanks at the end of a row is read as the
	// paragraph's trailing whitespace and paints nothing: an agent draws its
	// own cursor as one reversed space. A zero-width space after it keeps the
	// run visible without adding a glyph.
	if n := len(spans); n > 0 && spans[n-1].Background.A != 0 && strings.TrimSpace(spans[n-1].Text) == "" {
		spans = append(spans, ui.Span{Text: "\u200b"})
	}
	if len(spans) == 0 && len(art) == 0 {
		ui.Box(c).FillWidth().Shrink(0).Height(cellH)
		return
	}
	if len(spans) == 0 {
		spans = []ui.Span{{Text: " "}}
	}
	el := ui.RichText(c, spans...).FillWidth().Shrink(0).Font(t.fontFamily()).
		FontSize(float32(t.font.Get())).FixedLineHeight(cellH).NoWrap()
	if len(art) > 0 {
		// Painted over the glyphs: the caret sits on the character the
		// program left it on, and a bar under that character disappears
		// behind it, as it did in an agent's input field.
		el = el.DrawOver(func(p *ui.Painter, r ui.Rect) {
			// Edges snap to pixels, so the rectangles of neighbouring cells
			// share them without a seam.
			px := func(v float32) float32 { return float32(math.Round(float64(v))) }
			for _, a := range art {
				x0 := px(r.X + float32(a.col)*cellW)
				x1 := px(r.X + float32(a.col+1)*cellW)
				y0 := px(r.Y)
				y1 := px(r.Y + cellH)
				if a.bg.A != 0 {
					p.Fill(ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, a.bg, 0)
				}
				for _, q := range a.quads {
					qx0 := px(x0 + q[0]*(x1-x0))
					qx1 := px(x0 + (q[0]+q[2])*(x1-x0))
					qy0 := px(y0 + q[1]*(y1-y0))
					qy1 := px(y0 + (q[1]+q[3])*(y1-y0))
					if qx1 <= qx0 || qy1 <= qy0 {
						continue
					}
					p.Fill(ui.Rect{X: qx0, Y: qy0, W: qx1 - qx0, H: qy1 - qy0}, a.fg, 0)
				}
			}
		})
	}
}

// caretBar is the cursor's bar, two pixels wide, in fractions of the cell.
func caretBar(cellW float32) [][4]float32 {
	w := float32(2)
	if cellW > 0 && w > cellW/2 {
		w = cellW / 2
	}
	if cellW <= 0 {
		return [][4]float32{{0, .1, .2, .8}}
	}
	return [][4]float32{{0, .1, w / cellW, .8}}
}

// singleRune is the one rune a cell holds, and whether it holds exactly one.
func singleRune(text string) (rune, bool) {
	r, size := utf8.DecodeRuneInString(text)
	return r, size == len(text) && r != utf8.RuneError
}

// cellText is the text a cell paints: a space where it holds nothing, so the
// cell keeps its width.
func cellText(c vt.Cell) string {
	if c.Text == "" {
		return " "
	}
	return c.Text
}

// cellSpan is the style of a cell, as the window paints text.
func cellSpan(sf surface, st vt.Style) ui.Span {
	var sp ui.Span
	switch {
	case st.Reverse:
		sp.Color = sf.resolve(st.BG, sf.bg)
		sp.Background = sf.resolve(st.FG, sf.fg)
	default:
		sp.Color = sf.resolve(st.FG, sf.fg)
		if st.BG.Kind != vt.ColorDefault {
			sp.Background = sf.resolve(st.BG, sf.bg)
		}
	}
	if st.Bold {
		sp.Weight = 700
	}
	if st.Italic {
		sp.Italic = true
	}
	if st.Underline {
		sp.Underline = true
	}
	if st.Strike {
		sp.Strikethrough = true
	}
	if st.Dim {
		sp.Color = sp.Color.Alpha(0.6)
	}
	return sp
}

// cell measures one cell of the terminal's font, and remembers it until the
// font size changes.
func (t *Plugin) cell(c *ui.Context) (w, h float32) {
	size := t.font.Get()
	if t.cellFont == size && t.cellW > 0 && t.cellH > 0 {
		return t.cellW, t.cellH
	}
	mw, mh := c.MeasureText(0, ui.Span{Text: "MMMMMMMMMM", Font: t.fontFamily(), Size: float32(size)})
	t.cellW = mw / 10
	if t.cellW < 3 {
		t.cellW = float32(size) * 0.6
	}
	t.cellH = mh
	if t.cellH < 4 {
		t.cellH = float32(size) * 1.35
	}
	t.cellFont = size
	return t.cellW, t.cellH
}

// onAccent is the color of an icon's mark on its accent.
func onAccent(dark bool) ui.Color {
	if dark {
		return ui.Hex("#0e0e11")
	}
	return ui.Hex("#fbfbfc")
}

// shortenPath keeps the end of a path, which names it.
func shortenPath(path string, width int) string {
	if len(path) <= width {
		return path
	}
	return "…" + path[len(path)-width+1:]
}
