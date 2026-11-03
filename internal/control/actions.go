package control

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Arg is one named argument of an action. Its Type is "string", "int" or
// "bool"; its Default is what the window uses when it is omitted.
type Arg struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
	Default     any    `json:"default,omitempty"`
}

// Action is one thing `do` can make the window do.
type Action struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Args        []Arg  `json:"args,omitempty"`
}

// Actions is every action the window accepts, in the order the command list
// prints them. A mod or a new feature adds one here and a case in the window.
var Actions = []Action{
	{Name: "task.finish", Description: "Open the Finish task confirmation; only the person can confirm saving or discarding and cleanup."},
	{Name: "task.new", Description: "Create a local isolated task asynchronously. Inspect status/ls for the resulting space; creation errors open the task dialog.", Args: []Arg{
		{Name: "name", Type: "string", Required: true, Description: "Task name."},
		{Name: "brief", Type: "string", Description: "Optional task context and acceptance criteria."},
		{Name: "folder", Type: "string", Required: true, Description: "Local Git repository."},
		{Name: "agent", Type: "string", Description: "shell (default), claude, codex, opencode or custom:ID."}}},
	{Name: "task.review", Description: "Show and refresh the current task's changes."},
	{Name: "task.check", Description: "Run an explicit command in the current task asynchronously and retain its result.", Args: []Arg{{Name: "command", Type: "string", Required: true, Description: "Shell command authored by the user."}}},
	{Name: "layout.resize", Description: "Set the pixel size of a fixed-size layout block.",
		Args: []Arg{
			{Name: "id", Type: "string", Description: "The block's id."},
			{Name: "px", Type: "int", Description: "Its new size along the parent's direction."},
		}},
	{Name: "layout.stack", Description: "Show one child of a layout stack.",
		Args: []Arg{
			{Name: "stack", Type: "string", Description: "The stack's id in the layout."},
			{Name: "active", Type: "string", Description: "The child's id to show."},
		}},
	{Name: "layout.stack.next", Description: "Show the next child of a layout stack.",
		Args: []Arg{
			{Name: "stack", Type: "string", Description: "The stack's id in the layout."},
			{Name: "step", Type: "int", Description: "1 forward, -1 back; default 1."},
		}},
	{Name: "space.new", Description: "Make a space: a folder with its own tabs.",
		Args: []Arg{
			{Name: "folder", Type: "string", Description: "Where its terminals start; default the focused pane's folder."},
			{Name: "name", Type: "string", Description: "Its label; default the folder's name."},
			{Name: "machine", Type: "string", Description: "Run its terminals on this machine from machines.json; default this computer."},
		}},
	{Name: "space.close", Description: "Close a space and end its terminals. Tasks open the Finish task confirmation.",
		Args: []Arg{{Name: "id", Type: "int", Description: "The space; default the one on show."}}},
	{Name: "space.select", Description: "Show a space.",
		Args: []Arg{
			{Name: "index", Type: "int", Description: "Its position, 1 = the first."},
			{Name: "id", Type: "int", Description: "Its id, as ls prints it."},
		}},
	{Name: "space.export", Description: "Save this window's spaces for a machine into its core's layout document.",
		Args: []Arg{{Name: "machine", Type: "string", Description: "The machine; default this computer."}}},
	{Name: "space.import", Description: "Make the spaces from a core's layout document that this window does not show.",
		Args: []Arg{{Name: "machine", Type: "string", Description: "The machine; default this computer."}}},
	{Name: "space.next", Description: "Show the next space."},
	{Name: "space.prev", Description: "Show the previous space."},
	{Name: "tab.new", Description: "Make a tab in the shown space.",
		Args: []Arg{
			{Name: "kind", Type: "string", Description: "claude, codex, opencode or shell; default shell."},
			{Name: "folder", Type: "string", Description: "Where the session starts; default the focused pane's folder."},
		}},
	{Name: "tab.close", Description: "Close a tab and end its sessions.",
		Args: []Arg{{Name: "index", Type: "int", Description: "Its position, 1 = the first; default the shown one."}}},
	{Name: "tab.select", Description: "Show a tab.",
		Args: []Arg{{Name: "index", Type: "int", Required: true, Description: "Its position, 1 = the first."}}},
	{Name: "tab.next", Description: "Show the next tab."},
	{Name: "tab.prev", Description: "Show the previous tab."},
	{Name: "pane.split", Description: "Split a pane with another session.",
		Args: []Arg{
			{Name: "direction", Type: "string", Required: true, Description: "right or down."},
			{Name: "pane", Type: "int", Description: "The pane to split; default the focused one."},
			{Name: "kind", Type: "string", Description: "claude, codex, opencode or shell; default shell."},
		}},
	{Name: "pane.close", Description: "Close a pane and end its session.",
		Args: []Arg{{Name: "pane", Type: "int", Description: "Its id; default the focused pane."}}},
	{Name: "pane.focus", Description: "Give a pane the keyboard.",
		Args: []Arg{{Name: "pane", Type: "int", Required: true, Description: "Its id, as ls prints it."}}},
	{Name: "terminal.send", Description: "Type text into a pane's program.",
		Args: []Arg{
			{Name: "terminal", Type: "int", Required: true, Description: "The pane's id."},
			{Name: "text", Type: "string", Required: true, Description: "The text; no keys or escapes, send those with the program's own protocol."},
			{Name: "submit", Type: "bool", Description: "Press Return after the text; default true.", Default: true},
		}},
	{Name: "index.open", Description: "Open the Sessions."},
	{Name: "index.close", Description: "Close the Sessions."},
}

// FindAction returns the action with a name.
func FindAction(name string) (Action, bool) {
	for _, action := range Actions {
		if action.Name == name {
			return action, true
		}
	}
	return Action{}, false
}

// Validate checks an action's arguments: known names only, the required ones
// present, and each value of the type its action declares.
func Validate(action Action, args map[string]any) error {
	known := make(map[string]Arg, len(action.Args))
	for _, arg := range action.Args {
		known[arg.Name] = arg
	}
	var unknown []string
	for name := range args {
		if _, ok := known[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%s does not take %v", action.Name, unknown)
	}
	for _, arg := range action.Args {
		value, ok := args[arg.Name]
		if !ok || value == nil {
			if arg.Required {
				return fmt.Errorf("%s needs --%s", action.Name, arg.Name)
			}
			continue
		}
		switch arg.Type {
		case "string":
			if _, ok := String(args, arg.Name); !ok {
				return fmt.Errorf("--%s is a string", arg.Name)
			}
		case "int":
			if _, ok := Int(args, arg.Name); !ok {
				return fmt.Errorf("--%s is a number", arg.Name)
			}
		case "bool":
			if _, ok := Bool(args, arg.Name); !ok {
				return fmt.Errorf("--%s is true or false", arg.Name)
			}
		}
	}
	return nil
}

// String reads a string argument.
func String(args map[string]any, name string) (string, bool) {
	value, ok := args[name]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

// Int reads an integer argument. JSON numbers arrive as float64, and the
// command line may hand over an int directly.
func Int(args map[string]any, name string) (int, bool) {
	value, ok := args[name]
	if !ok {
		return 0, false
	}
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

// Bool reads a boolean argument; an absent one is true, the default for the
// actions that take one.
func Bool(args map[string]any, name string) (bool, bool) {
	value, ok := args[name]
	if !ok {
		return true, false
	}
	b, ok := value.(bool)
	return b, ok
}
