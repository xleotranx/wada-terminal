package terminal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/native"
)

// linkState is the link picker: the hyperlinks and URLs the focused pane's
// program printed.
type linkState struct {
	open    bool
	links   []printedLink
	sel     int
	session *session
}

// printedLink is a link a program printed: a URL in its text, with the line
// it is on counted from the top of the scrollback, or a hyperlink's target
// (OSC 8), which has no line, Line being -1.
type printedLink struct {
	URL  string
	Line int
}

// urlPattern finds the URLs a program prints as text.
var urlPattern = regexp.MustCompile(`(?:https?|file)://[^\s<>"'` + "`" + `]+`)

// printedLinks lists the targets of a program's hyperlinks, then the URLs in
// a terminal's text, each once.
func printedLinks(hyperlinks []string, text string) []printedLink {
	var out []printedLink
	seen := map[string]bool{}
	for _, url := range hyperlinks {
		if !seen[url] {
			seen[url] = true
			out = append(out, printedLink{URL: url, Line: -1})
		}
	}
	for i, line := range strings.Split(text, "\n") {
		for _, url := range urlPattern.FindAllString(line, -1) {
			url = strings.TrimRight(url, ".,;:!?)]}")
			if seen[url] {
				continue
			}
			seen[url] = true
			out = append(out, printedLink{URL: url, Line: i})
		}
	}
	return out
}

// openLinks shows the links of the focused pane, or closes the picker.
func (t *Plugin) openLinks() {
	if t.links.open {
		t.links = linkState{}
		if t.ctx != nil {
			t.ctx.Invalidate()
		}
		return
	}
	s := t.currentFocus()
	if s == nil {
		return
	}
	t.links = linkState{open: true, session: s, links: printedLinks(s.links, s.screenText())}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// linkDialog is the overlay that lists links before one opens.
func (t *Plugin) linkDialog(c *ui.Context) {
	native.Dialog(c, &t.links.open, 560, "Links", func() {
		t.linkBody(c)
	})
}

// linkBody draws the picker's list and keys.
func (t *Plugin) linkBody(c *ui.Context) {
	ch := chromeFor(c)
	pal := ch.pal
	if t.links.sel >= len(t.links.links) {
		t.links.sel = len(t.links.links) - 1
	}
	if t.links.sel < 0 {
		t.links.sel = 0
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, "Links").FontSize(18).FontWeight(600).TextColor(pal.Text)
		ui.Box(c).Grow(1).MinWidth(0)
		ui.Text(c, fmt.Sprintf("%d", len(t.links.links))).FontSize(10).TextColor(pal.Faint)
	})
	ui.Text(c, "Links this pane's program printed, on its screen and in its scrollback.").FontSize(11).
		TextColor(pal.Muted).SingleLine().Ellipsis("…")
	if len(t.links.links) == 0 {
		ui.Column(c).FillWidth().Height(100).Center().Gap(6).Children(func() {
			ui.Text(c, "No links on this screen").FontSize(13).TextColor(pal.Muted)
			keyHint(c, ch, "esc", "close")
		})
		return
	}
	var chosen ui.Element
	list := ui.Scroll(c).FillWidth().MaxHeight(360).Shrink(0).Focusable().Focus().
		HandleInput(func(ev ui.InputEvent) bool { return t.linkKey(ev) })
	list.Children(func() {
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			for i, link := range t.links.links {
				row := ui.Box(c).Height(36).FillWidth().Shrink(0).Radius(6).Padding(0, 10)
				switch {
				case i == t.links.sel:
					row.Background(pal.Selected)
				case row.Hovered():
					row.Background(pal.Hover)
				}
				row.Cursor(ui.CursorPointer).OnClick(func() { t.openLink(i) })
				row.Children(func() {
					ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(9).Children(func() {
						ui.Text(c, "↗").FontSize(11).TextColor(pal.Link)
						ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
							ui.Text(c, link.URL).FontSize(11).TextColor(pal.Text).
								SingleLine().Ellipsis("…")
							where := "hyperlink"
							if link.Line >= 0 {
								where = fmt.Sprintf("line %d", link.Line+1)
							}
							ui.Text(c, where).FontSize(8.5).TextColor(pal.Faint)
						})
					})
				})
				if i == t.links.sel {
					chosen = row
				}
			}
		})
	})
	_ = chosen
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		keyHint(c, ch, "↑↓", "move")
		keyHint(c, ch, "↵", "open")
		keyHint(c, ch, "esc", "close")
	})
}

// linkKey moves through the link list and opens the chosen URL.
func (t *Plugin) linkKey(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	switch ev.Key {
	case ui.KeyEscape:
		t.links.open = false
	case ui.KeyUp, ui.KeyK:
		if t.links.sel > 0 {
			t.links.sel--
		}
	case ui.KeyDown, ui.KeyJ:
		if t.links.sel < len(t.links.links)-1 {
			t.links.sel++
		}
	case ui.KeyEnter:
		t.openLink(t.links.sel)
	default:
		return false
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
	return true
}

// openLink opens one link and closes the picker.
func (t *Plugin) openLink(i int) {
	if i < 0 || i >= len(t.links.links) {
		return
	}
	url := t.links.links[i].URL
	t.links = linkState{}
	t.host.OpenURL(url)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}
