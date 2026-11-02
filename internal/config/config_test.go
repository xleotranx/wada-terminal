package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testDir is an ensured config folder.
func testDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestEnsureWritesTheInterface checks the files a fresh install gets.
func TestEnsureWritesTheInterface(t *testing.T) {
	dir := testDir(t)
	for _, name := range []string{"AGENTS.md", "settings.json", "keys.json", "layouts/main.json", "themes/atom-one-dark.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	guide, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{"settings.json", "keys.json", "themes/", "OSC 7501", "core run --bg"} {
		if !strings.Contains(string(guide), want) {
			t.Errorf("the guide does not mention %q", want)
		}
	}
	// Ensure never overwrites an edited settings file.
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"font_size": 18}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "18") {
		t.Fatalf("Ensure overwrote settings: %s", body)
	}
}

// TestLoadSettings checks partial files, defaults and the error a typo gets.
func TestLoadSettings(t *testing.T) {
	dir := testDir(t)
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Theme == nil || *s.Theme != "auto" || s.Layout == nil || *s.Layout != "main" || s.FontSize == nil || *s.FontSize != 12.5 {
		t.Fatalf("default settings = %+v", s)
	}
	if s.Sidebar != nil {
		t.Fatalf("the default file should leave sidebar to the window: %+v", s)
	}

	write(t, filepath.Join(dir, "settings.json"), `{"font_size": 18, "sidebar": false, "appearance": "dark", "theme": "atom-one-dark"}`)
	s, err = LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if *s.FontSize != 18 || *s.Sidebar || *s.Appearance != "dark" || *s.Theme != "atom-one-dark" {
		t.Fatalf("settings = %+v", s)
	}

	for _, body := range []string{
		`{"font_size": 2}`,
		`{"appearance": "sepia"}`,
		`{"theme": "../escape"}`,
		`{"fontsize": 12}`,
	} {
		write(t, filepath.Join(dir, "settings.json"), body)
		if _, err := LoadSettings(dir); err == nil {
			t.Errorf("settings %s were accepted", body)
		}
	}
}

// TestLoadLayout checks the sidebar arrangement and its errors.
func TestLoadLayout(t *testing.T) {
	dir := testDir(t)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Sidebar.Side != "left" || layout.Sidebar.Width != 236 {
		t.Fatalf("layout = %+v", layout)
	}
	write(t, filepath.Join(dir, "layouts/main.json"), `{"sidebar": {"side": "right", "width": 300}}`)
	layout, err = LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Sidebar.Side != "right" || layout.Sidebar.Width != 300 {
		t.Fatalf("layout = %+v", layout)
	}
	for _, body := range []string{`{"sidebar": {"side": "top"}}`, `{"sidebar": {"width": 20}}`, `{"sidebar": {"width": 999}}`} {
		write(t, filepath.Join(dir, "layouts/main.json"), body)
		if _, err := LoadLayout(dir, "main"); err == nil {
			t.Errorf("layout %s was accepted", body)
		}
	}
}

// TestLoadKeys checks the string and list forms and the empty list.
func TestLoadKeys(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "keys.json"), `{"new-tab": "cmd+t", "split-right": ["cmd+d", "ctrl+d"], "index": []}`)
	keys, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys["new-tab"]) != 1 || keys["new-tab"][0] != "cmd+t" {
		t.Fatalf("new-tab = %v", keys["new-tab"])
	}
	if len(keys["split-right"]) != 2 || keys["split-right"][1] != "ctrl+d" {
		t.Fatalf("split-right = %v", keys["split-right"])
	}
	if len(keys["index"]) != 0 {
		t.Fatalf("index = %v, want unbind", keys["index"])
	}
	write(t, filepath.Join(dir, "keys.json"), `{"new-tab": 3}`)
	if _, err := LoadKeys(dir); err == nil {
		t.Fatal("a numeric key was accepted")
	}
}

// TestLoadTheme checks herdr-app's theme format and the error a bad color gets.
func TestLoadTheme(t *testing.T) {
	dir := testDir(t)
	theme, err := LoadTheme(dir, "atom-one-dark")
	if err != nil {
		t.Fatal(err)
	}
	if theme.Background != "#282c34" || len(theme.ANSI) != 16 {
		t.Fatalf("theme = %+v", theme)
	}
	if theme, err := LoadTheme(dir, "auto"); err != nil || theme != nil {
		t.Fatalf("auto = %+v, %v", theme, err)
	}
	write(t, filepath.Join(dir, "themes/bad.json"), `{"background": "blue", "foreground": "#fff", "ansi": []}`)
	if _, err := LoadTheme(dir, "bad"); err == nil {
		t.Fatal("an invalid theme was accepted")
	}
	write(t, filepath.Join(dir, "themes/bad.json"), `{"background": "#000000", "foreground": "#ffffff", "ansi": ["#000000"]}`)
	if _, err := LoadTheme(dir, "bad"); err == nil {
		t.Fatal("a theme without sixteen colors was accepted")
	}
}

// TestStampChanges checks the signal a saves watcher waits for.
func TestStampChanges(t *testing.T) {
	dir := testDir(t)
	first := Stamp(dir)
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(dir, "settings.json"), `{"font_size": 14}`)
	if second := Stamp(dir); second == first {
		t.Fatal("the stamp did not change after a save")
	}
}

// TestWriteError checks the report beside a file that did not load.
func TestWriteError(t *testing.T) {
	dir := testDir(t)
	WriteError(dir, "settings.json", os.ErrInvalid)
	body, err := os.ReadFile(filepath.Join(dir, "settings.json.error"))
	if err != nil || !strings.Contains(string(body), "invalid") {
		t.Fatalf("settings.json.error = %q (%v)", body, err)
	}
	ClearError(dir, "settings.json")
	if _, err := os.Stat(filepath.Join(dir, "settings.json.error")); !os.IsNotExist(err) {
		t.Fatal("the error file was not removed")
	}
}

// write is a test helper for an edited config file.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDirFollowsDataDir checks that a capture or a test with a private data
// folder does not write into the developer's config folder.
func TestDirFollowsDataDir(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv("WADATERM_DATA_DIR", "/tmp/wada-data-here")
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/wada-data-here" {
		t.Fatalf("Dir() = %q, want the data folder", got)
	}
	t.Setenv(EnvDir, "/tmp/wada-config-here")
	if got, _ := Dir(); got != "/tmp/wada-config-here" {
		t.Fatalf("Dir() = %q, want the explicit config folder", got)
	}
}

// TestTemplates checks the template frontmatter, its body and its errors.
func TestTemplates(t *testing.T) {
	dir := testDir(t)
	if err := EnsureTemplates(dir); err != nil {
		t.Fatal(err)
	}
	if list, err := ListTemplates(dir); err != nil || len(list) != 0 {
		t.Fatalf("an empty templates folder = %+v, %v", list, err)
	}
	write(t, filepath.Join(dir, "templates/reviewer.md"), `---
description: Read-only review
command: pi
ready: quiet
submit: tab
paste_gap_ms: 80
---
Review the diff and report bugs.

Do not edit files.
`)
	template, err := LoadTemplate(dir, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if template.Name != "reviewer" || template.Description != "Read-only review" ||
		template.Command != "pi" || template.Ready != "quiet" || template.Submit != "tab" || template.PasteGapMS != 80 {
		t.Fatalf("template = %+v", template)
	}
	if !strings.Contains(template.Body, "Review the diff") || !strings.Contains(template.Body, "Do not edit") {
		t.Fatalf("body = %q", template.Body)
	}
	list, err := ListTemplates(dir)
	if err != nil || len(list) != 1 || list[0].Name != "reviewer" {
		t.Fatalf("ListTemplates = %+v, %v", list, err)
	}

	for _, bad := range []string{
		"no front matter",
		"---\ndescription: x\n---\nbody\n",
		"---\ncommand: sh\nready: fast\n---\n",
		"---\ncommand: sh\npaste_gap_ms: -1\n---\n",
		"---\ncommand: sh\n",
	} {
		write(t, filepath.Join(dir, "templates/bad.md"), bad)
		if _, err := LoadTemplate(dir, "bad"); err == nil {
			t.Errorf("template %q was accepted", bad)
		}
	}
}

// TestLayoutTree checks the composable layout format and the tree it
// synthesizes from the older sidebar-only shape.
func TestLayoutTree(t *testing.T) {
	dir := testDir(t)
	legacy, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Root == nil || legacy.Root.Type != "row" || len(legacy.Root.Children) != 2 ||
		legacy.Root.Children[0].Type != "workspaces" || legacy.Root.Children[1].Type != "panes" {
		t.Fatalf("legacy root = %+v", legacy.Root)
	}

	write(t, filepath.Join(dir, "layouts/board.json"), `{
	  "root": {
	    "type": "row",
	    "gap": 6,
	    "children": [
	      {"type": "workspaces", "size": {"px": 220}},
	      {"type": "panes", "size": {"weight": 1}},
	      {"type": "agents", "size": {"px": 260}, "background": "drawer"}
	    ]
	  }
	}`)
	layout, err := LoadLayout(dir, "board")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root.Gap != 6 || len(layout.Root.Children) != 3 || layout.Root.Children[2].Type != "agents" {
		t.Fatalf("board root = %+v", layout.Root)
	}

	bad := []string{
		`{"root": {"type": "row", "children": [{"type": "workspaces"}]}}`,                // no panes
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "panes"}]}}`,  // two panes
		`{"root": {"type": "row", "children": []}}`,                                      // no children
		`{"root": {"type": "wat"}}`,                                                      // no such block
		`{"root": {"type": "panes", "children": [{"type": "panes"}]}}`,                   // leaf children
		`{"root": {"type": "panes", "size": {"px": 100, "weight": 1}}}`,                  // both sizes
		`{"root": {"type": "panes", "background": "red"}}`,                               // not a palette name
		`{"root": {"type": "row", "children": [{"type": "panes", "size": {"px": 10}}]}}`, // too small
	}
	for _, body := range bad {
		write(t, filepath.Join(dir, "layouts/bad.json"), body)
		if _, err := LoadLayout(dir, "bad"); err == nil {
			t.Errorf("layout %s was accepted", body)
		}
	}
}

// TestMods checks the example mod, saving and loading its state, and mod
// blocks in a layout.
func TestMods(t *testing.T) {
	dir := testDir(t)
	names, err := ListMods(dir)
	if err != nil || len(names) != 1 || names[0] != "hello" {
		t.Fatalf("ListMods = %v, %v", names, err)
	}
	mod, err := LoadMod(dir, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mod.Source, "function render(api)") || !strings.Contains(mod.Source, "on_click") {
		t.Fatalf("source = %q", mod.Source)
	}
	if err := SaveState(dir, "hello", map[string]any{"count": 3}); err != nil {
		t.Fatal(err)
	}
	mod, err = LoadMod(dir, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if mod.State["count"] != float64(3) {
		t.Fatalf("state = %#v", mod.State)
	}
	if err := SaveState(dir, "../escape", nil); err == nil {
		t.Fatal("a state path escaped its mod")
	}

	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "mod", "mod": "hello", "size": {"px": 260}, "props": {"count": 1}}
	  ]}
	}`)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root.Children[1].Mod != "hello" || layout.Root.Children[1].Props["count"] != float64(1) {
		t.Fatalf("mod block = %+v", layout.Root.Children[1])
	}
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [{"type": "panes"}, {"type": "mod"}]}
	}`)
	if _, err := LoadLayout(dir, "main"); err == nil {
		t.Fatal("a mod block without a name was accepted")
	}
}

// TestLayoutStacks checks a stack's ids, active child and validation.
func TestLayoutStacks(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "stack", "id": "side", "active": "agents", "size": {"px": 280},
	     "children": [
	       {"type": "agents", "id": "agents"},
	       {"type": "mod", "id": "hello", "mod": "hello"}
	     ]}
	  ]}
	}`)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	stack := layout.Root.Children[1]
	if stack.Type != "stack" || stack.ID != "side" || stack.Active != "agents" || len(stack.Children) != 2 {
		t.Fatalf("stack = %+v", stack)
	}
	bad := []string{
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "stack", "children": [{"type": "agents", "id": "a"}]}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "stack", "id": "s", "active": "nope", "children": [{"type": "agents", "id": "a"}]}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "stack", "id": "s", "children": [{"type": "agents"}]}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "stack", "id": "s", "children": [{"type": "agents", "id": "a"}, {"type": "agents", "id": "a"}]}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "stack", "id": "s", "children": [{"type": "agents", "id": "a"}]}, {"type": "stack", "id": "s", "children": [{"type": "agents", "id": "b"}]}]}}`,
	}
	for _, body := range bad {
		write(t, filepath.Join(dir, "layouts/bad.json"), body)
		if _, err := LoadLayout(dir, "bad"); err == nil {
			t.Errorf("stack layout was accepted: %s", body)
		}
	}
}

// TestLayoutFloats checks a floating mod's edge, size and validation.
func TestLayoutFloats(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes", "size": {"weight": 1}},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right", "align": "center",
	     "width": 300, "height": 240, "offset": 16}
	  ]}
	}`)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	float := layout.Root.Children[1]
	if float.Type != "float" || float.Width != 300 || float.Edge != "right" {
		t.Fatalf("float = %+v", float)
	}
	bad := []string{
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "float"}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "float", "mod": "hello", "edge": "middle"}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "float", "mod": "hello", "align": "top"}]}}`,
		`{"root": {"type": "row", "children": [{"type": "panes"}, {"type": "float", "mod": "hello", "width": 20}]}}`,
	}
	for _, body := range bad {
		write(t, filepath.Join(dir, "layouts/bad.json"), body)
		if _, err := LoadLayout(dir, "bad"); err == nil {
			t.Errorf("float layout was accepted: %s", body)
		}
	}
}

// TestFloatPeek checks the peek strip's bounds.
func TestFloatPeek(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes"},
	    {"type": "float", "id": "hello", "mod": "hello", "edge": "right", "peek": 52}
	  ]}
	}`)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root.Children[1].Peek != 52 {
		t.Fatalf("peek = %d", layout.Root.Children[1].Peek)
	}
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes"},
	    {"type": "float", "id": "hello", "mod": "hello", "peek": 200}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err == nil {
		t.Fatal("an oversized peek was accepted")
	}
}

// TestLayoutMachinesPanel checks the Machines leaf and its singleton rule.
func TestLayoutMachinesPanel(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes"},
	    {"type": "machines", "size": {"px": 260}}
	  ]}
	}`)
	layout, err := LoadLayout(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root.Children[1].Type != "machines" {
		t.Fatalf("layout = %+v", layout.Root)
	}
	write(t, filepath.Join(dir, "layouts/main.json"), `{
	  "root": {"type": "row", "children": [
	    {"type": "panes"}, {"type": "machines"}, {"type": "machines"}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err == nil {
		t.Fatal("two machines panels were accepted")
	}
}

// TestAgents checks agents.json defaults, commands and validation.
func TestAgents(t *testing.T) {
	dir := testDir(t)
	agents, err := LoadAgents(dir)
	if err != nil || len(agents) != 0 {
		t.Fatalf("empty agents = %+v, %v", agents, err)
	}
	write(t, filepath.Join(dir, "agents.json"), `{
	  "fake": {"name": "Fake Agent", "command": ["fakeagent", "--fast"], "glyph": "F"}
	}`)
	agents, err = LoadAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	def := agents["fake"]
	if def.Name != "Fake Agent" || len(def.Command) != 2 || def.Command[1] != "--fast" || def.Glyph != "F" {
		t.Fatalf("agent = %+v", def)
	}
	if names := agents.AgentNames(); len(names) != 1 || names[0] != "fake" {
		t.Fatalf("names = %v", names)
	}
	write(t, filepath.Join(dir, "agents.json"), `{"fake": {"command": []}}`)
	agents, err = LoadAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if agents["fake"].Command[0] != "fake" {
		t.Fatalf("default command = %+v", agents["fake"])
	}
	write(t, filepath.Join(dir, "agents.json"), `{"../bad": {"command": ["x"]}}`)
	if _, err := LoadAgents(dir); err == nil {
		t.Fatal("an agent name that leaves the folder was accepted")
	}
}

// TestProgramsJSON adds runnable programs and their colors to the agent menu.
func TestProgramsJSON(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "programs.json"), `{
	  "programs": {
	    "aider": {"name": "Aider", "run": ["aider"], "glyph": "A", "color": "#ff8800"},
	    "viewer": {"name": "Viewer"}
	  }
	}`)
	agents, err := LoadAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	def, ok := agents["aider"]
	if !ok || def.Name != "Aider" || def.Color != "#ff8800" || def.Command[0] != "aider" {
		t.Fatalf("aider = %+v", def)
	}
	if _, exists := agents["viewer"]; exists {
		t.Fatal("an entry without run joined the Session menu")
	}
	write(t, filepath.Join(dir, "programs.json"), `{
	  "programs": {"bad": {"run": ["bad"], "color": "orange"}}
	}`)
	if _, err := LoadAgents(dir); err == nil {
		t.Fatal("an invalid program color was accepted")
	}
}

// TestAgentIconPaths accepts a relative icon and refuses one outside config.
func TestAgentIconPaths(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "agents.json"),
		`{"icon": {"command": ["x"], "icon": "icons/x.svg", "tint": true}}`)
	agents, err := LoadAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if def := agents["icon"]; def.Icon != "icons/x.svg" || !def.Tint {
		t.Fatalf("icon agent = %+v", def)
	}
	write(t, filepath.Join(dir, "agents.json"),
		`{"bad": {"command": ["x"], "icon": "../x.svg"}}`)
	if _, err := LoadAgents(dir); err == nil {
		t.Fatal("an icon outside the config folder was accepted")
	}
}

// TestTabsLayoutBlock lets a layout place the tab strip, at most once.
func TestTabsLayoutBlock(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts", "main.json"), `{
	  "root": {"type": "column", "children": [
	    {"type": "tabs", "size": {"px": 80}},
	    {"type": "panes", "size": {"weight": 1}}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "layouts", "main.json"), `{
	  "root": {"type": "column", "children": [
	    {"type": "tabs"}, {"type": "tabs"}, {"type": "panes"}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err == nil {
		t.Fatal("two tabs blocks were accepted")
	}
}

// TestTitleLayoutBlock lets a layout place the title-bar actions, at most once.
func TestTitleLayoutBlock(t *testing.T) {
	dir := testDir(t)
	write(t, filepath.Join(dir, "layouts", "main.json"), `{
	  "root": {"type": "column", "children": [
	    {"type": "title", "size": {"weight": 1}},
	    {"type": "panes", "size": {"weight": 1}}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "layouts", "main.json"), `{
	  "root": {"type": "column", "children": [
	    {"type": "title"}, {"type": "title"}, {"type": "panes"}
	  ]}
	}`)
	if _, err := LoadLayout(dir, "main"); err == nil {
		t.Fatal("two title blocks were accepted")
	}
}
