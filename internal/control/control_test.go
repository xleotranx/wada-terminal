package control

import (
	"path/filepath"
	"testing"
)

// TestValidate checks the argument contract a caller sees through
// `commands`: known names only, required ones present, and the right types.
func TestValidate(t *testing.T) {
	split, ok := FindAction("pane.split")
	if !ok {
		t.Fatal("pane.split is not in the action list")
	}
	if err := Validate(split, map[string]any{"direction": "right"}); err != nil {
		t.Fatalf("a valid split was refused: %v", err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"missing", map[string]any{"pane": 3}},
		{"unknown", map[string]any{"direction": "right", "sideways": true}},
		{"type", map[string]any{"direction": 3}},
	} {
		if err := Validate(split, tc.args); err == nil {
			t.Errorf("%s arguments were accepted", tc.name)
		}
	}

	send, _ := FindAction("terminal.send")
	args := map[string]any{"terminal": 8, "text": "run the tests", "submit": false}
	if err := Validate(send, args); err != nil {
		t.Fatalf("a valid send was refused: %v", err)
	}
	if !sendArgRequired(send, "text") {
		t.Error("terminal.send does not require its text")
	}
}

// sendArgRequired reports whether an action requires an argument.
func sendArgRequired(action Action, name string) bool {
	for _, arg := range action.Args {
		if arg.Name == name {
			return arg.Required
		}
	}
	return false
}

// TestActionNamesAreUnique checks the list a command client reads.
func TestActionNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, action := range Actions {
		if action.Name == "" || action.Description == "" {
			t.Errorf("an action is missing its name or description: %+v", action)
		}
		if seen[action.Name] {
			t.Errorf("%s appears twice", action.Name)
		}
		seen[action.Name] = true
	}
}

// TestSocketPath checks that the socket lives in the app's own folder.
func TestSocketPath(t *testing.T) {
	got := SocketPath("/tmp/wada")
	if want := filepath.Join("/tmp/wada", SocketName); got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
}
