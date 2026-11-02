package terminal

import (
	"testing"

	"wada-terminal/internal/config"

	"github.com/egoist/mygo/ui"
)

// TestNormalizeKeys checks keys.json's names and its errors.
func TestNormalizeKeys(t *testing.T) {
	plug := &Plugin{}
	specs := plug.commandSpecs()
	overrides, err := normalizeKeys(config.Keys{
		"terminal.new-tab": {"ctrl+t"},
		"index":            {"cmd+shift+i"},
		"copy":             {},
	}, specs)
	if err != nil {
		t.Fatal(err)
	}
	if got := overrides["new-tab"]; len(got) != 1 || got[0] != "ctrl+t" {
		t.Fatalf("new-tab = %v", got)
	}
	if got := overrides["copy"]; got == nil || len(got) != 0 {
		t.Fatalf("copy = %v, want an empty binding", got)
	}
	if _, err := normalizeKeys(config.Keys{"new-tab": {" "}}, specs); err == nil {
		t.Fatal("a blank key was accepted")
	}
	if _, err := normalizeKeys(config.Keys{"explode": {"cmd+e"}}, specs); err == nil {
		t.Fatal("an unknown command was accepted")
	}
	if _, err := normalizeKeys(config.Keys{"new-tab": {"cmd+t"}, "terminal.new-tab": {"ctrl+t"}}, specs); err == nil {
		t.Fatal("the same command under two names was accepted")
	}
}

// TestSurfaceFromTheme checks the theme file becomes the cells' palette,
// including a link color taken from the ANSI blue.
func TestSurfaceFromTheme(t *testing.T) {
	theme := &config.Theme{
		Background: "#101010", Foreground: "#f0f0f0", Selection: "#303030", Cursor: "#ff0000",
		ANSI: []string{
			"#000000", "#aa0000", "#00aa00", "#aaaa00", "#0000aa", "#aa00aa", "#00aaaa", "#aaaaaa",
			"#555555", "#ff5555", "#55ff55", "#ffff55", "#5555ff", "#ff55ff", "#55ffff", "#ffffff",
		},
	}
	sf := surfaceFromTheme(theme)
	if sf.bg != ui.Hex("#101010") || sf.fg != ui.Hex("#f0f0f0") {
		t.Fatalf("surface = %+v", sf)
	}
	if sf.cursor != ui.Hex("#ff0000") || sf.selBg != ui.Hex("#303030") {
		t.Fatalf("cursor/selection = %+v", sf)
	}
	if sf.ansi[4] != ui.Hex("#0000aa") || sf.link != sf.ansi[4] {
		t.Fatalf("ansi/link = %+v", sf)
	}
	// A theme without a selection gets one that shows on either surface.
	light := &config.Theme{Background: "#ffffff", Foreground: "#111111",
		ANSI: make([]string, 16)}
	for i := range light.ANSI {
		light.ANSI[i] = "#000000"
	}
	sf = surfaceFromTheme(light)
	if sf.selBg == ui.Transparent {
		t.Fatal("a light theme has no selection color")
	}
}
