# wada-terminal: drive the person's window

wada-terminal runs coding agents and shells in tabs and split panes. Each pane
is a real program on a pseudo-terminal. The programs belong to a small core
process, not the window: closing the window detaches and leaves them running,
and the next window reattaches to the saved layout.

Run these commands; they talk to the running window, not a new one. Do not
start a second window to inspect the first.

```sh
wada-terminal status     # the open space, tab, pane, flags and attention count
wada-terminal ls         # every space > tab > pane, with ids, folders and states
wada-terminal commands   # every action and its arguments
```

## Change what the person sees

Create things only when asked, with an explicit name or folder. Take ids from
`ls`; do not act on the focused pane by guesswork.

```sh
wada-terminal do space.new --folder /path/to/repo --name api
wada-terminal do tab.new --kind claude --folder /path/to/repo
wada-terminal do pane.split --direction right --kind shell --pane 4
wada-terminal do pane.focus --pane 4
wada-terminal do terminal.send --terminal 4 --text "run the tests"
wada-terminal do terminal.read --pane 4 --lines 40
```

`do` takes named arguments or one JSON object:

```sh
wada-terminal do pane.split '{"direction":"right","pane":4}'
```

It prints the created or changed ids as JSON, or an error and exits 1. Check
the result with `ls` or `status`.

`terminal.send` takes its target as `--terminal`, the pane's id from `ls` (not
a `t7` from `core ls`). The target is required and need not have the focus; an
id no pane has is an error, and the text never goes to the focused pane instead.

## Programs outside the window

The core keeps running without a window, and its own command line can start,
read, type into, wait for and stop programs. A terminal is named `t7` or by the
`--name` it was started with. This is the herdr-core (`hc`) idea, smaller.

```sh
wada-terminal core ls
wada-terminal core templates
wada-terminal core layout get
wada-terminal core layout set spaces.json
wada-terminal core run --bg --template reviewer "Review src/x.go"
wada-terminal core run --bg --name build -- make test
wada-terminal core --on vps ls
wada-terminal core read build --lines 100
wada-terminal core send build "try the other test"
wada-terminal core wait build --timeout 120
wada-terminal core kill build
```

A foreground `core run` streams the program and exits with its code. A `--bg`
run prints the address and returns. Inside a core terminal, `$WADA_TERMINAL_ID`
is your address and `$WADA_PARENT_ID` is the terminal that started you.

When you start a helper with `--bg`, its exit is typed into your input as a
note tagged with its address:

```text
<wada from="t7 reviewer" exit="0">finished</wada>
```

A helper can also send you its own report, which arrives the same way:

```sh
wada-terminal core report "reviewed src/x.go; one bug, details in /tmp/findings.md"
```

Wait for the helpers you started without polling:

```sh
wada-terminal core wait --children --timeout 300
wada-terminal core wait --children --any --timeout 120
wada-terminal core events --children
```

`wait --children` returns when none of your children is working or blocked, or
all plain programs have ended. `--any` returns on the next piece of news: a
child ended, changed state, or is already blocked. `core events` prints the
core's created, changed and exit events as JSON lines. A child that reports
`blocked` or `error` and asked for notices also types one into your input.

A child can ask you a question and block:

```sh
wada-terminal core questions
wada-terminal core answer t7 --question q1 "src/x.go"
```

The child runs `core ask "…"`; the answer is its stdout. The question also
arrives in your input tagged `<wada from="t7 …" question="q1">`. Answer the one
the person would answer; do not invent an answer to a question about intent.

## See the window

`wada-terminal shot [file.png]` saves the window's last frame and prints its
absolute path. It captures the app, not the desktop, at the window's pixel
size, even when the window is covered; no screen-recording permission is
needed. Use it after changing a theme, keys or layout, and look at the picture
before calling the change done. It needs a running window that has drawn one
frame.

```sh
wada-terminal shot /tmp/wada.png
```

## Customize the window

The window is also files: settings, keys, a layout and themes. Find the folder
with `wada-terminal config`; read `AGENTS.md` in it. They apply on save, and a
broken file keeps the last good version and writes `settings.error` or
`theme.error` beside it.

```sh
DIR=$(wada-terminal config path)
cat "$DIR/settings.json"          # font_size, appearance, theme, layout
cat "$DIR/keys.json"              # command id -> key or list of keys
cat "$DIR/layouts/main.json"      # rows/columns of workspaces, panes, agents, mods
cat "$DIR/themes/atom-one-dark.json"  # background, foreground, selection, ansi[16]
cat "$DIR/mods/hello/main.js"     # a JavaScript panel
```

Only edit a file the person asked about, and only the part they named. Layouts
are rows, columns and stacks of built-in blocks, plus draggable edge floats.
Fixed-pixel blocks resize by their divider or `do layout.resize --id ID --px N`.
Floats can be dragged, can peek as an edge strip, and with `snap` spring to the
nearest edge. Mods draw a neutral tree, not a web page.

## Machines

Add a machine once, then put `--on NAME` on a core command. The remote needs
`wada-terminal` installed and SSH keys that work without a password prompt.

```sh
wada-terminal core machine add vps --ssh vps --bin wada-terminal
wada-terminal core --on vps run --bg --name build --label space=review -- make test
wada-terminal core read --on vps build --lines 100
wada-terminal core machine remove vps
```

The first command starts the remote core detached over SSH. A `machines` block
in a layout lists each machine's running terminals; click one to open it here.
A space on the machine appears in this host's sidebar with a machine badge:

```sh
wada-terminal do space.new --machine vps --name vps
wada-terminal core layout get --on vps   # layout saves publish here too
wada-terminal do layout.stack --stack panel --active agents
```

Its tabs and splits stay on that machine and reattach from a saved layout. The
Machines panel lists its live terminals, but a remote core's own saved spaces
are not merged. A machine is someone's work: do not add or start one unless
asked.

## Reusable roles

Templates live in `templates/<name>.md` in the config folder. List them with
`wada-terminal core templates
wada-terminal core layout get
wada-terminal core layout set spaces.json`. A template supplies the command, how to know
it is ready, and the instructions; your task is appended:

```sh
wada-terminal core run --bg --template reviewer "Review src/x.go only."
```

The default readiness is the program's first OSC 7501 report; `ready: quiet`
waits for one second of quiet output instead. Do not edit a template the person
did not ask you to change.

## Mods

Mods are JavaScript panels in `mods/<name>/main.js`. Read the installed
`AGENTS.md` for the API before changing one. A layout shows a mod with:

```json
{"type": "mod", "mod": "hello", "size": {"px": 280}}
```

The script defines `render(api)` and may define `click(id, api)`. `api` exposes
`spaces`, `agents`, `focused`, `inputs`, `props`, `state`, `saveState()` and
`now`; a click can call `api.run(action, args)` or `api.send(terminal, text)`.
An `input` node has `value`, `placeholder`, `on_submit` and `autofocus`; a
submit calls `click(id, api)`, and `api.inputs` holds every field's text.
Style can translate, scale, rotate and fade a node, and `api.animate(ms)` asks
for frames at that pace. Colors are palette names only. There is no Node, imports, network or web view. A
render is interrupted after 500 ms and a click after 2 s, so a runaway loop
shows an error instead of freezing the window. Keep a change to the mod the person asked
about; save, then check the panel and `mods/<name>/error`.

## Agent state

A program that speaks the Program Status Protocol (OSC 7501) is an agent to
the window, whatever it is called. It reports what it is doing, and the window
shows that instead of reading the screen:

```sh
printf '\033]7501;state=working:app=cargo:progress=40\033\\'
printf '\033]7501;state=blocked:kind=permission:title=Rml4IGxvZ2lu:msg=QWxsb3cgdGhlIGVkaXQ/\033\\'
printf '\033]7501;state=done\033\\'
```

States are `idle`, `working`, `done`, `blocked` and `error`; `blocked` may say
`kind=permission`, `question` or `auth`. `title` and `msg` are base64 text for
people. A report without an id is the program's own record; `state=clear`
withdraws it. Programs that never report fall back to reading the bottom of
their screen.

## Rules

- Never signal, stop or restart the person's window or its programs.
- Prefer `terminal.send` for text; keys a full-screen program needs (Escape,
  arrows) are not modelled yet.
- A window with no socket is not running. Do not open another one.
- `WADATERM_CORE=off` makes the window own its panes again; it is for
  comparison, not a normal setting.
- Pane data lives in the core, under the app's data folder: `core.sock`,
  `core.json` and `core.log`. The window's own socket is `terminal.sock`.
  Do not remove or rewrite a core another person is using. Do not kill a core
  to "clean up": its terminals are someone's work.
- `report`, `wait --children`, `wait --children --any`, `core events`,
  templates and `--on MACHINE` are the small versions. Machine spaces join
  this sidebar when made with `space.new --machine`; a remote core's own
  layout does not. `stop` in a template is stored but unused; read a child's
  screen with `core read` when you need the full output.

## Task workspaces

When asked to create isolated task work, use `do task.new --name "Task name"
--folder /absolute/repository --agent opencode` (or `shell`, `claude`, `codex`,
`custom:ID`). It starts asynchronously; inspect `status`/`ls` for the new space's
`task` metadata. Errors open the task dialog. Task creation starts from committed
HEAD, leaves source edits untouched, and keeps the worktree when the window closes. Finishing a task uses the app’s
confirmation dialog: `do task.finish` opens it but cannot confirm deletion.
The person can save changes and clean up, or explicitly discard unsaved files.
Unmerged commits retain a recovery branch; the working folder and task entry
are removed after successful confirmation.
Select the intended space using its id before `do task.review` or `do task.check
--command "go test ./..."`. Checks are explicit commands, executed in the task
worktree with retained exit code/output and snapshot evidence. Never run checks
or send task prompts without the person's authorization. No automatic merge.
