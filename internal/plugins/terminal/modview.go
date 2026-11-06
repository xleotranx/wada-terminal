package terminal

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/control"
	"wada-terminal/internal/mods"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// modState owns the panels, input values and animation scheduling.
type modState struct {
	// Mods: the loaded JavaScript panels, the tree each layout block last
	// rendered, and what their console.log calls have said.
	mods           map[string]*loadedMod
	modViews       map[*config.Block]*modView
	modInputs      map[*config.Block]map[string]*string
	modAutoFocused map[*config.Block]map[string]bool
	modFocus       map[*config.Block]map[string]bool
	modLogs        map[string][]string

	// Animating mods ask the window to draw at their own pace. A worker
	// ticks and posts an invalidation only while a frame saw an animation,
	// and sleeps while none does, so an idle window wakes for nothing.
	modAnimated      atomic.Bool
	modAnimatedFrame bool
	modAnimatedWake  chan struct{}
}

// loadedMod is one mod's JavaScript runtime, or why it did not load.
type loadedMod struct {
	runtime *mods.Runtime
	err     error
}

// modView is the tree a mod block last rendered, cached so a frame does not
// run JavaScript on every pass.
type modView struct {
	node     *mods.Node
	err      error
	at       time.Time
	interval time.Duration
	animated bool
}

// loadMods reads every mods/<name>/main.js in the config folder. A mod's
// state.json comes back with it; render errors are shown in its block.
func (t *Plugin) loadMods() {
	if t.configDir == "" {
		return
	}
	names, err := config.ListMods(t.configDir)
	if err != nil {
		t.configProblem("mods", err)
		return
	}
	modsByName := make(map[string]*loadedMod, len(names))
	for _, name := range names {
		mod, err := config.LoadMod(t.configDir, name)
		if err != nil {
			modsByName[name] = &loadedMod{err: err}
			config.WriteModError(t.configDir, name, err)
			continue
		}
		runtime, err := mods.New(name, mod.Source, t.modAPI(name, mod.State))
		if err != nil {
			modsByName[name] = &loadedMod{err: err}
			config.WriteModError(t.configDir, name, err)
			continue
		}
		config.ClearModError(t.configDir, name)
		modsByName[name] = &loadedMod{runtime: runtime}
	}
	t.mods = modsByName
	t.modViews = map[*config.Block]*modView{}
	t.modInputs = map[*config.Block]map[string]*string{}
	t.modAutoFocused = map[*config.Block]map[string]bool{}
	t.modFocus = map[*config.Block]map[string]bool{}
	if t.modLogs == nil {
		t.modLogs = map[string][]string{}
	}
}

// modAPI gives a mod the data and the actions it may use. Data is read on the
// UI thread and copied into JavaScript; run and send are part of a click's
// API only, never a render's.
func (t *Plugin) modAPI(name string, state map[string]any) *mods.API {
	return &mods.API{
		Spaces: func() any {
			list := t.controlList()
			out := make([]map[string]any, 0, len(list.Spaces))
			for _, space := range list.Spaces {
				out = append(out, map[string]any{
					"id": space.ID, "name": space.Name, "folder": space.Folder, "active": space.Active,
					"tabs": len(space.Tabs),
				})
			}
			return out
		},
		Agents: func() any {
			var out []map[string]any
			for _, e := range t.indexEntries() {
				if !e.s.isAgent() {
					continue
				}
				out = append(out, map[string]any{
					"id": e.s.id, "title": e.s.title(), "message": e.s.statusMsg,
					"state": e.s.apiState(), "word": e.s.statusWord(), "app": e.s.app,
					"workspace": e.w.name, "tab": e.tab + 1, "focused": t.focused(e.s),
				})
			}
			return out
		},
		Focused: func() int {
			if t.current != nil && t.current.focus != nil {
				return t.current.focus.id
			}
			return 0
		},
		State: state,
		SaveState: func(next map[string]any) error {
			return config.SaveState(t.configDir, name, next)
		},
		Run: func(action string, args map[string]any) (any, error) {
			spec, ok := control.FindAction(action)
			if !ok {
				return nil, fmt.Errorf("no action %q", action)
			}
			if err := control.Validate(spec, args); err != nil {
				return nil, err
			}
			return t.controlDo(action, args)
		},
		Send: func(terminal int, text string) error {
			s := t.sessionByID(terminal)
			if s == nil {
				return fmt.Errorf("no pane %d", terminal)
			}
			s.write([]byte(text))
			return nil
		},
		Log: func(line string) {
			if t.modLogs == nil {
				t.modLogs = map[string][]string{}
			}
			t.modLogs[name] = append(t.modLogs[name], line)
			if len(t.modLogs[name]) > 200 {
				t.modLogs[name] = t.modLogs[name][1:]
			}
		},
	}
}

// modBlock draws a layout's mod leaf: its cached tree, or the error that kept
// it from drawing.
func (t *Plugin) modBlock(c *ui.Context, ch chrome, block *config.Block) ui.Element {
	loaded := t.mods[block.Mod]
	if loaded == nil {
		return t.modError(c, ch, block.Mod, fmt.Errorf("no mods/%s/main.js", block.Mod))
	}
	if loaded.err != nil {
		return t.modError(c, ch, block.Mod, loaded.err)
	}
	if t.modViews == nil {
		t.modViews = map[*config.Block]*modView{}
	}
	view := t.modViews[block]
	if view == nil {
		view = &modView{}
		t.modViews[block] = view
	}
	interval := view.interval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if view.node == nil || time.Since(view.at) > interval {
		view.node, view.err = loaded.runtime.RenderForProps(block.Props, t.modInputValues(block))
		view.at = time.Now()
		view.interval = loaded.runtime.FrameEvery()
		view.animated = view.interval > 0 && view.err == nil
	}
	if view.animated {
		t.modAnimatedFrame = true
	}
	if view.err != nil {
		return t.modError(c, ch, block.Mod, view.err)
	}
	if view.node == nil {
		return t.modError(c, ch, block.Mod, fmt.Errorf("render returned nothing"))
	}
	root := ui.Box(c).Fill().MinWidth(0).MinHeight(0)
	root.Children(func() {
		t.modElement(c, ch, block, view.node, true)
	})
	return root
}

// modElement turns one mod node into a native element. Only palette names are
// accepted as colors; an unknown name leaves the default.
func (t *Plugin) modElement(c *ui.Context, ch chrome, block *config.Block, node *mods.Node, root bool) ui.Element {
	var element ui.Element
	switch node.Type {
	case "row", "column":
		if node.Type == "row" {
			element = ui.Row(c)
		} else {
			element = ui.Column(c)
		}
		if node.Style.Gap > 0 {
			element = element.Gap(node.Style.Gap)
		}
		if node.Style.Align != "" {
			element = element.AlignItems(alignFor(node.Style.Align))
		}
		if node.Style.Justify != "" {
			element = element.Justify(justifyFor(node.Style.Justify))
		}
		element.Children(func() {
			for _, child := range node.Children {
				t.modElement(c, ch, block, child, false)
			}
		})
	case "box":
		element = ui.Column(c)
		element.Children(func() {
			for _, child := range node.Children {
				t.modElement(c, ch, block, child, false)
			}
		})
	case "scroll":
		scroll := ui.Scroll(c).Grow(1).FillWidth().MinHeight(0)
		scroll.Children(func() {
			ui.Column(c).FillWidth().Gap(node.Style.Gap).Children(func() {
				for _, child := range node.Children {
					t.modElement(c, ch, block, child, false)
				}
			})
		})
		element = scroll
	case "text":
		element = ui.Text(c, node.Text)
		if node.Style.FontSize > 0 {
			element = element.FontSize(node.Style.FontSize)
		}
		if node.Style.Bold {
			element = element.FontWeight(600)
		}
		element = element.TextColor(paletteColor(ch, node.Style.Color, ch.pal.Text))
	case "button":
		element = modButton(c, ch, block, t, node)
	case "input":
		element = t.modInput(c, ch, block, node)
	case "spacer":
		element = ui.Box(c).Grow(1)
	case "divider":
		element = ui.Box(c).FillWidth().Height(1).Background(ch.pal.Hairline)
	default:
		element = ui.Box(c)
	}
	element = t.modStyle(element, ch, node)
	if root {
		element = element.Fill()
	}
	// Text, boxes and containers let a press fall through to the frame, so a
	// floating panel can be dragged by its body. Buttons and inputs still
	// take their own presses.
	if node.Type != "button" && node.Type != "input" && !node.Click {
		element = element.PassThrough()
	}
	return element
}

// modButton draws a mod's button and gives it the mod's click handler.
func modButton(c *ui.Context, ch chrome, block *config.Block, t *Plugin, node *mods.Node) ui.Element {
	button := ui.Box(c).MinHeight(26).Padding(5, 10).Radius(6).Center()
	button = button.Background(paletteColor(ch, node.Style.Bg, ch.pal.Raised))
	if button.Hovered() {
		button = button.Background(ch.pal.Hover)
	}
	button = button.Cursor(ui.CursorPointer)
	if node.Click {
		button.OnClick(func() { t.modClick(block, node.ID) })
	}
	button.Children(func() {
		text := ui.Text(c, node.Text).FontSize(max(node.Style.FontSize, 11.5)).
			TextColor(paletteColor(ch, node.Style.Color, ch.pal.Text)).SingleLine().Ellipsis("…")
		if node.Style.Bold {
			text = text.FontWeight(600)
		}
	})
	return button
}

// modStyle applies the common box style a node asked for.
func (t *Plugin) modStyle(element ui.Element, ch chrome, node *mods.Node) ui.Element {
	style := node.Style
	if style.Padding > 0 {
		element = element.Padding(style.Padding)
	}
	if style.Grow > 0 {
		element = element.Grow(style.Grow)
	}
	if style.Width > 0 {
		element = element.Width(style.Width)
	}
	if style.Height > 0 {
		element = element.Height(style.Height)
	}
	if style.MinWidth > 0 {
		element = element.MinWidth(style.MinWidth)
	}
	if style.MaxWidth > 0 {
		element = element.MaxWidth(style.MaxWidth)
	}
	if style.MinHeight > 0 {
		element = element.MinHeight(style.MinHeight)
	}
	if style.MaxHeight > 0 {
		element = element.MaxHeight(style.MaxHeight)
	}
	if style.Radius > 0 {
		element = element.Radius(style.Radius)
	}
	if style.Bg != "" {
		element = element.Background(paletteColor(ch, style.Bg, ui.Transparent))
	}
	if style.Border != "" {
		element = element.Border(1, paletteColor(ch, style.Border, ch.pal.Hairline))
	}
	if style.TranslateX != 0 || style.TranslateY != 0 {
		element = element.Translate(style.TranslateX, style.TranslateY)
	}
	if style.Scale > 0 && style.Scale != 1 {
		element = element.Scale(style.Scale, style.Scale)
	}
	if style.Rotate != 0 {
		element = element.Rotate(style.Rotate)
	}
	if style.Opacity > 0 && style.Opacity < 1 {
		element = element.Opacity(style.Opacity)
	}
	return element
}

// modClick calls a mod's click handler and makes its block render again.
func (t *Plugin) modClick(block *config.Block, id string) {
	loaded := t.mods[block.Mod]
	if loaded == nil || loaded.runtime == nil {
		return
	}
	if err := loaded.runtime.ClickWith(id, block.Props, t.modInputValues(block)); err != nil {
		t.host.Toast(ui.Toast{Title: "Mod " + block.Mod + " failed", Description: err.Error(),
			Type: components.ToastError})
	}
	t.invalidateMod(block)
}

// invalidateMod makes a block's script run again on the next frame.
func (t *Plugin) invalidateMod(block *config.Block) {
	if view := t.modViews[block]; view != nil {
		view.node = nil
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// inputFor returns the stable string a mod input edits. A layout block's
// inputs outlive a frame, so the native input keeps its caret and text.
func (t *Plugin) inputFor(block *config.Block, node *mods.Node) *string {
	if t.modInputs == nil {
		t.modInputs = map[*config.Block]map[string]*string{}
		t.modAutoFocused = map[*config.Block]map[string]bool{}
		t.modFocus = map[*config.Block]map[string]bool{}
	}
	values := t.modInputs[block]
	if values == nil {
		values = map[string]*string{}
		t.modInputs[block] = values
	}
	value := values[node.ID]
	if value == nil {
		value = new(string)
		*value = node.Value
		values[node.ID] = value
	}
	return value
}

// modAutofocused reports whether an input has already taken the keyboard.
// It is marked on the first frame, so a later click on a pane is not stolen
// back by a panel that re-renders twice a second.
func (t *Plugin) modAutofocused(block *config.Block, id string) bool {
	if t.modAutoFocused == nil {
		t.modAutoFocused = map[*config.Block]map[string]bool{}
		t.modFocus = map[*config.Block]map[string]bool{}
	}
	seen := t.modAutoFocused[block]
	if seen == nil {
		seen = map[string]bool{}
		t.modAutoFocused[block] = seen
	}
	if seen[id] {
		return true
	}
	seen[id] = true
	return false
}

// keepModFocus marks an input to take the keyboard in the next build, after
// the change invalidates and rebuilds its tree.
func (t *Plugin) keepModFocus(block *config.Block, id string) {
	if t.modFocus == nil {
		t.modFocus = map[*config.Block]map[string]bool{}
	}
	seen := t.modFocus[block]
	if seen == nil {
		seen = map[string]bool{}
		t.modFocus[block] = seen
	}
	seen[id] = true
}

// takeModFocus reports whether the next build should focus an input, and
// clears the mark.
func (t *Plugin) takeModFocus(block *config.Block, id string) bool {
	seen := t.modFocus[block]
	if !seen[id] {
		return false
	}
	delete(seen, id)
	return true
}

// modInputValues is what api.inputs shows a script for this block.
func (t *Plugin) modInputValues(block *config.Block) map[string]string {
	values := t.modInputs[block]
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for id, value := range values {
		if value != nil {
			out[id] = *value
		}
	}
	return out
}

// modInput draws a single-line input. Its text lives in the plugin, not the
// mod node, so typing and caret position survive the twice-a-second render.
func (t *Plugin) modInput(c *ui.Context, ch chrome, block *config.Block, node *mods.Node) ui.Element {
	input := ui.TextInput(c, t.inputFor(block, node)).FillWidth().FontSize(max(node.Style.FontSize, 11.5))
	if node.Placeholder != "" {
		input = input.Placeholder(node.Placeholder)
	}
	input = input.Label(node.ID)
	if t.takeModFocus(block, node.ID) {
		input = input.Focus()
	} else if node.AutoFocus && !t.modAutofocused(block, node.ID) {
		input = input.AutoFocus()
	}
	if node.Style.Color != "" {
		input = input.TextColor(paletteColor(ch, node.Style.Color, ch.pal.Text))
	}
	changed := input.Changed()
	submitted := input.Submitted()
	if changed || submitted {
		t.keepModFocus(block, node.ID)
	}
	if changed {
		t.ctx.Post(func() { t.invalidateMod(block) })
	}
	if submitted && node.Submit {
		t.ctx.Post(func() { t.modClick(block, node.ID) })
	}
	return input
}

// modError is the block a missing or broken mod draws.
func (t *Plugin) modError(c *ui.Context, ch chrome, name string, err error) ui.Element {
	panel := ui.Column(c).Fill().Padding(12).Gap(6).Background(ch.pal.DrawerBg)
	panel.Children(func() {
		ui.Text(c, "Mod "+name).FontSize(12.5).FontWeight(600).TextColor(ch.pal.Danger)
		ui.Text(c, err.Error()).FontSize(10.5).TextColor(ch.pal.Muted)
		ui.Text(c, "See mods/"+name+"/main.js and mods/"+name+"/error.").FontSize(9.5).TextColor(ch.pal.Faint)
	})
	return panel
}

// alignFor maps a mod's alignment name to the UI's.
func alignFor(name string) ui.Align {
	switch name {
	case "start":
		return ui.Start
	case "center":
		return ui.Center
	case "end":
		return ui.End
	default:
		return ui.Stretch
	}
}

// justifyFor maps a mod's main-axis name to the UI's.
func justifyFor(name string) ui.Align {
	switch name {
	case "center":
		return ui.Center
	case "end":
		return ui.End
	case "between":
		return ui.SpaceBetween
	default:
		return ui.Start
	}
}

// paletteColor picks a palette entry by the name a file may use. Unknown
// names fall back, so the chrome never takes a raw color from a mod.
func paletteColor(ch chrome, name string, fallback ui.Color) ui.Color {
	switch name {
	case "text":
		return ch.pal.Text
	case "muted":
		return ch.pal.Muted
	case "faint":
		return ch.pal.Faint
	case "link", "accent":
		return ch.pal.Link
	case "green":
		return ch.pal.GreenBadge
	case "waiting":
		return ch.pal.Waiting
	case "danger":
		return ch.pal.Danger
	case "warning":
		return ch.pal.Warning
	case "orange":
		return ch.pal.Orange
	case "panel":
		return ch.pal.PanelBg
	case "drawer":
		return ch.pal.DrawerBg
	case "raised":
		return ch.pal.Raised
	case "hover":
		return ch.pal.Hover
	case "selected":
		return ch.pal.Selected
	case "hairline":
		return ch.pal.Hairline
	case "transparent":
		return ui.Transparent
	default:
		return fallback
	}
}

// startModAnimation wakes the window at 60 Hz while a visible mod asked for
// animation with api.animate, and sleeps until a frame asks again while one
// did not: an idle window must not tick for an animation that is not there.
func (t *Plugin) startModAnimation() {
	t.ctx.Spawn(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.modAnimatedWake:
			}
			ticker := time.NewTicker(16 * time.Millisecond)
			for t.modAnimated.Load() {
				select {
				case <-ctx.Done():
					ticker.Stop()
					return
				case <-ticker.C:
					t.ctx.PostQuiet(func() { t.ctx.Invalidate() })
				}
			}
			ticker.Stop()
		}
	})
}
