# AGENTS.md — wada-terminal

A native terminal for coding agents on the wada-kit shell: Go, MyGo's GPU-drawn UI,
Onevra components. macOS is the tested platform. This repository is one module,
`wada-terminal`, and one window.

## Source map

- `main.go`: the composition — `shell.Run` with the app's window and plugins.
- `internal/plugins/terminal`: the feature. `plugin.go` (manifest, commands,
  workspaces and their saved folders, tabs),
  `session.go` (a program and the terminal that shows it: MyGo's
  `plugins/terminal`, Ghostty's emulator, which draws, selects, scrolls,
  searches and takes the keyboard), `view.go`
  (the workspace tree, tabs, split layout), `pane.go` (a pane's chrome, its
  terminal view and who holds the keyboard), `search.go` and `linkdialog.go`
  (the find bar and the link picker over a pane), `theme.go` (the terminals'
  colors and the chrome's), `split.go` (the layout tree), `agents.go` (the Sessions's agent
  state and quiet watcher), `protocol.go` (OSC 7501 as agent state),
  `index.go` (the Sessions overlay), `control.go` (the local control
  socket), `remote.go` (a pane attached to a core terminal: its terminal
  shows the core's frames), `layoutspec.go` (saved spaces, tabs, splits and terminal
  ids), `layoutview.go` (the row/column layout tree and the Agents panel),
  `modview.go` (loading mod panels and drawing their trees), `layoutactions.go`
  (switching a stack), `machines.go`
  (connecting machine cores, recording their instances and the Machines
  panel), `config.go`
  (loading the interface files and reloading them on save).
  `internal/control` is the window protocol and its action list;
  `internal/cli` is the command line; `internal/core` is the persistent
  terminal server and client (including the command transport and SSH proxy):
  each program's terminal is a headless MyGo terminal, and a window gets
  frames of it (`terminal.Mirror`) on its attach stream;
  `internal/config` is the interface as files, including machines;
  `internal/mods` is the JavaScript mod runtime; `internal/appdir` is the data
  folder rule.
  `docs/sessions.md` and `docs/herdr-app.md` record the design and how it
  maps onto herdr.
- `internal/status`: reads what the emulator does not report out of a
  program's output: OSC 7501 status reports and queries, bracketed paste
  mode, and hyperlink targets. No screen, no I/O.
- `internal/pty`: a program on a pseudo-terminal. Starts no goroutine.
- `design/`: the app icon's SVG sources; `resources/icon.png` and
  `resources/icon-dev.png` are the packaged renders MyGo picks up.

## Rules

1. Plugin code registers through `kernel.Context`; it never imports `shell` or
   another plugin, only `contract/*`. Feature policy stays in the app.
2. Plugin state belongs to the UI thread. A terminal's callbacks and its
   reader (`localConn`, `core.Stream`) reach a session through its scope's
   posts, and workers come from `Scope.Spawn` or `kernel.Go`. No bare `go`
   statement in a plugin.
3. A frame draws: it performs no I/O and mutates no kernel state. MyGo's
   terminal view sizes its terminal to the pane, and the terminal tells the
   program (the pty, or the core over the stream). The emulator answers a
   program's queries; `internal/status` answers OSC 7501's, where the bytes
   are read.
4. The terminal view owns the selection, the scrollback and the search; the
   plugin owns the split ratios, which a drag updates from its input handler.
   A pane asks for the keyboard once, as it becomes the focused one or an
   overlay closes (`pane.go`), never every frame.
5. Every chrome color comes from `native.PaletteFor`. The terminal's own colors
   (the surface, the sixteen ANSI hues, the selection) live in `theme.go`,
   because a program chooses its own colors.
6. Elements and `*ui.Context` never outlive a build pass; a pane's terminal
   keeps its own view state from frame to frame.
7. Keep the public surface small: `terminal.New` is the only export the app's
   `main` needs.

## Verification

```sh
go test ./...
go vet ./...
go tool mygo vet .
WADATERM_CAPTURE=/tmp/wada.png WADATERM_CAPTURE_WAIT=6s go run .
```

The capture writes a screenshot of the window after startup and quits; use a
temporary `WADATERM_DATA_DIR`, never developer data. The pseudo-terminal tests need
`/dev/ptmx`, which a restricted sandbox may deny; run them outside it. MyGo's
terminal loads Ghostty's libghostty-vt at run time: `go run` and `go test`
download it once into the user's cache, and `mygo build` puts it in the app.

Docs describe the code as it is: update `README.md` when a feature or a shortcut
changes.
