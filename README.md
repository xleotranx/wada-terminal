<p align="center"><img src="docs/images/icon.png" width="112" alt="Wada Terminal's icon"></p>

# Wada Terminal

A native terminal for coding agents, built with Go, [MyGo](https://mygo.egoist.dev)
and [wada-kit](https://github.com/xleotranx/wada-kit). Workspaces of sessions in
tabs and split panes; every pane is a real program on a pseudo-terminal, and the
window draws its cells itself — there is no web view and no `xterm.js`. The
programs run in a small core that outlives the window, so closing it leaves
your agents working.

![Wada Terminal's panes](docs/images/terminal.png)

Wada Terminal is development software. macOS is the tested platform; another
Unix should build but is untested.

## Run

There are no release builds yet; build from a checkout. Requirements:

- the Go version in [go.mod](go.mod), which also provides the `go tool` MyGo
  runs;
- network access on the first build, for Go modules and MyGo's libghostty-vt,
  which MyGo then caches;
- `git`, for the task workspaces.

From a checkout:

```sh
make run
```

Panes inherit your `PATH`. To run the `wada-terminal` CLI inside a pane, build
it once and add it:

```sh
make build-dev
export PATH="$PWD/dist/dev:$PATH"
```

`make run` and `go run .` run the development app; `go tool mygo dev .` adds
live reload. `make build-dev` writes a development executable to
`dist/dev/wada-terminal`, and `make build` writes a production executable to
`dist/prod/wada-terminal`. `go tool mygo build` packages a production app in
`dist`; `go tool mygo dev .` keeps its separate development app in `.mygo/dev`.

Development and production use independent data, settings, tasks, sockets and
cores. On macOS their default folders are
`~/Library/Application Support/wada-terminal-dev` and
`~/Library/Application Support/wada-terminal`, respectively. Existing data
stays in its current folder. The `wada-terminal` CLI run inside a pane targets
that pane's environment, while launching another window chooses its own default.
Use the current `wada-terminal` CLI in the pane; older CLI builds do not know
how to select the pane's environment.
`WADATERM_DATA_DIR` explicitly selects a data folder for either environment;
using the same override for both makes them share it. `WADATERM_CONFIG_DIR`
separately overrides the interface files. `MYGO_ENV=production` makes an
ordinary Go development executable use production defaults; packaged production
builds always use production defaults.

The app depends on the tagged
[wada-kit](https://github.com/xleotranx/wada-kit) release in `go.mod`. To change
the kit and the app together, put both checkouts beside each other and make a
local workspace, which `GOWORK=off go build ./...` then ignores:

```sh
go work init . ../wada-kit
```

`go tool mygo build` packages the app with the icon in `resources/icon.png`; a
development build takes `resources/icon-dev.png`. Both are rendered from the
sources in [design](design/).

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
- **Terminals, one list** — the sidebar lists every terminal in screen order.
  Split peers have equal rows under a neutral `Split …` header with a pane count;
  focusing another pane keeps the header and order stable. Arrangement names are
  saved with the layout. The horizontal tab strip is hidden while the workspace
  sidebar is visible, and returns when `⌘B` hides it (or a custom layout omits
  the workspace column). Explicit `tabs` layout blocks remain available.
  `New terminal` (`⌘T`) opens a tab; `All terminals` opens the Sessions.
- **The Sessions** — `⌘⇧I`, the View menu or the row at the foot of the
  column opens an index of every session in the window, across workspaces, tabs
  and splits: its program, folder, tab and state. The session that needs you is
  chosen first — an agent waiting on an approval, then one that finished while
  you were elsewhere — and `↑` `↓` then Return gives it the keyboard; `esc`
  closes the index.
- **Find in scrollback** — `⌘F` opens a find bar over the focused pane.
  Search is case-insensitive for ASCII letters, over the screen and its
  history; matches are highlighted as they are found, Return or `↑` selects the
  next one up the scrollback and `↓` the next one down, each scrolled into
  view, and `⌘F` closes it again. The pane's terminal holds what the core's
  holds, so it sees what the program printed while the window was closed.
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
  program's queries when no window is attached, and keeps the scrollback. A
  window's pane shows frames of the core's terminal, cell for cell, and never
  lays out the program's output itself; it saves each pane's
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
- **Real terminals** — each pane runs a program on a pseudo-terminal and shows
  it with [MyGo's terminal](https://github.com/egoist/mygo), whose emulator is
  Ghostty's (libghostty-vt): `TERM=xterm-256color`, 256-color and 24-bit
  color, the alternate screen, Unicode with wide characters and emoji,
  synchronized output, the Kitty keyboard protocol, input methods, and a
  scrollback that reflows as the pane resizes. It answers what a program asks
  — its device attributes, the cursor's position, and the pane's colors (OSC
  4, 10, 11) — so a program that adapts to a light or dark terminal sees this
  one. Box drawing, blocks and Powerline's separators are drawn rather than
  taken from the font, so borders join; a Nerd Font, when one is installed,
  joins the font list for the icons programs print.
- **The mouse, when a program asks** — clicks, drags and the wheel go to a
  program that takes the mouse; hold Shift to select text instead.
- **Select and copy** — drag over cells to select them, double-click a word or
  triple-click a line, then `⌘C`; `⌘A` selects everything. `⌘V` pastes, and
  the context menu has Copy, Paste, Select All and Clear. The wheel and
  `⇧PageUp`/`⇧PageDown` scroll the scrollback; typing returns to the bottom.
- **Links** — `⌘`-click opens a hyperlink a program printed (OSC 8) or a URL
  printed as text. `⌘⇧L` opens a link picker listing the hyperlinks the
  program printed and the URLs on the pane and in its scrollback.
- **Agents as first-class sessions** — Claude Code, Codex, OpenCode, a shell,
  or any program that reports over OSC 7501; the pane header shows the
  program's own window title, its reported label and message, and its working
  directory, and the Sessions follows each agent from working to waiting to
  done.
- **Font** — `⌘=` and `⌘-` change every pane's text size, as does the wheel
  with `⌘` held over a pane; `⌘0` sets it back.

![The Sessions: every session in one list, with the blocked agent chosen](docs/images/sessions.png)

## Known gaps

- There is no keyboard mode for moving through the scrollback and selecting
  text: the pointer, the wheel and the find bar do it.
- The link picker's hyperlinks are those the program printed since its pane's
  terminal started, up to the last 200, whether or not they are still on
  screen; hovering a link does not preview it.
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
| `internal/core` | The persistent core: PTYs, each program's headless MyGo terminal, OSC 7501 state, the frames a window attaches to, its socket and its launcher |
| `internal/appdir` | The data folder rule shared by the window, the core and the command line |
| `internal/plugins/terminal/agents.go` | Agent state — working, waiting, done, idle — and the quiet watcher that settles it |
| `internal/status` | Reads OSC 7501 reports, bracketed paste and hyperlink targets out of a program's output, which the emulator does not report |
| `internal/pty` | A program on a pseudo-terminal: start, read, write, resize, signal |

The research and design behind the index, measured against [herdr](https://github.com/herdrdev/herdr),
is in [docs/sessions.md](docs/sessions.md).

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

## Task workspaces

`New task…` in the sidebar or File menu (`⌘⇧T`) adds a named task to the
queue without creating a worktree. Choose a local repository and a built-in
agent, a program from `agents.json` or `programs.json`, or a shell. Select the
queued task and click `Start task` to create a new branch and worktree from
the then-current committed HEAD under the data folder's `tasks/` directory.
Uncommitted source changes are left in their original checkout. A repository needs a commit;
remote task creation is not supported yet.

Add an optional brief with context, constraints, and acceptance criteria when
creating a task; `Task brief…` lets you review or edit it later. Open **Tasks**
from the sidebar or View menu (`⌘⇧J`) to see and search tasks across repositories.
The view groups them by **Needs you**, **Ready to review**, **Working**, **Queued**,
**Open**, and **Finished**. Each action opens the appropriate task or terminal;
`Start…` opens the queued task so you can choose a program before starting.
`Back to workspace` returns to the current workspace. Their terminals,
splits, task name, branch, starting commit and last check survive reopening the
window. The brief is saved with the task. `Terminal` returns to the agent;
`Send task` submits the task name and brief as a prompt when the focused agent is
ready. The task view identifies the started program and selected terminal,
shows an explicit agent status report when available, and otherwise says
readiness is unknown. Bracketed paste does not prove an agent is ready. After a
successful input write, Wada records when and where it submitted the task to the
terminal transport; this does not confirm the agent received or accepted it.
The receipt is kept with the attempt, and changes to the brief after submission
are shown. `Send task again` explicitly resubmits the current task. Briefs
require the agent to enable bracketed paste, so their line breaks cannot submit
partial prompts. This action is disabled for ordinary shells.
Agent startup uses the agent's existing authentication and configuration.

`Changes` reviews the task against its starting commit, including later commits,
staged and unstaged edits and untracked text files. It refreshes every two seconds
while visible. Reviews and command output have bounded previews; large files are identified; tracked binary changes use Git’s binary-patch
representation and untracked binaries are labelled. `Refresh changes` also works
on demand. Task status uses explicit program reports: **Working**, **Needs you**
or **Ready to review**; without a report it says **Open**, never guessed success.
`Finish task…` opens the completion and cleanup dialog.

Enter an explicit shell command such as `go test ./...` and click `Run check`.
The command runs in the task worktree, with a two-minute limit, and the receipt
shows its command, timestamp, actual exit code and output. The default is
`Not run`. A changed review snapshot marks an old check as unverified. Checks do
not automatically merge, commit, approve agent commands, or declare a task done.
`Finish task…` saves ordinary changed files as a commit on the task branch,
stops its terminals, and removes its temporary folder. The finished task remains
in Tasks with its brief, branch, and last check receipt. If the branch is
retained, `Review saved changes` shows its committed diff against the starting
commit. The receipt predates cleanup and is not proof that the retained branch
was checked. Removing the task record does not remove a retained branch.

If you need another approach, select a program and choose `Start another attempt`.
Wada creates a fresh worktree from the original checkout's current committed
HEAD, then keeps the earlier attempt, its brief, branch, check receipt, and
integration receipt under `ATTEMPTS`. Select an earlier attempt to review or
integrate its retained branch. The task brief can be edited for the next attempt;
the earlier attempt keeps the brief it started with. Attempts start one at a
time, and the current workspace must be finished before starting another.
If there is nothing to save, use `Finish task`. You can explicitly choose to discard
unsaved changes instead. Ignored local files are listed and need a separate
acknowledgment because Git does not save them. Unmerged commits keep their branch
for recovery; branches already merged into the original checkout are removed.
The original project is unchanged by finishing. To apply a retained branch,
choose `Integrate…` on the finished task. The preview shows the original
checkout, target branch and exact commits; confirming fast-forwards the target
and keeps the task branch. Wada requires a clean checkout and rechecks the
preview before applying it. Ignored files are protected from overwrite. If
the branches have diverged, merge manually and resolve any conflicts. The
integration receipt records what was applied; it is not a live branch status.
Cancel leaves files and terminals alone. A failed save or cleanup retains the task
and explains the problem. Closing a task from its menu opens the same finish
dialog. Closing only the window keeps core-owned terminals running.
After a lost core, restored panes are fresh shells, as with ordinary workspaces.

The window API also accepts asynchronous task actions:

```sh
wada-terminal do task.new --name "Fix login redirect" --brief "Keep sessions and add a regression test" --folder /path/to/repo --agent opencode
wada-terminal ls
wada-terminal do task.review
wada-terminal do task.finish  # opens confirmation in the app; does not delete by itself
wada-terminal do task.check --command "go test ./..."
```

`task.new` returns `started`, not a completed task. Inspect `status` or `ls` for
the new space's `task` metadata; creation failures open the task dialog with the
error. Select the intended task space before review/check actions. `task.check`
retains its result in that metadata when finished.

## Contributing

Bug reports and focused pull requests are welcome; see
[CONTRIBUTING.md](CONTRIBUTING.md). The rules the code follows are in
[AGENTS.md](AGENTS.md), which is also the guide for agents editing it.

## License

MIT — see [LICENSE](LICENSE).
