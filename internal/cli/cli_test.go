package cli

import (
	"strings"
	"testing"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/control"
)

// TestParseArgsFlags checks the named form an agent uses, including the types
// a flag becomes without JSON.
func TestParseArgsFlags(t *testing.T) {
	args, err := parseArgs([]string{"--direction", "right", "--pane", "4", "--submit", "false"})
	if err != nil {
		t.Fatal(err)
	}
	if args["direction"] != "right" || args["pane"] != 4 || args["submit"] != false {
		t.Fatalf("args = %#v", args)
	}
}

// TestParseArgsJSON checks that one JSON object is taken as it is written.
func TestParseArgsJSON(t *testing.T) {
	args, err := parseArgs([]string{`{"direction":"down","pane":12,"submit":true}`})
	if err != nil {
		t.Fatal(err)
	}
	if args["direction"] != "down" || args["pane"] != float64(12) || args["submit"] != true {
		t.Fatalf("args = %#v", args)
	}
}

// TestRequest checks the command line builds the request the window expects.
func TestRequest(t *testing.T) {
	req, err := request([]string{"do", "pane.focus", "--pane", "4"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "do" || req.Action != "pane.focus" || req.Args["pane"] != 4 {
		t.Fatalf("request = %+v", req)
	}
	if _, err := request([]string{"do", "pane.explode"}); err == nil {
		t.Fatal("an unknown action was accepted before asking the window")
	}
	if _, err := request([]string{"do", "pane.focus"}); err == nil {
		t.Fatal("a missing required argument was not refused")
	}
	if _, ok := control.FindAction(req.Action); !ok {
		t.Fatal("the action list lost pane.focus")
	}
}

// TestParseAddress checks the two ways a script may name a terminal.
func TestParseAddress(t *testing.T) {
	for _, tc := range []struct {
		ref string
		id  int
		ok  bool
	}{
		{"t7", 7, true},
		{"7", 7, true},
		{" t12 ", 12, true},
		{"t0", 0, false},
		{"chat", 0, false},
		{"", 0, false},
	} {
		id, ok := parseAddress(tc.ref)
		if id != tc.id || ok != tc.ok {
			t.Errorf("parseAddress(%q) = %d, %v; want %d, %v", tc.ref, id, ok, tc.id, tc.ok)
		}
	}
}

// TestEnvSize checks that the shell's reported size is used, with a fallback.
func TestEnvSize(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "40")
	if cols, rows := envSize(); cols != 120 || rows != 40 {
		t.Fatalf("envSize() = %d, %d", cols, rows)
	}
	t.Setenv("COLUMNS", "")
	t.Setenv("LINES", "")
	if cols, rows := envSize(); cols != 80 || rows != 24 {
		t.Fatalf("envSize() = %d, %d without a size", cols, rows)
	}
}

// TestTemplateMessage checks the first message a template sends.
func TestTemplateMessage(t *testing.T) {
	template := config.Template{Body: "\nRules one.\nRules two.\n"}
	if got := templateMessage(template, "Review x"); got != "Rules one.\nRules two.\n\nReview x" {
		t.Fatalf("templateMessage() = %q", got)
	}
	if got := templateMessage(config.Template{Body: "Only body"}, ""); got != "Only body" {
		t.Fatalf("templateMessage() = %q", got)
	}
	if got := templateMessage(config.Template{}, "Only task"); got != "Only task" {
		t.Fatalf("templateMessage() = %q", got)
	}
}

// TestSubmitBytes checks the key names a template may use.
func TestSubmitBytes(t *testing.T) {
	for name, want := range map[string]string{
		"": "\r", "enter": "\r", "tab": "\t", "escape": "\x1b",
		"up": "\x1b[A", "ctrl+c": "\x03", "ctrl+d": "\x04", "custom": "custom",
	} {
		if got := submitBytes(name); got != want {
			t.Errorf("submitBytes(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestExtractOn checks where a machine flag is taken from and what it leaves
// for a program.
func TestExtractOn(t *testing.T) {
	machine, rest, err := extractOn([]string{"run", "--on", "vps", "--", "echo", "hi"})
	if err != nil || machine != "vps" || strings.Join(rest, " ") != "run -- echo hi" {
		t.Fatalf("machine = %q, rest = %v, %v", machine, rest, err)
	}
	machine, rest, err = extractOn([]string{"read", "t3", "--on", "box"})
	if err != nil || machine != "box" || strings.Join(rest, " ") != "read t3" {
		t.Fatalf("machine = %q, rest = %v, %v", machine, rest, err)
	}
	machine, rest, err = extractOn([]string{"run", "--", "echo", "--on", "vps"})
	if err != nil || machine != "" || strings.Join(rest, " ") != "run -- echo --on vps" {
		t.Fatalf("protected --on: machine = %q, rest = %v, %v", machine, rest, err)
	}
	if _, _, err := extractOn([]string{"read", "t1", "--on"}); err == nil {
		t.Fatal("--on without a machine was accepted")
	}
}

// TestQuestionTimeout checks the ask timeout's two forms.
func TestQuestionTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30": 30 * time.Second,
		"5m": 5 * time.Minute,
		"2s": 2 * time.Second,
	} {
		got, err := questionTimeout(in)
		if err != nil || got != want {
			t.Errorf("questionTimeout(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0", "-3", "soon"} {
		if _, err := questionTimeout(in); err == nil {
			t.Errorf("questionTimeout(%q) was accepted", in)
		}
	}
}
