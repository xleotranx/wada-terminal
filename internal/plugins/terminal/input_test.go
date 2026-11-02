package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestEncodeKey checks the bytes a terminal receives for the keys that type no
// text, and that keys that do type text are left to InputText.
func TestEncodeKey(t *testing.T) {
	tests := []struct {
		name string
		ev   ui.InputEvent
		want string
		ok   bool
	}{
		{"enter", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEnter}, "\r", true},
		{"tab", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyTab}, "\t", true},
		{"shift-tab", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyTab, Mods: ui.Shift}, "\x1b[Z", true},
		{"escape", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}, "\x1b", true},
		{"backspace", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyBackspace}, "\x7f", true},
		{"up", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUp}, "\x1b[A", true},
		{"alt-left", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyLeft, Mods: ui.Alt}, "\x1b[1;3D", true},
		{"ctrl-up", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUp, Mods: ui.Ctrl}, "\x1b[1;5A", true},
		{"delete", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDelete}, "\x1b[3~", true},
		{"page up", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyPageUp}, "\x1b[5~", true},
		{"f1", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyF1}, "\x1bOP", true},
		{"f5", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyF5}, "\x1b[15~", true},
		{"ctrl-c", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyC, Mods: ui.Ctrl}, "\x03", true},
		{"ctrl-space", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeySpace, Mods: ui.Ctrl}, "\x00", true},
		{"cmd-c is the window's", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyC, Mods: ui.Super}, "", false},
		{"plain letter is text", ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyA}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := encodeKey(tt.ev)
			if ok != tt.ok || string(got) != tt.want {
				t.Fatalf("encodeKey() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestIsTypedByAKey checks that the text of keys already sent as escape
// sequences is not sent twice.
func TestIsTypedByAKey(t *testing.T) {
	for _, text := range []string{"\r", "\n", "\t", "\x7f"} {
		if !isTypedByAKey(text) {
			t.Errorf("%q should be typed by a key", text)
		}
	}
	for _, text := range []string{"hello", " ", "ü"} {
		if isTypedByAKey(text) {
			t.Errorf("%q should come as text", text)
		}
	}
}
