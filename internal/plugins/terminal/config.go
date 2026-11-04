package terminal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wada-terminal/internal/config"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// loadConfig reads the interface files and applies them, before the window
// restores its sessions: a theme is what a program starting now should see.
// Without a config folder the window runs on its built-in defaults, and a
// broken file leaves the last good values in place.
func (t *Plugin) loadConfig() {
	dir, err := config.Dir()
	if err != nil {
		t.configProblem("config", err)
		_ = t.registerCommands()
		return
	}
	t.configDir = dir
	if err := config.Ensure(dir); err != nil {
		t.configProblem("config", err)
	}
	if machines, err := config.LoadMachines(dir); err != nil {
		t.configProblem("machines", err)
	} else {
		t.machineDefs = machines
	}
	if agents, err := config.LoadAgents(dir); err != nil {
		t.configProblem("agents", err)
	} else {
		t.agentDefs = agents
		t.loadAgentIcons()
	}
	t.applySettings()
	t.applyKeys()
	t.loadMods()
	t.connectPanelMachines()
}

// applySettings reads settings.json and its theme and layout. Any failure
// keeps what the window already has.
func (t *Plugin) applySettings() {
	if t.configDir == "" {
		return
	}
	settings, err := config.LoadSettings(t.configDir)
	if err != nil {
		config.WriteError(t.configDir, "settings", err)
		t.configProblem("settings.json", err)
		return
	}
	config.ClearError(t.configDir, "settings")
	if settings.FontSize != nil && *settings.FontSize != t.font.Get() {
		t.setFont(*settings.FontSize)
	}
	if settings.Sidebar != nil && *settings.Sidebar != t.sidebar.Get() {
		t.sidebar.Set(*settings.Sidebar)
	}
	if settings.Appearance != nil && *settings.Appearance != t.host.Appearance() {
		t.host.SetAppearance(*settings.Appearance)
	}
	themeName := "auto"
	if settings.Theme != nil {
		themeName = *settings.Theme
	}
	t.applyTheme(themeName)
	layoutName := "main"
	if settings.Layout != nil {
		layoutName = *settings.Layout
	}
	t.applyLayout(layoutName)
}

// applyTheme turns a theme file into the palette the terminal cells paint
// with. "auto" goes back to the window's own light and dark surfaces.
func (t *Plugin) applyTheme(name string) {
	if t.configDir == "" {
		return
	}
	theme, err := config.LoadTheme(t.configDir, name)
	if err != nil {
		config.WriteError(t.configDir, "theme", err)
		t.configProblem("theme", err)
		return
	}
	config.ClearError(t.configDir, "theme")
	t.themeName = name
	if theme == nil {
		t.theme = nil
		t.refreshSessionThemes()
		t.ctx.Invalidate()
		return
	}
	surface := surfaceFromTheme(theme)
	t.theme = &surface
	t.refreshSessionThemes()
	t.ctx.Invalidate()
}

// applyLayout reads a layout's sidebar place and width.
func (t *Plugin) applyLayout(name string) {
	if t.configDir == "" {
		return
	}
	layout, err := config.LoadLayout(t.configDir, name)
	if err != nil {
		config.WriteError(t.configDir, "layout", err)
		t.configProblem("layout", err)
		return
	}
	config.ClearError(t.configDir, "layout")
	t.layoutName = name
	t.sidebarPx = layout.Sidebar.Width
	t.sidebarAt = layout.Sidebar.Side
	t.windowLayout = &layout
	t.ctx.Invalidate()
}

// applyKeys reads keys.json and registers the commands again under a fresh
// scope. A bad file leaves the commands that were working alone.
func (t *Plugin) applyKeys() {
	if t.configDir == "" {
		return
	}
	keys, err := config.LoadKeys(t.configDir)
	if err != nil {
		config.WriteError(t.configDir, "keys", err)
		t.configProblem("keys.json", err)
		return
	}
	previous := t.keys
	t.keys = keys
	if err := t.registerCommands(); err != nil {
		t.keys = previous
		config.WriteError(t.configDir, "keys", err)
		t.configProblem("keys.json", err)
		if previous == nil {
			_ = t.registerCommands()
		}
		return
	}
	config.ClearError(t.configDir, "keys")
}

// reloadConfig is a config save, on the UI thread. It re-reads every file, so
// editing a theme and the settings that select it together still applies.
func (t *Plugin) reloadConfig() {
	if t.configDir == "" {
		return
	}
	if machines, err := config.LoadMachines(t.configDir); err != nil {
		t.configProblem("machines", err)
	} else {
		t.machineDefs = machines
	}
	if agents, err := config.LoadAgents(t.configDir); err != nil {
		t.configProblem("agents", err)
	} else {
		t.agentDefs = agents
		t.loadAgentIcons()
	}
	t.applySettings()
	t.applyKeys()
	t.loadMods()
	t.ctx.Invalidate()
}

// watchConfig is a worker that notices a save. A mtime stamp avoids reading
// files that did not change; the reload itself runs on the UI thread.
func (t *Plugin) watchConfig() {
	if t.configDir == "" {
		return
	}
	t.ctx.Spawn(func(ctx context.Context) {
		last := config.Stamp(t.configDir)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(600 * time.Millisecond):
			}
			stamp := config.Stamp(t.configDir)
			if stamp == last {
				continue
			}
			last = stamp
			t.ctx.Post(t.reloadConfig)
		}
	})
}

// configProblem says which file did not load and why, once per save.
func (t *Plugin) configProblem(name string, err error) {
	if t.host == nil {
		return
	}
	description := strings.TrimSpace(err.Error())
	if len(description) > 240 {
		description = description[:240] + "…"
	}
	t.host.Toast(ui.Toast{
		Title:       name + " did not load; keeping the last good one",
		Description: description,
		Type:        components.ToastError,
	})
}

// loadAgentIcons parses the SVG files agents.json and programs.json name.
// A bad file leaves that agent's glyph in place and is reported beside it.
func (t *Plugin) loadAgentIcons() {
	t.agentIcons = map[string]*ui.SVG{}
	for name, def := range t.agentDefs {
		if def.Icon == "" {
			continue
		}
		path := filepath.Join(t.configDir, def.Icon)
		body, err := os.ReadFile(path)
		if err != nil {
			config.WriteError(t.configDir, "agent-icon-"+name, err)
			continue
		}
		icon, err := ui.ParseSVG(body)
		if err != nil {
			config.WriteError(t.configDir, "agent-icon-"+name, err)
			continue
		}
		t.agentIcons[name] = icon
	}
}
