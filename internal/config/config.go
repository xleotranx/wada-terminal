// Package config is the window's interface as files: the settings, layout,
// keys and themes an agent can edit while Wada Terminal runs, and the
// AGENTS.md that map teaches it how. It reads and writes files; it draws
// nothing and knows no terminal.
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"wada-terminal/internal/appdir"
)

// Folder is the app's folder in the user's config directory. It matches the
// shell's data folder unless a run overrides the data folder separately.
const Folder = "wada-terminal"

// EnvDir names the variable that selects a config folder, for a test, a
// capture or a person keeping several setups.
const EnvDir = "WADATERM_CONFIG_DIR"

// Dir is where the interface files live: <config dir>/wada-terminal, with a
// -dev suffix for a development run. WADATERM_CONFIG_DIR selects it outright;
// when the app keeps its data elsewhere, its interface files go with it.
func Dir() (string, error) {
	if dir := os.Getenv(EnvDir); dir != "" {
		return dir, nil
	}
	return appdir.Dir(Folder, "WADATERM")
}

// AGENTS is written into the config folder on every start. Edits to it are
// overwritten; the files it describes are the ones a person or an agent edits.
//
//go:embed AGENTS.md
var AGENTS string

// Ensure makes the config folder and writes the files that should exist: the
// agent guide, a default settings file, layout, keys and one theme. It never
// overwrites a file a person may have edited, except the guide itself.
func Ensure(dir string) error {
	for _, sub := range []string{"", "layouts", "themes", "templates", "mods"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(AGENTS), 0o600); err != nil {
		return err
	}
	files := map[string]string{
		"settings.json":             defaultSettings,
		"keys.json":                 defaultKeys,
		"layouts/main.json":         defaultLayout,
		"themes/atom-one-dark.json": defaultTheme,
	}
	if err := EnsureMods(dir); err != nil {
		return err
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Settings is settings.json. A field left out keeps the value the window had
// (for sidebar) or the built-in default.
type Settings struct {
	FontSize   *float64 `json:"font_size,omitempty"`
	Sidebar    *bool    `json:"sidebar,omitempty"`
	Appearance *string  `json:"appearance,omitempty"`
	Theme      *string  `json:"theme,omitempty"`
	Layout     *string  `json:"layout,omitempty"`
}

// LoadSettings reads settings.json, applying the built-in values to fields it
// leaves out. A malformed file, an unknown key or a value out of range is an
// error: the window keeps its last good settings.
func LoadSettings(dir string) (Settings, error) {
	var s Settings
	if err := decodeStrict(filepath.Join(dir, "settings.json"), &s); err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return Settings{}, err
	}
	if s.FontSize != nil && (*s.FontSize < 9 || *s.FontSize > 22) {
		return Settings{}, fmt.Errorf("font_size is between 9 and 22")
	}
	if s.Appearance != nil {
		switch *s.Appearance {
		case "system", "light", "dark":
		default:
			return Settings{}, fmt.Errorf("appearance is system, light or dark, not %q", *s.Appearance)
		}
	}
	if s.Theme != nil && !safeName(*s.Theme) && *s.Theme != "auto" {
		return Settings{}, fmt.Errorf("theme is auto or a file name, not %q", *s.Theme)
	}
	if s.Layout != nil && !safeName(*s.Layout) {
		return Settings{}, fmt.Errorf("layout is a file name, not %q", *s.Layout)
	}
	return s, nil
}

// Layout is layouts/<name>.json. It arranges the window as a tree of rows and
// columns of blocks: the workspace tree, the panes, and the agents list. The
// older sidebar-only shape is still read and becomes the default row.
type Layout struct {
	Sidebar Sidebar `json:"sidebar,omitempty"`
	Root    *Block  `json:"root,omitempty"`
}

// Sidebar is the old layout shorthand: where the workspace tree sits and how
// wide it is.
type Sidebar struct {
	Side  string `json:"side,omitempty"`  // left or right
	Width int    `json:"width,omitempty"` // 180 to 360 pixels
}

// Block is one node of a layout: a row or column of children, or a leaf that
// draws the workspace tree, the panes, or the agents list.
type Block struct {
	Type       string         `json:"type"`                 // row, column, stack, workspaces, panes, agents, mod, float
	ID         string         `json:"id,omitempty"`         // names a stack or float
	Active     string         `json:"active,omitempty"`     // the child a stack shows
	Mod        string         `json:"mod,omitempty"`        // the folder in mods/ for a mod or float
	Props      map[string]any `json:"props,omitempty"`      // what the mod is showing
	Children   []Block        `json:"children,omitempty"`   // row, column and stack
	Edge       string         `json:"edge,omitempty"`       // float: top, right, bottom or left
	Align      string         `json:"align,omitempty"`      // float: start, center or end along the edge
	Offset     int            `json:"offset,omitempty"`     // float: pixels in from the edge
	Width      int            `json:"width,omitempty"`      // float: pixels
	Height     int            `json:"height,omitempty"`     // float: pixels
	Peek       int            `json:"peek,omitempty"`       // float: narrow edge strip until hovered
	Snap       bool           `json:"snap,omitempty"`       // float: spring to the nearest edge on release
	Size       *Size          `json:"size,omitempty"`       // how the parent divides room
	Min        int            `json:"min,omitempty"`        // pixels
	Max        int            `json:"max,omitempty"`        // pixels
	Padding    int            `json:"padding,omitempty"`    // pixels inside the block
	Gap        int            `json:"gap,omitempty"`        // pixels between row/column children
	Background string         `json:"background,omitempty"` // drawer, panel, raised, transparent
	Hidden     bool           `json:"hidden,omitempty"`
}

// Size is how a block takes room from its parent: fixed pixels along the
// parent's direction, or a share of what the fixed siblings leave.
type Size struct {
	Px     int     `json:"px,omitempty"`
	Weight float64 `json:"weight,omitempty"`
}

// LoadLayout reads layouts/<name>.json and validates its tree.
func LoadLayout(dir, name string) (Layout, error) {
	layout := Layout{Sidebar: Sidebar{Side: "left", Width: 236}}
	if !safeName(name) {
		return Layout{}, fmt.Errorf("layout is a file name, not %q", name)
	}
	if err := decodeStrict(filepath.Join(dir, "layouts", name+".json"), &layout); err != nil {
		if os.IsNotExist(err) {
			layout.Root = defaultRoot(layout.Sidebar)
			return layout, nil
		}
		return Layout{}, err
	}
	switch layout.Sidebar.Side {
	case "":
		layout.Sidebar.Side = "left"
	case "left", "right":
	default:
		return Layout{}, fmt.Errorf("sidebar.side is left or right, not %q", layout.Sidebar.Side)
	}
	if layout.Sidebar.Width == 0 {
		layout.Sidebar.Width = 236
	}
	if layout.Sidebar.Width < 180 || layout.Sidebar.Width > 360 {
		return Layout{}, fmt.Errorf("sidebar.width is between 180 and 360")
	}
	if layout.Root == nil {
		layout.Root = defaultRoot(layout.Sidebar)
	}
	counts := leafCounts{}
	blocks := 0
	stacks := map[string]bool{}
	if err := validateBlock(layout.Root, 0, &blocks, &counts, stacks); err != nil {
		return Layout{}, err
	}
	if counts.panes != 1 {
		return Layout{}, fmt.Errorf("a layout needs exactly one panes block, not %d", counts.panes)
	}
	if counts.workspaces > 1 || counts.agents > 1 || counts.machines > 1 || counts.tabs > 1 || counts.title > 1 {
		return Layout{}, fmt.Errorf("a layout has at most one workspaces, agents and machines block")
	}
	return layout, nil
}

// defaultRoot is the row the old sidebar shape describes: the workspace tree
// on its side, and the panes taking the rest.
func defaultRoot(sidebar Sidebar) *Block {
	workspaces := Block{Type: "workspaces", Size: &Size{Px: sidebar.Width}}
	panes := Block{Type: "panes", Size: &Size{Weight: 1}}
	children := []Block{workspaces, panes}
	if sidebar.Side == "right" {
		children = []Block{panes, workspaces}
	}
	return &Block{Type: "row", Children: children}
}

// leafCounts is what a layout must hold exactly once or at most once.
type leafCounts struct {
	panes, workspaces, agents, machines, tabs, title int
}

// validateBlock checks a node and its children, with a depth and block cap so
// a malformed save cannot make the window draw forever.
func validateBlock(block *Block, depth int, blocks *int, counts *leafCounts, stacks map[string]bool) error {
	*blocks++
	if *blocks > 64 {
		return fmt.Errorf("a layout has at most 64 blocks")
	}
	if depth > 8 {
		return fmt.Errorf("a layout is at most 8 levels deep")
	}
	if block.Padding < 0 || block.Padding > 64 {
		return fmt.Errorf("%s padding is between 0 and 64", block.Type)
	}
	if block.Offset < 0 || block.Offset > 64 {
		return fmt.Errorf("%s offset is between 0 and 64", block.Type)
	}
	if block.Width < 0 || block.Width > 800 || block.Height < 0 || block.Height > 600 {
		return fmt.Errorf("%s width is up to 800 and height up to 600", block.Type)
	}
	if block.Peek < 0 || block.Peek > 120 {
		return fmt.Errorf("%s peek is between 0 and 120", block.Type)
	}
	if block.Gap < 0 || block.Gap > 64 {
		return fmt.Errorf("%s gap is between 0 and 64", block.Type)
	}
	switch block.Background {
	case "", "drawer", "panel", "raised", "transparent":
	default:
		return fmt.Errorf("background is drawer, panel, raised or transparent, not %q", block.Background)
	}
	if block.Size != nil {
		if block.Size.Px != 0 && block.Size.Weight != 0 {
			return fmt.Errorf("%s has both px and weight", block.Type)
		}
		if block.Size.Px != 0 && (block.Size.Px < 80 || block.Size.Px > 800) {
			return fmt.Errorf("%s px is between 80 and 800", block.Type)
		}
		if block.Size.Weight < 0 || block.Size.Weight > 10 {
			return fmt.Errorf("%s weight is between 0 and 10", block.Type)
		}
	}
	if block.Min < 0 || block.Max < 0 || block.Min > 2000 || block.Max > 2000 {
		return fmt.Errorf("%s min and max are pixel counts up to 2000", block.Type)
	}
	if block.Min > 0 && block.Max > 0 && block.Min > block.Max {
		return fmt.Errorf("%s min is larger than its max", block.Type)
	}
	switch block.Type {
	case "row", "column":
		if len(block.Children) == 0 {
			return fmt.Errorf("a %s needs children", block.Type)
		}
		for i := range block.Children {
			if err := validateBlock(&block.Children[i], depth+1, blocks, counts, stacks); err != nil {
				return err
			}
		}
	case "stack":
		if len(block.Children) == 0 {
			return fmt.Errorf("a stack needs children")
		}
		if block.ID == "" {
			return fmt.Errorf("a stack needs an id")
		}
		if stacks[block.ID] {
			return fmt.Errorf("two stacks are named %q", block.ID)
		}
		stacks[block.ID] = true
		children := map[string]bool{}
		for i := range block.Children {
			child := &block.Children[i]
			if child.ID == "" {
				return fmt.Errorf("a stack child needs an id")
			}
			if children[child.ID] {
				return fmt.Errorf("stack %q has two children named %q", block.ID, child.ID)
			}
			children[child.ID] = true
			if err := validateBlock(child, depth+1, blocks, counts, stacks); err != nil {
				return err
			}
		}
		if block.Active != "" && !children[block.Active] {
			return fmt.Errorf("stack %q shows %q, which is not one of its children", block.ID, block.Active)
		}
	case "workspaces":
		counts.workspaces++
	case "panes":
		counts.panes++
	case "agents":
		counts.agents++
	case "machines":
		counts.machines++
	case "tabs":
		counts.tabs++
	case "title":
		counts.title++
	case "mod":
		if !safeName(block.Mod) {
			return fmt.Errorf("a mod block names a folder in mods/, not %q", block.Mod)
		}
	case "float":
		if block.ID == "" {
			return fmt.Errorf("a float needs an id")
		}
		if !safeName(block.Mod) {
			return fmt.Errorf("a float names a mod folder in mods/, not %q", block.Mod)
		}
		switch block.Edge {
		case "", "top", "right", "bottom", "left":
		default:
			return fmt.Errorf("float edge is top, right, bottom or left, not %q", block.Edge)
		}
		switch block.Align {
		case "", "start", "center", "end":
		default:
			return fmt.Errorf("float align is start, center or end, not %q", block.Align)
		}
		if block.Width != 0 && block.Width < 120 || block.Height != 0 && block.Height < 80 {
			return fmt.Errorf("a float is at least 120 by 80")
		}
	case "":
		return fmt.Errorf("a block needs a type")
	default:
		return fmt.Errorf("no block type %q", block.Type)
	}
	if block.Type != "row" && block.Type != "column" && block.Type != "stack" && len(block.Children) > 0 {
		return fmt.Errorf("%s cannot have children", block.Type)
	}
	return nil
}

// Keys is keys.json: command id to the keys that run it. A value may be one
// string or a list. An empty list leaves the command in the menu with no key.
type Keys map[string]KeyList

// KeyList is a key or a list of keys in keys.json.
type KeyList []string

// UnmarshalJSON accepts "cmd+t" or ["cmd+t", "ctrl+t"].
func (k *KeyList) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*k = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("a key is a string or a list of strings")
	}
	*k = many
	return nil
}

// LoadKeys reads keys.json. Keys are command ids, with or without the
// "terminal." prefix, as `commands` prints them.
func LoadKeys(dir string) (Keys, error) {
	body, err := os.ReadFile(filepath.Join(dir, "keys.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Keys{}, nil
		}
		return nil, err
	}
	var keys Keys
	if err := json.Unmarshal(body, &keys); err != nil {
		return nil, fmt.Errorf("keys.json: %w", err)
	}
	return keys, nil
}

// Theme is themes/<name>.json, in herdr-app's format.
type Theme struct {
	Background string   `json:"background"`
	Foreground string   `json:"foreground"`
	Selection  string   `json:"selection,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
	ANSI       []string `json:"ansi"`
}

// LoadTheme reads themes/<name>.json. "auto" and "" mean the window's own
// light and dark palettes, with no theme file.
func LoadTheme(dir, name string) (*Theme, error) {
	if name == "" || name == "auto" {
		return nil, nil
	}
	if !safeName(name) {
		return nil, fmt.Errorf("theme is a file name, not %q", name)
	}
	var theme Theme
	if err := decodeStrict(filepath.Join(dir, "themes", name+".json"), &theme); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no theme %q in %s", name, filepath.Join(dir, "themes"))
		}
		return nil, err
	}
	if len(theme.ANSI) != 16 {
		return nil, fmt.Errorf("theme %q needs 16 ansi colors, not %d", name, len(theme.ANSI))
	}
	for _, color := range append([]string{theme.Background, theme.Foreground, theme.Selection, theme.Cursor}, theme.ANSI...) {
		if color == "" {
			continue
		}
		if !validColor(color) {
			return nil, fmt.Errorf("theme %q has %q, which is not #rrggbb", name, color)
		}
	}
	if theme.Background == "" || theme.Foreground == "" {
		return nil, fmt.Errorf("theme %q needs background and foreground", name)
	}
	return &theme, nil
}

// Stamp describes every editable file and its modification time, so a worker
// can notice a save without reading the files.
func Stamp(dir string) string {
	var entries []string
	for _, pattern := range []string{"settings.json", "keys.json", "layouts/*.json", "themes/*.json", "machines.json", "agents.json", "programs.json", "mods/*/main.js"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, path := range matches {
			if info, err := os.Stat(path); err == nil {
				entries = append(entries, fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano()))
			}
		}
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

// WriteError records why a file did not load beside it, as <name>.error. The
// window keeps the last good version.
func WriteError(dir, name string, err error) {
	_ = os.WriteFile(filepath.Join(dir, name+".error"), []byte(err.Error()+"\n"), 0o600)
}

// ClearError removes the .error file for a name once it loads again.
func ClearError(dir, name string) {
	_ = os.Remove(filepath.Join(dir, name+".error"))
}

// decodeStrict reads JSON into v, refusing keys that have no meaning here.
func decodeStrict(path string, v any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// safeName is a name that cannot leave the folder it names a file in.
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\`)
}

// validColor is #rrggbb or #rrggbbaa.
var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

func validColor(color string) bool { return colorPattern.MatchString(color) }

const defaultSettings = `{
  "font_size": 12.5,
  "appearance": "system",
  "theme": "auto",
  "layout": "main"
}
`

const defaultKeys = `{
}
`

const defaultLayout = `{
  "sidebar": {
    "side": "left",
    "width": 236
  }
}
`

const defaultTheme = `{
  "background": "#282c34",
  "foreground": "#abb2bf",
  "selection": "#3e4451",
  "cursor": "#abb2bf",
  "ansi": [
    "#282c34", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#abb2bf",
    "#5c6370", "#e06c75", "#98c379", "#e5c07b", "#61afef", "#c678dd", "#56b6c2", "#ffffff"
  ]
}
`
