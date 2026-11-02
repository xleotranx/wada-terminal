# AGENTS.md — wada-terminal

A native terminal for coding agents on the wada-kit shell: Go, MyGo's GPU-drawn UI,
Onevra components. macOS is the tested platform. This repository is one module,
`wada-terminal`, and one window.

## Source map

- `main.go`: the composition — `shell.Run` with the app's window and plugins.
- `internal/plugins/terminal`: the feature. `plugin.go` (manifest, commands,
  workspaces and their saved folders, tabs),
  `session.go` (a program on a pseudo-terminal, selection, links), `view.go`
  (the workspace tree, tabs, split layout), `pane.go` (a pane's chrome and cells),
  `input.go` (keys, text, paste, scroll, pointer and drags), `mouse.go` (the
  reports a program reads), `blocks.go`, `boxdraw.go` and `boxpaint.go` (the
  characters painted as rectangles and lines), `theme.go` (the terminal's
  colors), `split.go` (the layout tree), `agents.go` (the Sessions's agent
  state and quiet watcher), `protocol.go` (OSC 7501 as agent state),
  `index.go` (the Sessions overlay), `control.go` (the local control
  socket), `remote.go` (a pane attached to a core terminal, its stream and
  screen mirror), `layoutspec.go` (saved spaces, tabs, splits and terminal
  ids), `layoutview.go` (the row/column layout tree and the Agents panel),
  `modview.go` (loading mod panels and drawing their trees), `layoutactions.go`
  (switching a stack), `machines.go`
  (connecting machine cores, recording their instances and the Machines
  panel), `config.go`
  (loading the interface files and reloading them on save).
  `internal/control` is the window protocol and its action list;
  `internal/cli` is the command line; `internal/core` is the persistent
  terminal server and client (including the command transport and SSH proxy);
  `internal/config` is the interface as files, including machines;
  `internal/mods` is the JavaScript mod runtime; `internal/appdir` is the data
  folder rule.
  `docs/sessions.md` and `docs/herdr-app.md` record the design and how it
  maps onto herdr.
- `internal/vt`: the screen emulator. Parses bytes into cells, keeps the marks
  of the lines that wrapped, and re-wraps them on a resize (`reflow.go`); no
  UI, no I/O.
- `internal/pty`: a program on a pseudo-terminal. Starts no goroutine.

## Rules

1. Plugin code registers through `kernel.Context`; it never imports `shell` or
   another plugin, only `contract/*`. Feature policy stays in the app.
2. Plugin state belongs to the UI thread. A program's bytes arrive through a
   scope post (`session.push`), and workers come from `Scope.Spawn` or `kernel.Go`.
   No bare `go` statement in a plugin.
3. A frame draws: it performs no I/O and mutates no kernel state. The one
   exception, documented in `pane.go`, is sizing a session to its pane, which
   resizes an in-memory screen and sends `SIGWINCH` to the program on the widget's
   behalf. A program's queries are answered where its bytes are applied
   (`session.feed`), not in a frame.
4. The terminal owns the selection and the split ratios; a drag updates them
   from the widget's input handler, and the next frame draws them.
5. Every chrome color comes from `native.PaletteFor`. The terminal's own colors
   (the surface, the sixteen ANSI hues, the selection) live in `theme.go`,
   because a program chooses its own colors.
6. Elements and `*ui.Context` never outlive a build pass; persistent identity is
   a `ui.Handle` (a pane's body, for its committed size and its focus).
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
`/dev/ptmx`, which a restricted sandbox may deny; run them outside it.

Docs describe the code as it is: update `README.md` when a feature or a shortcut
changes.
