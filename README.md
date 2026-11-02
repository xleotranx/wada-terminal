# Wada Terminal

A native terminal for coding agents, built with Go, [MyGo](https://mygo.egoist.dev)
and [wada-kit](https://github.com/xleotranx/wada-kit). Workspaces of sessions in
tabs and split panes; every pane is a real program on a pseudo-terminal, and the
window draws its cells itself — there is no web view and no `xterm.js`. The
programs run in a small core that outlives the window, so closing it leaves
your agents working.

![Wada Terminal's panes](docs/images/terminal.png)

Wada Terminal is development software. macOS is the tested platform.

## Run

Install the Go version in [go.mod](go.mod); the first run needs network access to
download Go modules and MyGo's native libraries.

```sh
go run .
```

`go tool mygo dev .` runs it with live reload. The app depends on the tagged
[wada-kit](https://github.com/xleotranx/wada-kit) release in `go.mod`. To change
the kit and the app together, put both checkouts beside each other and make a
local workspace, which `GOWORK=off go build ./...` then ignores:

```sh
go work init . ../wada-kit
```

Sessions start in your home folder. `claude`, `codex` and `opencode` are started
when they are installed; a pane whose agent is missing runs your login shell
instead and says so. The window starts a core on first run (`--serve-core`,
detached) and attaches to it; closing the window detaches, and reopening it
attaches to the same programs and the saved layout. `WADATERM_CORE=off` keeps
the older window-owned sessions. `WADATERM_DATA_DIR` keeps settings and the
core elsewhere, `WADATERM_CAPTURE=/path.png` writes
a screenshot and quits (`WADATERM_TAB=2` shows the second tab first,
`WADATERM_INDEX=1` opens the Sessions and `WADATERM_WORKSPACES=1` opens the
workspace menu, so a capture can show a split, the index or the menu), and
`WADATERM_APPEARANCE=light` fixes the palette.

## What it does

- **Tabs and splits** — `⌘T` opens a tab, `⌘D` splits right, `⌘⇧D` splits down,
  `⌘⇧W` closes the pane, `⌘1`–`⌘9` show a tab. Drag the bar between two panes to
  give one more room; a tab closes from the ✕ on hover or a middle click.
- **Workspaces, one column** — a workspace is a folder with its own tabs. The
  column shows the workspace you are in at its top; click it for the menu of
  every workspace, with the folder of the one on show beneath it, whether each
  other one is empty or how many tabs it holds, and whether its agents need
  you. `New workspace…` in that menu (`⌘⇧N`) picks a folder, `⌘⇧]` and `⌘⇧[`
  step between them, and the app remembers them between runs. The first run
  opens the folder the app was started in, with your home folder beside it.
  `⌘B` hides the column.
- **Tabs, one level** — under a `TABS` label, one row per tab, named after the
  pane you are in. A tab that splits shows its focused pane with the others
  nested under it behind a rail, and a dot or a word says what each program is
  doing: an agent that waits for you says `needs you`. The foot of the column
  opens a tab (`⌘T`) and the Sessions.
- **The Sessions** — `⌘⇧I`, the View menu or the row at the foot of the
  column opens an index of every session in the window, across workspaces, tabs
  and splits: its program, folder, tab and state. The session that needs you is
  chosen first — an agent waiting on an approval, then one that finished while
  you were elsewhere — and `↑` `↓` then Return gives it the keyboard; `esc`
  closes the index.
- **Scroll mode** — `⌃⇧Space` takes the keyboard from the program for the
  focused pane: `j`/`k` or the arrows move through history, `g`/`G` go to the
  ends, Page keys page, `/` opens find, and `q` or Esc returns to the live
  screen. `v` starts a character-wise visual selection (`h`/`l`, `j`/`k`,
  `w`/`b`/`e` and `0`/`$` move within it), `V` selects whole lines, `y`
  copies, `p` pastes the clipboard into the program and returns to the live
  screen, and `d` drops the selection (scrollback is read-only). A
  `SCROLL`/`VISUAL` badge marks the mode.
- **Find in scrollback** — `⌘F` opens a find bar over the focused pane.
  Search is case-insensitive over the screen and its history, matches are
  highlighted, `↑`/`↓` or Return cycle and scroll to each one, and `⌘F` closes
  it again. The query runs on the core's screen, so it sees what the program
  printed even while the window was closed.
- **State at a glance** — an agent's dot is green while it works, amber when it
  waits for an answer, blue when it finished unseen, and grey at its prompt;
  the title bar counts what needs you, and a pane's header names its state.
- **Agents report their own state** — a program that speaks OSC 7501, the
  Program Status Protocol herdr-app uses, is an agent whatever it is called.
  It sends `working`, `blocked`, `done`, `idle` or `error`, optionally with a
  kind (approval, question, login), a progress percentage, and a label and
  message for people. The window believes that instead of reading the screen;
  programs that never report still get the quiet-screen fallback. A shell that
  prints one report becomes an agent too.
- **A window an agent can drive** — `wada-terminal status`, `ls`, `commands`
  and `do` talk to the running window over a local socket: create and focus
  spaces, tabs and panes, type into a program, open the index. `do` takes
  named arguments or one JSON object, and `wada-terminal skill` prints the
  guide for an agent; `commands` lists every action without opening a window.
- **Close it, they keep working** — the programs belong to a core process, not
  the window. The core owns the pseudo-terminals and screens, answers a
  program's queries when no window is attached, and keeps the scrollback. The
  window attaches to a snapshot, streams the output, and saves each pane's
  terminal id and the core instance with its layout; a reopened window
  reattaches, while a terminal the core lost comes back as a shell in its last
  folder.
- **Agents can see the window** — `wada-terminal shot [file.png]` saves the
  window's last frame as a PNG and prints its path. It is the app's own
  rendering, at the window's pixel size, even when covered; no screen-recording
  permission is needed. Use it to check a theme, layout or key change.
- **The interface as files** — a config folder holds `AGENTS.md`,
  `settings.json` (text size, appearance, theme, layout), `keys.json` (every
  command's shortcut), `layouts/<name>.json` (a row/column/stack tree of Workspaces,
  Panes and Agents) and `themes/<name>.json` (the terminal's foreground,
  background, cursor, selection and sixteen colors). Save a file and the window
  reloads it within a second; a broken file keeps the last good version and
  writes `settings.error`, `keys.error`, `layout.error` or `theme.error` beside
  it.
- **More agents as files** — `agents.json`, or runnable entries in
  `programs.json`, add programs beyond Claude Code, Codex and OpenCode: each
  names its argv, glyph, optional `#rrggbb` color and optional SVG icon,
  appears under Session,
  and the pane is treated as an agent before it reports. Its layout entry is
  saved and restarted with the window.
- **A permanent agents panel, if the layout asks** — put an `agents` block in a
  layout and every reporting agent gets a column with its state and a click to
  focus it, beside the spaces and panes.
- **Mods, in JavaScript** — `mods/<name>/main.js` defines `render(api)` and
  `click(id, api)`; a layout's `mod` block shows the tree it returns. Mods see
  spaces, agents, focus, props, their saved state and the text of their native
  inputs, and a click can run any `do` action or type into a pane. The runtime is a small neutral API, not a
  web view: palette colors, native rows/columns/text/buttons, no imports or
  Node. A broken mod keeps the last good panel and writes
  `mods/<name>/error`.
- **Machines over SSH** — `core machine add vps --ssh vps` records a core on
  another machine; `--on vps` sends any core command there through `ssh`. Each
  core records the foreground program of its terminals, shown by `core ls` and
  in the Machines panel. A
  terminal started with `--label space=review` appears under that space in the
  Machines panel and opens as a space of that name. Each core also has a
  `layout.json` spaces document. Saving the layout publishes this window's
  spaces for each connected machine there, connecting a machine imports the
  spaces the window does not already show, and `space.export`/`space.import`
  ask for either again. The
  remote needs `wada-terminal` installed and SSH keys that do not prompt. A
  space made with `do space.new --machine vps` runs its tabs and splits on that
  machine and shows its name in the sidebar; the saved layout reattaches to
  them. A `tabs` block places the tab strip in the layout instead of the title
  bar, and a `title` block places the title-bar actions. A `machines` block
  lists each machine's running terminals and opens one
  in this window. A remote core's own saved spaces are still not merged.
- **The core from a terminal** — `wada-terminal core ls/run/send/read/wait/kill`
  starts and steers programs without the window, like herdr-core's `hc`. A
  foreground run streams output and exits with the program's code; `--bg`
  prints an address such as `t7`; a name makes a terminal easy to find; a
  program inherits `WADA_TERMINAL_ID` and `WADA_PARENT_ID`. `read` still works
  after the program ended. A background helper's exit, its `core report` and
  its blocked state arrive in its parent's input as a tagged
  `<wada from="t7 …">` note; a helper can ask with `core ask "…"` and wait, its
  parent answers with `core answer t7 --question q1 "…"`, and `core questions`
  lists open asks. `core events` streams changes and `core wait --children`
  waits for the helpers a terminal started. Templates in
  `templates/<name>.md` (a command, readiness, submit key and instructions) let
  `core run --template reviewer "Review src/x.go"` start a role and send its
  first message when it is ready.
- **Real terminals** — each pane runs a program on a pseudo-terminal, with
  `TERM=xterm-256color`, the alternate screen, 256-color and 24-bit color,
  scrollback, the cursor, and `SIGWINCH` as the pane resizes. The terminal
  answers what a program asks — its device attributes, the cursor's position,
  and the foreground and background colors of the pane (OSC 10 and 11) — so a
  program that adapts to a light or dark terminal sees this one.
- **The mouse, when a program asks** — X10, normal, button-event and any-event
  tracking (modes 9, 1000, 1002, 1003) in the SGR, urxvt and classic encodings:
  clicks, drags and the wheel go to the program. Hold Shift to select text
  instead, as other terminals allow.
- **Select and copy** — drag over cells to select them, double-click a word or
  triple-click a line, then `⌘C` (the Edit menu's Copy) or `⌘⇧C`; `⌘⇧A` selects
  the screen. `⌘V` pastes.
- **Links** — hyperlinks a program prints with OSC 8 are underlined, the pointer
  says so, and hovering shows the URL in a small bar at the pane's foot before a
  click opens it. `⌘⇧L` opens a link picker listing every URL on the pane and in
  its scrollback.
- **Art and borders that tile** — the half and quarter blocks a program draws
  its wordmarks with, and the box-drawing characters it draws borders with, are
  painted as rectangles and lines on the cell grid, not as font glyphs, so a
  logo has no seams and a `┼` joins its neighbours exactly. A Nerd Font, when
  one is installed, joins the font list for the icons programs print. The
  cursor is a two-pixel bar at the cell's edge rather than a block, which
  leaves a program's colors readable; it is painted over the character under
  it, so an agent's input caret shows even when the program puts it on a
  placeholder's first letter. A run of reversed blanks keeps its background
  too, which is how Pi draws a cursor of its own.
- **Reflow on resize** — narrowing or widening the window wraps the lines the
  program wrote again at the new width, keeping the cursor at its place in its
  line; lines a shorter window has no room for scroll into the scrollback. A
  full-screen program on the alternate screen is cropped instead, since it
  redraws.
- **Agents as first-class sessions** — Claude Code, Codex, OpenCode, a shell,
  or any program that reports over OSC 7501; the pane header shows the
  program's own window title, its reported label and message, and its working
  directory, and the Sessions follows each agent from working to waiting to
  done.
- **Font** — `⌘=` and `⌘-` change the terminal's text size; the grid is measured
  from the monospaced font, so panes stay aligned as they resize.

![The Sessions: every session in one list, with the blocked agent chosen](docs/images/sessions.png)

## Known gaps

- A selection follows the cells it covers, not the text behind them: new output
  under it moves what it holds.
- Scroll mode is a small vi subset (`j`/`k`, `g`/`G`, arrows, Page keys,
  `q`/Esc); visual selection is character- and line-wise with word and line
  motions, a paste (`p`) and clear-selection (`d`); terminal text is never
  deleted. A link opens on a click,
  not a command-click; the Links picker lists URLs but hovering does not
  preview one.
- Icons a program takes from a Nerd Font show as boxes when no such font is
  installed; the window uses one if the system has it.
- After a core restart, reboot or crash, a saved pane comes back as a shell in
  its last folder, not as the agent and its work. The core never reruns an
  agent's original command.
- The Sessions infers an agent's state from activity and from the text at
  the bottom of its screen, not from per-agent detection manifests: an
  unfamiliar approval can show as idle, and a long silent spell of work as
  done. A program that reports over OSC 7501 is believed instead. The window
  does not notify the desktop yet.
- Floats can be dragged, peek as a strip and snap to the nearest edge with
  `snap: true`; fixed layout blocks resize by dragging their divider or with
  `do layout.resize`. The mod API has no web view, and a remote core's own saved
  layout is not merged into this window, though its live terminals can be opened
  from the Machines panel; terminals started elsewhere on that core can carry
  a `space` label and are grouped under it. Layout saves publish this window's
  machine spaces, and connecting imports them; the document carries a revision
  so a stale explicit write is refused. Mods can
  animate with `api.animate` and transforms, and a runaway script is interrupted
  and shown as an error.
  [docs/herdr-app.md](docs/herdr-app.md) records the port and what comes next.

## Layout of the code

| Path | What it is |
| --- | --- |
| `main.go` | The composition: the app's window and its plugins |
| `internal/plugins/terminal` | The feature: tabs, splits, the workspace column, panes, and the input a terminal gives its program |
| `internal/plugins/terminal/index.go` | The Sessions: the index of every session, its rows, and its keys |
| `internal/plugins/terminal/protocol.go` | OSC 7501 reports as the agent states the window shows |
| `internal/plugins/terminal/control.go` | The window's control socket: status, ls, commands and do |
| `internal/plugins/terminal/remote.go` | A pane attached to a core terminal, its stream and its screen mirror |
| `internal/plugins/terminal/layoutspec.go` | The saved spaces, tabs, splits and core terminal ids |
| `internal/plugins/terminal/layoutview.go` | The layout tree: rows, columns, stacks, Workspaces, Panes, Agents and mods |
| `internal/plugins/terminal/modview.go` | Mod runtimes, their cached panels, the node-to-element renderer and clicks |
| `internal/mods` | The JavaScript mod runtime and its neutral render tree |
| `internal/control` | The control protocol, its action list, and its client |
| `internal/cli` | The app's command line, the core command line (`core.go`), `config`, `shot`, and the guide `skill` prints |
| `internal/config` | The interface files: settings, keys, layout, themes, machines, mods, their defaults and the installed `AGENTS.md` |
| `internal/plugins/terminal/config.go` | Loading those files, the reload watcher, and the last-good-on-error rules |
| `internal/core` | The persistent core: PTYs, screens, OSC 7501 state, attach snapshots, its socket and its launcher |
| `internal/appdir` | The data folder rule shared by the window, the core and the command line |
| `internal/plugins/terminal/agents.go` | Agent state — working, waiting, done, idle — and the quiet watcher that settles it |
| `internal/vt` | A VT100/xterm screen emulator: parses what a program writes and answers the cells to paint |
| `internal/pty` | A program on a pseudo-terminal: start, read, write, resize, signal |

The research and design behind the index, measured against [herdr](https://github.com/herdrdev/herdr),
is in [docs/sessions.md](docs/sessions.md). The port of the newer
desktop app — its core, status protocol, control API, and what is still to
come — is in [docs/herdr-app.md](docs/herdr-app.md).

Rendering follows the kit's rules: a frame paints cells and performs no I/O, the
program's bytes arrive through the UI thread, and a worker reads them with the
kernel's scope so closing a pane ends its work. Colors of the chrome come from
`native.PaletteFor`; the terminal's own colors come from `internal/plugins/terminal/theme.go`,
as a program chooses its own.

## Check

```sh
go test ./...
go vet ./...
go tool mygo vet .
```

The pseudo-terminal tests open `/dev/ptmx`, which a restricted sandbox may deny.
