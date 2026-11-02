package mods

import (
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// TestRenderTree checks a script's tree becomes the window's neutral nodes.
func TestRenderTree(t *testing.T) {
	runtime, err := New("hello", `
function render(api) {
  return {
    type: "column",
    padding: 12,
    gap: 6,
    children: [
      {type: "text", text: "Pane " + api.focused, font_size: 14, bold: true},
      {type: "row", gap: 4, children: [
        {type: "button", id: "new", label: "New tab", on_click: true},
        {type: "spacer"},
        {type: "divider"}
      ]}
    ]
  };
}
`, &API{Focused: func() int { return 7 }})
	if err != nil {
		t.Fatal(err)
	}
	node, err := runtime.Render()
	if err != nil {
		t.Fatal(err)
	}
	if node.Type != "column" || node.Style.Padding != 12 || len(node.Children) != 2 {
		t.Fatalf("node = %+v", node)
	}
	if got := node.Children[0].Text; got != "Pane 7" {
		t.Fatalf("text = %q", got)
	}
	button := node.Children[1].Children[0]
	if button.Type != "button" || button.ID != "new" || !button.Click || button.Text != "New tab" {
		t.Fatalf("button = %+v", button)
	}
}

// TestClickAndState checks click handlers, state export and saveState.
func TestClickAndState(t *testing.T) {
	var saved map[string]any
	runtime, err := New("counter", `
function render(api) {
  return {type: "button", id: "go", text: "Count " + (api.state.count || 0), on_click: true};
}
function click(id, api) {
  if (id === "go") { api.state.count = (api.state.count || 0) + 1; }
}
`, &API{
		State: map[string]any{"count": int64(2)},
		SaveState: func(state map[string]any) error {
			saved = state
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.Render()
	if err != nil || before.Text != "Count 2" {
		t.Fatalf("before = %+v, %v", before, err)
	}
	if err := runtime.Click("go"); err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved["count"] == nil {
		t.Fatalf("state was not saved: %+v", saved)
	}
	after, err := runtime.Render()
	if err != nil || after.Text != "Count 3" {
		t.Fatalf("after = %+v, %v", after, err)
	}
}

// TestInteractiveOnlyOnClick checks that render does not get run/send; a mod
// must not change the window while it is being drawn.
func TestInteractiveOnlyOnClick(t *testing.T) {
	runtime, err := New("quiet", `
function render(api) {
  if (typeof api.run !== "undefined") { throw new Error("run in render"); }
  return {type: "text", text: "ok"};
}
`, &API{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Render(); err != nil {
		t.Fatalf("render saw interactive functions: %v", err)
	}
}

// TestErrors checks the failures a broken mod gets.
func TestErrors(t *testing.T) {
	for _, source := range []string{
		`function click() {}`,
		`function render(api) { return {type: "wat"}; }`,
		`function render(api) { return {type: "button", on_click: true}; }`,
		`function render(api) { return {type: "row", children: "nope"}; }`,
		`function render(api) { syntax error }`,
	} {
		if _, err := New("bad", source, &API{}); err != nil {
			continue // a parse error is caught at load, which is fine
		}
		runtime, _ := New("bad", source, &API{})
		if _, err := runtime.Render(); err == nil {
			t.Errorf("source was accepted: %s", source)
		}
	}
	if _, err := New("norender", `var x = 1;`, &API{}); err == nil || !strings.Contains(err.Error(), "render") {
		t.Fatalf("a mod without render = %v", err)
	}
}

// TestConsole checks that console.log reaches the API.
func TestConsole(t *testing.T) {
	var logged []string
	runtime, err := New("log", `
console.log("hello", 3);
function render(api) { return {type: "text", text: "ok"}; }
`, &API{Log: func(line string) { logged = append(logged, line) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Render(); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || logged[0] != "hello 3" {
		t.Fatalf("log = %q", logged)
	}
}

// TestClickCanRunActions checks the click-only API reaches the window's
// action dispatcher with the arguments the script gave.
func TestClickCanRunActions(t *testing.T) {
	var ran string
	var args map[string]any
	runtime, err := New("action", `
function render(api) { return {type: "button", id: "go", text: "Go", on_click: true}; }
function click(id, api) { api.run("tab.new", {kind: "shell"}); api.send(4, "hello"); }
`, &API{
		Run: func(action string, given map[string]any) (any, error) {
			ran, args = action, given
			return map[string]any{"pane": 9}, nil
		},
		Send: func(terminal int, text string) error {
			if terminal != 4 || text != "hello" {
				t.Errorf("send(%d, %q)", terminal, text)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Click("go"); err != nil {
		t.Fatal(err)
	}
	if ran != "tab.new" || args["kind"] != "shell" {
		t.Fatalf("run(%q, %#v)", ran, args)
	}
}

// TestInputs checks input nodes and api.inputs in render and click.
func TestInputs(t *testing.T) {
	var saved map[string]any
	runtime, err := New("form", `
function render(api) {
  return {type: "column", gap: 4, children: [
    {type: "text", text: "Value: " + (api.inputs.name || "")},
    {type: "input", id: "name", value: "", placeholder: "Type name", on_submit: true}
  ]};
}
function click(id, api) {
  if (id === "name") { api.state.last = api.inputs.name; }
}
`, &API{
		State:     map[string]any{},
		SaveState: func(state map[string]any) error { saved = state; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := runtime.RenderForProps(nil, map[string]string{"name": "src/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	if node.Children[0].Text != "Value: src/x.go" {
		t.Fatalf("text = %q", node.Children[0].Text)
	}
	input := node.Children[1]
	if input.Type != "input" || input.ID != "name" || !input.Submit || input.Placeholder != "Type name" {
		t.Fatalf("input = %+v", input)
	}
	if err := runtime.ClickWith("name", nil, map[string]string{"name": "src/x.go"}); err != nil {
		t.Fatal(err)
	}
	if saved["last"] != "src/x.go" {
		t.Fatalf("saved = %#v", saved)
	}
}

// TestRunawayRender checks an infinite render is interrupted rather than
// freezing, and the runtime still works afterwards.
func TestRunawayRender(t *testing.T) {
	runtime, err := New("loop", `function render(api) { while (true) {} }`, &API{})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := runtime.Render(); err == nil {
		t.Fatal("an infinite render returned a tree")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the render took %s to interrupt", elapsed)
	}
	if _, err := runtime.vm.RunString(`function render(api) { return {type: "text", text: "ok"}; }`); err != nil {
		t.Fatal(err)
	}
	replacement, ok := goja.AssertFunction(runtime.vm.Get("render"))
	if !ok {
		t.Fatal("the replacement render is not callable")
	}
	runtime.render = replacement
	node, err := runtime.Render()
	if err != nil || node.Text != "ok" {
		t.Fatalf("after an interrupt = %+v, %v", node, err)
	}
}

// TestRunawayLoad checks a top-level loop is bounded too.
func TestRunawayLoad(t *testing.T) {
	start := time.Now()
	if _, err := New("load-loop", `while (true) {}`, &API{}); err == nil {
		t.Fatal("an infinite load succeeded")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the load took %s to interrupt", elapsed)
	}
}

// TestRunawayClick checks a click loop is bounded.
func TestRunawayClick(t *testing.T) {
	runtime, err := New("click-loop", `
function render(api) { return {type: "button", id: "go", text: "Go", on_click: true}; }
function click(id, api) { while (true) {} }
`, &API{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.clickLimit = 300 * time.Millisecond
	if err := runtime.Click("go"); err == nil {
		t.Fatal("an infinite click succeeded")
	}
}

// TestAnimate checks a render's requested pace and transform styles.
func TestAnimate(t *testing.T) {
	runtime, err := New("pulse", `
function render(api) {
  api.animate(30);
  return {type: "box", translate_x: 2, translate_y: -3, scale: 1.5, rotate: 45, opacity: 0.8,
          children: [{type: "text", text: "tick"}]};
}
`, &API{})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.FrameEvery() != 0 {
		t.Fatalf("a render before the first call asked for %s", runtime.FrameEvery())
	}
	node, err := runtime.Render()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.FrameEvery() != 30*time.Millisecond {
		t.Fatalf("FrameEvery = %s", runtime.FrameEvery())
	}
	if node.Style.TranslateX != 2 || node.Style.TranslateY != -3 ||
		node.Style.Scale != 1.5 || node.Style.Rotate != 45 || node.Style.Opacity != 0.8 {
		t.Fatalf("style = %+v", node.Style)
	}
}
