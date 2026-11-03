// Wada Terminal is a native terminal for coding agents: workspaces of sessions
// in tabs and split panes, each a real program on a pseudo-terminal, drawn by
// MyGo on the wada-kit shell.
//
//	go run .
//
// WADATERM_DATA_DIR keeps its settings elsewhere; WADATERM_CAPTURE=/path.png
// writes a screenshot and quits, as the other variables of shell.App.Env do.
package main

import (
	"log"
	"os"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/cli"
	"wada-terminal/internal/core"
	"wada-terminal/internal/plugins/terminal"

	"github.com/xleotranx/wada-kit/plugins/inspector"
	"github.com/xleotranx/wada-kit/plugins/settings"
	"github.com/xleotranx/wada-kit/shell"
)

// app names the window, where its data lives and its menus.
var app = shell.App{
	Name: "Wada Terminal", Folder: "wada-terminal", Env: "WADATERM",
	Width: 1180, Height: 780, MinWidth: 820, MinHeight: 560,
	Menus: []string{"File", "Session", "View", "Help"},
}

func main() {
	// The window starts a core that outlives it, and a core runs headless
	// until the process is asked to stop. The hidden argument is only how a
	// window starts its core; a person runs the app without it.
	if len(os.Args) > 1 && os.Args[1] == "--serve-core" {
		dir, err := appdir.Dir(app.Folder, app.Env)
		if err != nil {
			log.Fatal(err)
		}
		if err := core.Serve(dir); err != nil {
			log.Fatal(err)
		}
		return
	}
	// core-proxy carries one core connection over standard input and output.
	// SSH runs it on another machine so the CLI can speak the same protocol.
	if len(os.Args) > 1 && os.Args[1] == "core-proxy" {
		dir, err := appdir.Dir(app.Folder, app.Env)
		if err != nil {
			log.Fatal(err)
		}
		if err := core.ProxyStdio(dir); err != nil {
			log.Fatal(err)
		}
		return
	}

	// A command inspects or drives the running window; without one, the app
	// opens. The commands never start a second window.
	if len(os.Args) > 1 && isCommand(os.Args[1]) {
		appdir.UseTerminalContext()
		os.Exit(cli.Run(app.Name, app.Folder, app.Env, os.Args[1:]))
	}
	if os.Getenv("WADATERM_CORE") != "off" {
		if dir, err := appdir.Dir(app.Folder, app.Env); err == nil {
			if err := core.Ensure(dir); err != nil {
				log.Printf("wada-terminal: panes will stay in this window: %v", err)
			}
		}
	}
	shell.Run(app,
		terminal.New,
		inspector.New,
		settings.New,
	)
}

// isCommand reports whether an argument names one of the command line's
// commands. Anything else, including the arguments a packaged app is launched
// with, opens the window as usual.
func isCommand(arg string) bool {
	switch arg {
	case "status", "ls", "do", "commands", "core", "config", "shot", "skill", "--skill", "help", "--help", "-h":
		return true
	}
	return false
}
