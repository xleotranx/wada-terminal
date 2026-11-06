package mods

import (
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// API is what a mod can see and do. The window provides it; the runtime
// turns it into the JavaScript object passed to render and click.
type API struct {
	Spaces  func() any
	Agents  func() any
	Focused func() int
	Props   map[string]any
	State   map[string]any
	// SaveState writes the mod's state after a click. It runs on the UI thread.
	SaveState func(map[string]any) error
	// Run and Send are available to click handlers, not to render: a render
	// happens during a frame and must not change the window.
	Run  func(action string, args map[string]any) (any, error)
	Send func(terminal int, text string) error
	Log  func(string)
}

// Runtime is one loaded main.js: the script, its render and click functions,
// and the state object it keeps between frames.
type Runtime struct {
	name   string
	vm     *goja.Runtime
	render goja.Callable
	click  goja.Callable
	api    *API
	state  *goja.Object

	// A render or click is interrupted after its limit, so a runaway script
	// shows an error instead of freezing the window. The lock and generation
	// keep a late timer from interrupting the next call.
	renderLimit time.Duration
	clickLimit  time.Duration
	frameEvery  time.Duration
	mu          sync.Mutex
	generation  int
	active      bool
}

// New loads a mod script. The script defines render(api), and may define
// click(id, api). A script with no render is an error.
func New(name, source string, api *API) (*Runtime, error) {
	r := &Runtime{name: name, vm: goja.New(), api: api,
		renderLimit: 500 * time.Millisecond, clickLimit: 2 * time.Second}
	if api == nil {
		api = &API{}
		r.api = api
	}
	if api.Log != nil {
		console := r.vm.NewObject()
		console.Set("log", func(call goja.FunctionCall) goja.Value {
			var parts []string
			for _, arg := range call.Arguments {
				parts = append(parts, arg.String())
			}
			api.Log(join(parts, " "))
			return goja.Undefined()
		})
		console.Set("error", console.Get("log"))
		r.vm.Set("console", console)
	}
	if err := r.limit(r.renderLimit, func() error {
		_, err := r.vm.RunString(source)
		return err
	}); err != nil {
		return nil, r.wrap("load", err)
	}
	render, ok := goja.AssertFunction(r.vm.Get("render"))
	if !ok {
		return nil, fmt.Errorf("mods/%s: render(api) is not defined", name)
	}
	r.render = render
	if value := r.vm.Get("click"); value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
		click, ok := goja.AssertFunction(value)
		if !ok {
			return nil, fmt.Errorf("mods/%s: click(id, api) is not a function", name)
		}
		r.click = click
	}
	r.state = r.vm.NewObject()
	for key, value := range api.State {
		_ = r.state.Set(key, r.vm.ToValue(value))
	}
	return r, nil
}

// Name is the mod's folder name.
func (r *Runtime) Name() string { return r.name }

// FrameEvery is the interval the last render asked for with api.animate(ms),
// or 0 when it is static.
func (r *Runtime) FrameEvery() time.Duration { return r.frameEvery }

// Render calls the mod and returns the tree it described.
func (r *Runtime) Render() (*Node, error) { return r.RenderFor(nil) }

// RenderFor is Render with the block's props, so one mod can show different
// things in two layout blocks.
func (r *Runtime) RenderFor(props map[string]any) (*Node, error) {
	return r.RenderForProps(props, nil)
}

// RenderForProps is RenderFor with the values of the block's inputs, so a
// script can draw what has been typed.
func (r *Runtime) RenderForProps(props map[string]any, inputs map[string]string) (node *Node, err error) {
	r.frameEvery = 0
	defer func() {
		if recovered := recover(); recovered != nil {
			node, err = nil, r.wrap("render", recovered)
		}
	}()
	var value goja.Value
	err = r.limit(r.renderLimit, func() error {
		var callErr error
		value, callErr = r.render(goja.Undefined(), r.apiObject(false, props, inputs))
		return callErr
	})
	if err != nil {
		return nil, r.wrap("render", r.explain(err))
	}
	exported := value.Export()
	nodes := 0
	return parseNode(exported, 0, &nodes)
}

// Click calls the mod's click(id, api) and saves its state. A mod without a
// click function ignores clicks.
func (r *Runtime) Click(id string) error { return r.ClickFor(id, nil) }

// ClickFor is Click with the block's props.
func (r *Runtime) ClickFor(id string, props map[string]any) error {
	return r.ClickWith(id, props, nil)
}

// ClickWith is ClickFor with the block's current input values.
func (r *Runtime) ClickWith(id string, props map[string]any, inputs map[string]string) (err error) {
	if r.click == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = r.wrap("click", recovered)
		}
	}()
	if err := r.limit(r.clickLimit, func() error {
		_, err := r.click(goja.Undefined(), r.vm.ToValue(id), r.apiObject(true, props, inputs))
		return err
	}); err != nil {
		return r.wrap("click", r.explain(err))
	}
	if r.api.SaveState != nil {
		var state map[string]any
		if err := r.vm.ExportTo(r.state, &state); err != nil {
			return r.wrap("state", err)
		}
		if err := r.api.SaveState(state); err != nil {
			return r.wrap("state", err)
		}
	}
	return nil
}

// apiObject builds the JavaScript API for one call. Only a click gets the
// functions that change the window.
func (r *Runtime) apiObject(interactive bool, props map[string]any, inputs map[string]string) *goja.Object {
	object := r.vm.NewObject()
	_ = object.Set("spaces", r.vm.ToValue(callAny(r.api.Spaces)))
	_ = object.Set("agents", r.vm.ToValue(callAny(r.api.Agents)))
	_ = object.Set("focused", r.vm.ToValue(callInt(r.api.Focused)))
	if props == nil {
		props = r.api.Props
	}
	_ = object.Set("props", r.vm.ToValue(props))
	if inputs == nil {
		inputs = map[string]string{}
	}
	_ = object.Set("inputs", r.vm.ToValue(inputs))
	_ = object.Set("state", r.state)
	_ = object.Set("now", time.Now().UnixMilli())
	_ = object.Set("animate", func(call goja.FunctionCall) goja.Value {
		ms := call.Argument(0).ToInteger()
		if ms < 16 {
			ms = 16
		}
		if ms > 1000 {
			ms = 1000
		}
		r.frameEvery = time.Duration(ms) * time.Millisecond
		return goja.Undefined()
	})
	if !interactive {
		return object
	}
	save := func(goja.FunctionCall) goja.Value {
		if r.api.SaveState == nil {
			return goja.Undefined()
		}
		var state map[string]any
		if err := r.vm.ExportTo(r.state, &state); err != nil {
			panic(r.vm.NewGoError(err))
		}
		if err := r.api.SaveState(state); err != nil {
			panic(r.vm.NewGoError(err))
		}
		return goja.Undefined()
	}
	_ = object.Set("saveState", save)
	_ = object.Set("save_state", save)
	_ = object.Set("run", func(call goja.FunctionCall) goja.Value {
		if r.api.Run == nil {
			panic(r.vm.NewGoError(fmt.Errorf("mods cannot run commands here")))
		}
		action := call.Argument(0).String()
		args := map[string]any{}
		if len(call.Arguments) > 1 {
			if err := r.vm.ExportTo(call.Argument(1), &args); err != nil {
				panic(r.vm.NewGoError(err))
			}
		}
		result, err := r.api.Run(action, args)
		if err != nil {
			panic(r.vm.NewGoError(err))
		}
		return r.vm.ToValue(result)
	})
	_ = object.Set("send", func(call goja.FunctionCall) goja.Value {
		if r.api.Send == nil {
			panic(r.vm.NewGoError(fmt.Errorf("mods cannot send here")))
		}
		terminal := int(call.Argument(0).ToInteger())
		text := call.Argument(1).String()
		if err := r.api.Send(terminal, text); err != nil {
			panic(r.vm.NewGoError(err))
		}
		return goja.Undefined()
	})
	return object
}

// limit runs fn with a watchdog that interrupts the script after limit. The
// defer clears the interrupt so the runtime can be used again.
func (r *Runtime) limit(limit time.Duration, fn func() error) error {
	r.mu.Lock()
	r.generation++
	generation := r.generation
	r.active = true
	r.mu.Unlock()
	timer := time.AfterFunc(limit, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.active && r.generation == generation {
			r.vm.Interrupt("mod timed out")
		}
	})
	defer func() {
		timer.Stop()
		r.mu.Lock()
		r.active = false
		r.generation++
		r.mu.Unlock()
		r.vm.ClearInterrupt()
	}()
	return fn()
}

// explain turns a watchdog interruption into a short message.
func (r *Runtime) explain(err error) error {
	if _, ok := err.(*goja.InterruptedError); ok {
		return fmt.Errorf("the script took longer than %s", r.renderLimit)
	}
	return err
}

// wrap names the mod and the phase in an error.
func (r *Runtime) wrap(phase string, err any) error {
	return fmt.Errorf("mods/%s %s: %v", r.name, phase, err)
}

// callAny calls a data function, or returns nil when it is unset.
func callAny(fn func() any) any {
	if fn == nil {
		return nil
	}
	return fn()
}

// callInt calls a data function, or returns 0 when it is unset.
func callInt(fn func() int) int {
	if fn == nil {
		return 0
	}
	return fn()
}

// join is a tiny strings.Join for the console callback.
func join(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}
