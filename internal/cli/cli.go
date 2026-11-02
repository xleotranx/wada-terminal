// Package cli is the app's command line: status, ls, commands and do, which
// talk to the running window over its control socket, and skill, the guide an
// agent reads. It is not a second window: without a command, main runs the
// app.
package cli

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/control"
)

// skill is the guide `wada-terminal skill` prints.
//
//go:embed skill.md
var skill string

// Run runs one CLI command and returns the process exit code. Name, folder and
// env are the app's, so the command finds the same socket the window made.
func Run(name, folder, env string, args []string) int {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "skill", "--skill":
		fmt.Print(skill)
		return 0
	case "help", "--help", "-h":
		return usage()
	case "commands":
		return printJSON(control.Actions)
	case "core":
		return RunCore(folder, env, args[1:])
	case "config":
		return configCommand(args[1:])
	case "shot":
		return shotCommand(folder, env, args[1:])
	case "status", "ls", "do":
		socket, err := socketPath(folder, env)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wada-terminal:", err)
			return 1
		}
		req, err := request(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wada-terminal:", err)
			return 2
		}
		result, err := control.Call(socket, req)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wada-terminal:", err)
			return 1
		}
		return printJSON(result)
	default:
		fmt.Fprintf(os.Stderr, "wada-terminal: no command %q\n\n", args[0])
		return usage()
	}
}

// request turns status/ls/do arguments into a control request.
func request(args []string) (control.Request, error) {
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return control.Request{}, fmt.Errorf("status takes no arguments")
		}
		return control.Request{Method: "status"}, nil
	case "ls":
		if len(args) != 1 {
			return control.Request{}, fmt.Errorf("ls takes no arguments")
		}
		return control.Request{Method: "ls"}, nil
	case "do":
		if len(args) < 2 {
			return control.Request{}, fmt.Errorf("do needs an action; run `wada-terminal commands` to list them")
		}
		action, ok := control.FindAction(args[1])
		if !ok {
			return control.Request{}, fmt.Errorf("no action %q; run `wada-terminal commands` to list them", args[1])
		}
		values, err := parseArgs(args[2:])
		if err != nil {
			return control.Request{}, err
		}
		if err := control.Validate(action, values); err != nil {
			return control.Request{}, err
		}
		return control.Request{Method: "do", Action: action.Name, Args: values}, nil
	}
	return control.Request{}, fmt.Errorf("unknown command")
}

// parseArgs reads either one JSON object or --key value pairs. A value is a
// bool, a whole number, a JSON array or object, or a string.
func parseArgs(args []string) (map[string]any, error) {
	values := map[string]any{}
	if len(args) == 1 && strings.HasPrefix(strings.TrimSpace(args[0]), "{") {
		if err := json.Unmarshal([]byte(args[0]), &values); err != nil {
			return nil, fmt.Errorf("arguments are not a JSON object: %w", err)
		}
		return values, nil
	}
	for i := 0; i < len(args); i += 2 {
		flag := args[i]
		if !strings.HasPrefix(flag, "--") || len(flag) == 2 {
			return nil, fmt.Errorf("expected --name value, not %q", flag)
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("%s needs a value", flag)
		}
		values[strings.TrimPrefix(flag, "--")] = value(args[i+1])
	}
	return values, nil
}

// value reads a command-line argument as the type it looks like, so a caller
// does not have to write JSON for a number or a bool.
func value(raw string) any {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}
	return raw
}

// socketPath is where the window with this app's data folder listens. It uses
// the same folder rule as the shell, so a command run with the same
// environment finds the same window.
func socketPath(folder, env string) (string, error) {
	dir, err := appdir.Dir(folder, env)
	if err != nil {
		return "", err
	}
	return control.SocketPath(dir), nil
}

// printJSON prints a result as indented JSON, or raw when it is not JSON.
func printJSON(v any) int {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal:", err)
		return 1
	}
	fmt.Println(string(body))
	return 0
}

// usage prints how to use the command line.
func usage() int {
	fmt.Print(`wada-terminal: a native terminal for coding agents

  wada-terminal                 open the window
  wada-terminal status          the open space, tab, pane and flags
  wada-terminal ls              every space > tab > pane, as JSON
  wada-terminal commands        every do action, as JSON
  wada-terminal do <action> ... change the window; --name value or one JSON object
  wada-terminal core ...        programs that outlive the window: ls, run, send, read, wait, kill
  wada-terminal config          the interface files: path or a list of them
  wada-terminal shot [FILE]     save a PNG of the window's last frame
  wada-terminal skill           the guide an agent reads
`)
	return 2
}
