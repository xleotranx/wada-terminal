package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ModsDir is where a config folder keeps its mods.
func ModsDir(dir string) string { return filepath.Join(dir, "mods") }

// Mod is one loaded mod folder.
type Mod struct {
	Name   string
	Source string
	State  map[string]any
}

// LoadMod reads mods/<name>/main.js and its state.json, if any.
func LoadMod(dir, name string) (Mod, error) {
	if !safeName(name) {
		return Mod{}, fmt.Errorf("mod is a folder name, not %q", name)
	}
	source, err := os.ReadFile(filepath.Join(ModsDir(dir), name, "main.js"))
	if err != nil {
		if os.IsNotExist(err) {
			return Mod{}, fmt.Errorf("no mods/%s/main.js in %s", name, dir)
		}
		return Mod{}, err
	}
	mod := Mod{Name: name, Source: string(source)}
	if body, err := os.ReadFile(StatePath(dir, name)); err == nil {
		if err := jsonUnmarshal(body, &mod.State); err != nil {
			return Mod{}, fmt.Errorf("mods/%s/state.json: %w", name, err)
		}
	}
	return mod, nil
}

// StatePath is a mod's saved state.
func StatePath(dir, name string) string {
	return filepath.Join(ModsDir(dir), name, "state.json")
}

// SaveState writes a mod's state atomically.
func SaveState(dir, name string, state map[string]any) error {
	if !safeName(name) {
		return fmt.Errorf("mod is a folder name, not %q", name)
	}
	if err := os.MkdirAll(filepath.Join(ModsDir(dir), name), 0o700); err != nil {
		return err
	}
	body, err := jsonMarshalIndent(state)
	if err != nil {
		return err
	}
	path := StatePath(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureMods makes the mods folder and writes a small example when no hello
// mod exists. The example is only written once, so an edited or deleted one
// stays the person's.
func EnsureMods(dir string) error {
	if err := os.MkdirAll(ModsDir(dir), 0o700); err != nil {
		return err
	}
	folder := filepath.Join(ModsDir(dir), "hello")
	if _, err := os.Stat(folder); err == nil {
		return nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(folder, "main.js"), []byte(defaultMod), 0o600)
}

const defaultMod = `// hello: a tiny panel. Put it in a layout with
//   {"type": "mod", "mod": "hello", "size": {"px": 260}}
// then save the layout.
function render(api) {
  return {
    type: "column",
    padding: 12,
    gap: 8,
    children: [
      {type: "text", text: "Hello from a mod", font_size: 14, bold: true},
      {type: "text", color: "muted", font_size: 11,
       text: api.focused ? "Focused pane " + api.focused : "No pane has the keyboard"},
      {type: "button", id: "count", on_click: true,
       text: "Clicks: " + (api.state.count || 0)}
    ]
  };
}
function click(id, api) {
  if (id === "count") { api.state.count = (api.state.count || 0) + 1; }
}
`

// ListMods returns the folders in mods/ that have a main.js, sorted.
func ListMods(dir string) ([]string, error) {
	entries, err := os.ReadDir(ModsDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !safeName(entry.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(ModsDir(dir), entry.Name(), "main.js")); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// WriteModError records why a mod did not load beside its folder, as
// mods/<name>/error.
func WriteModError(dir, name string, err error) {
	if !safeName(name) {
		return
	}
	folder := filepath.Join(ModsDir(dir), name)
	if os.MkdirAll(folder, 0o700) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(folder, "error"), []byte(err.Error()+"\n"), 0o600)
}

// ClearModError removes a mod's load error.
func ClearModError(dir, name string) {
	if safeName(name) {
		_ = os.Remove(filepath.Join(ModsDir(dir), name, "error"))
	}
}
