# Wada Terminal

<!-- Written on every start. Edits to this file are overwritten. -->

This folder is Wada Terminal's interface as files. The window redraws itself
when one changes. The files you edit are `settings.json`, `keys.json`,
`layouts/<name>.json` and `themes/<name>.json`; this guide is replaced on every
start, so put notes elsewhere.

## The two programs

- **`wada-terminal`** is the window: spaces, tabs, split panes, the Sessions view.
- **the core** runs the programs. It keeps running when the window closes. The
  window reattaches to its terminals and to the saved layout.

```sh
wada-terminal config             # where these files are, and which are broken
wada-terminal shot [file.png]    # a PNG of the window's last frame
wada-terminal config path        # just the folder
wada-terminal status             # the open space, tab, pane and flags
wada-terminal ls                 # every space > tab > pane
wada-terminal do <action> ...    # change what the window shows
wada-terminal commands           # every do action and its arguments
wada-terminal core ls            # every terminal, its foreground program and labels
wada-terminal core machine ls
wada-terminal core machine add vps --ssh vps --bin wada-terminal
wada-terminal core templates
wada-terminal core layout get
wada-terminal core layout set spaces.json
wada-terminal core run --bg --template reviewer "Review src/x.go"
wada-terminal core run --bg --name build -- make test
wada-terminal core --on vps run --bg --name remote -- make test
wada-terminal core read --on vps remote --lines 100
wada-terminal do space.new --machine vps --name vps
wada-terminal do layout.stack --stack panel --active agents
wada-terminal core ask "Which file should I edit?"
wada-terminal core questions
wada-terminal core answer t7 --question q1 "src/x.go"
wada-terminal core read build --lines 100
wada-terminal core send build "rerun the failed package"
wada-terminal core wait build
wada-terminal core report "the tests pass"
wada-terminal core wait --children
wada-terminal core wait --children --any
wada-terminal core events --children
wada-terminal skill              # the short guide for driving the window
```

A terminal is addressed as `t7` or by the `--name` it was started with. Inside
a core terminal, `WADA_TERMINAL_ID` is its address and `WADA_PARENT_ID` is the
terminal that started it. A background child's exit, and its `core report`,
arrive in the parent's input tagged `<wada from="t7 name">…</wada>`.

A helper can ask and wait:

```sh
wada-terminal core ask "Which file should I edit?"
```

The parent sees `<wada from="t7 reviewer" question="q1">…</wada>` and the command
`wada-terminal core answer t7 --question q1 "..."`. The answer is the ask's
stdout. `core questions` lists open questions; `core questions --all` includes
answered ones. `--timeout 5m` sets the wait; a timeout cancels the question.

## The files

| File | What it decides | Applied |
| --- | --- | --- |
| `settings.json` | text size, appearance, theme, layout, sidebar | live |
| `keys.json` | what each command's shortcut is | live |
| `layouts/<name>.json` | rows and columns of Workspaces, Panes, Agents and mods | live |
| `themes/<name>.json` | the terminal's sixteen ANSI colors and its surface | live |
| `templates/<name>.md` | reusable agent roles for `core run --template` | read on use |
| `mods/<name>/main.js` | a JavaScript panel a layout can show | live |
| `mods/<name>/state.json` | the mod's saved state | written by the mod |
| `machines.json` | cores on other machines, reached over SSH | on use |
| `agents.json` | extra programs in the Session menu | live |
| `programs.json` | richer program entries; those with `run` join the menu | live |
| `AGENTS.md` | this map | rewritten on start |

A broken settings, keys, layout or theme file keeps the last good version and is reported beside it as
`settings.error`, `keys.error`, `layout.error` or `theme.error`. Fix the file
and save; the error goes away. The window shows a toast when a save does not
load. A save can take up to a second to apply.

### templates/<name>.md

A template is a reusable role: a command, a description, how to know it is
ready, and the instructions it starts with. The header is line-based, not
YAML; values are literal text after the first colon.

```markdown
---
description: Read-only code review
command: pi
ready: quiet
submit: tab
paste_gap_ms: 80
---
Review the diff. Report findings with file paths and lines.
Do not edit files.
```

- `command` (required): a shell command that starts the agent.
- `ready`: empty waits for the program's first OSC 7501 report; `quiet` waits
  for one second of quiet output.
- `submit`: `enter` (default), `tab`, `escape`, arrows or `ctrl+x`.
- `paste_gap_ms`: pause before the submit key.
- `stop`: stored for compatibility; not used yet.

`core run --template reviewer "Review src/x.go"` starts the command, waits for
ready, then sends the body, a blank line and the task. A template never installs
an agent or teaches it OSC 7501.

### agents.json

An agent beyond the built-in Claude Code, Codex and OpenCode entries:

```json
{
  "aider": { "name": "Aider", "command": ["aider"], "glyph": "◆", "color": "#ff8800" }
}
```

A runnable entry in `programs.json` works too and is merged in unless
`agents.json` already names it:

```json
{ "programs": { "aider": { "name": "Aider", "run": ["aider"], "color": "#ff8800",
                           "icon": "icons/aider.svg", "tint": true } } }
```

`icon` is an SVG path under the config folder; `tint` draws it in the text
color instead of its own colors. `match` lists program names: when a local pane's
foreground process on this computer or a machine (or, as a fallback, its window
title) contains one, that pane becomes the agent.

Entries without `run` are display metadata and do not join the Session menu
yet. `color` is the icon's `#rrggbb` background.

It adds **Session → New Aider Session** (command id `agent-aider`, so
`keys.json` can bind it) and starts `command` in the shown space. A session it
starts is an agent even before it reports OSC 7501, and the saved layout knows
which entry to run again after a restart. `name` defaults to the key and
`command` to `[key]`; `glyph` is the icon's mark.

### machines.json

A machine is a core on another machine, reached with `ssh`. This window and
the core command line can run programs there; the remote needs
`wada-terminal` installed and SSH keys that work without a password prompt.

```json
{
  "vps": { "ssh": "me@vps", "bin": "/usr/local/bin/wada-terminal" }
}
```

`bin` is the remote executable, or `wada-terminal` from the remote `$PATH`.
Manage the file with:

```sh
wada-terminal core machine ls
wada-terminal core machine add vps --ssh vps --bin wada-terminal
wada-terminal core machine remove vps
```

Put `--on NAME` on a core command to use that machine:

```sh
wada-terminal core --on vps ls
wada-terminal core run --on vps --bg --name build --label space=review -- make
wada-terminal core read --on vps build
```

The first use starts the remote core detached over SSH.

A `machines` block lists every configured machine and its running terminals;
the list refreshes every two seconds, and clicking a terminal opens it as a
machine space in this window:

```json
{"type": "machines", "size": { "px": 280 }}
```

This shows a remote core's terminals, not its saved spaces. A space made on a
machine runs its terminals there and appears in this window
with the machine's name in its header:

```sh
wada-terminal do space.new --machine vps --name vps
wada-terminal do layout.stack --stack panel --active agents
```

Every tab and split in that space starts on the same machine. The saved layout
names the machine and its terminals, so another window reattaches to them. The
Machines panel lists a remote core's live terminals, but a remote core's own
saved spaces are not merged into this layout.

### settings.json

```json
{
  "font_size": 12.5,
  "appearance": "system",
  "theme": "auto",
  "layout": "main"
}
```

- `font_size`: 9 to 22.
- `appearance`: `system`, `light` or `dark`.
- `theme`: `auto` for the window's own light and dark palettes, or the name of
  a file in `themes/`.
- `layout`: the name of a file in `layouts/`.
- `sidebar`: optional `true` or `false`; when absent the window keeps the
  person's last choice (also changed by the Toggle Sidebar command).

Unknown keys are an error, so a typo is visible instead of silently ignored.

### layouts/main.json

A layout is a tree of rows and columns. A leaf shows one of three built-in
blocks: `workspaces` (the space/tab tree), `panes` (the shown tab), `tabs`
(the tab strip), `title` (the title-bar actions) or `agents`
(every agent, permanently, with its state). The layout above the panes is yours.

```json
{
  "root": {
    "type": "row",
    "gap": 4,
    "children": [
      { "type": "workspaces", "size": { "px": 220 } },
      { "type": "panes", "size": { "weight": 1 } },
      { "type": "agents", "size": { "px": 270 }, "background": "drawer" }
    ]
  }
}
```

- `type`: `row`, `column`, `stack`, `workspaces`, `panes`, `tabs`, `title`, `agents`, `machines`,
  `mod` or `float`.
- `children`: for a row, column or stack.
- `size`: `{ "px": 220 }` fixed along the parent's direction, or
  `{ "weight": 2 }` a share of what fixed siblings leave.
- `min`, `max`: pixels.
- `padding`, `gap`: pixels.
- `background`: `drawer`, `panel`, `raised` or `transparent`; chrome colors
  come from the window palette, not from the file.
- `hidden`: leave the block out.

A layout needs exactly one `panes` block and at most one `workspaces`, one
`agents` and one `machines` block. The older `sidebar` shorthand is still read and becomes a row:

```json
{ "sidebar": { "side": "left", "width": 236 } }
```

A `mod` block shows a JavaScript panel:

```json
{
  "type": "mod",
  "mod": "hello",
  "size": { "px": 280 },
  "props": { "title": "Review" }
}
```

A stack is a small tab group: it shows one child at a time. Each child needs an
`id`, and the stack's `active` names the one to show first.

```json
{
  "type": "stack",
  "id": "panel",
  "active": "hello",
  "size": { "px": 280 },
  "children": [
    { "type": "mod", "id": "hello", "mod": "hello" },
    { "type": "agents", "id": "agents" }
  ]
}
```

Switch it without editing the file:

```sh
wada-terminal do layout.stack --stack panel --active agents
wada-terminal do layout.stack.next --stack panel
```

The choice is saved per window. `status` reports it under `stacks`.

A float is a mod in a framed panel over the page, anchored to an edge:

```json
{
  "type": "float",
  "id": "hello",
  "mod": "hello",
  "edge": "right",
  "align": "center",
  "width": 300,
  "height": 240,
  "offset": 12
}
```

`edge` is `top`, `right`, `bottom` or `left`; `align` is `start`, `center` or
`end` along that edge; `width` and `height` are pixels; `offset` is the inset.
`peek` (a side edge only) collapses the float to that many pixels with a `⋮ id`
strip until the pointer is over it, then expands it. `snap: true` springs the
float to the nearest edge when it is let go. A float takes no room from
the row or column. The pointer drags it by its frame (buttons and inputs still
take their presses); the offset is saved per window and reported under `floats`
in `status`. With `snap` it springs to the nearest edge; there are no thirds or
custom spring curves. Keep a float small and out of the way of the panes; it
does not resize them.

A block with a fixed `size.px` can be resized by dragging the divider beside
it, or by naming its `id` and calling:

```sh
wada-terminal do layout.resize --id side --px 300
```

The size is saved under `layout-sizes` and reported under `sizes` in `status`.
A block sized by `weight` has no divider. `Cmd+B` hides the `workspaces` block
wherever it is; the split dividers inside a tab still drag separately.

### mods/<name>/main.js

A mod is a small JavaScript panel: trusted code, with no imports, no Node and
no web view. It defines `render(api)` returning a tree, and may define
`click(id, api)`. The window calls render at most twice a second per block.

```js
function render(api) {
  return {
    type: "column",
    padding: 12,
    gap: 8,
    children: [
      {type: "text", text: "Agents: " + api.agents.length, font_size: 13, bold: true},
      {type: "text", text: "Focused pane " + api.focused, color: "muted", font_size: 11},
      {type: "button", id: "count", on_click: true,
       text: "Clicks: " + (api.state.count || 0)}
    ]
  };
}
function click(id, api) {
  if (id === "count") { api.state.count = (api.state.count || 0) + 1; }
}
```

Node types: `row`, `column`, `box`, `text`, `button`, `input`, `scroll`,
`spacer`, `divider`. Fields: `type`, `id`, `text`/`label`, `children` or
`child`, `on_click: true` (needs `id`). An input adds `value`, `placeholder`,
`on_submit: true` (calls `click(id, api)` on Enter) and `autofocus: true` (takes
the keyboard the first time it draws). Its text survives the script's
twice-a-second render.

`api.inputs` is every input's current text, by id. Style fields: `padding`,
`gap`, `grow`, `width`, `height`, `min_width`, `max_width`, `min_height`,
`max_height`, `align` (`start`, `center`, `end`, `stretch`), `justify`
(`start`, `center`, `end`, `between`), `background`, `border`, `color`,
`font_size`, `bold`, `radius`, `translate_x`, `translate_y`, `scale`, `rotate`
and `opacity` (above 0, up to 1).

Colors are palette names only: `text`, `muted`, `faint`, `link`/`accent`,
`green`, `waiting`, `danger`, `warning`, `orange`, `panel`, `drawer`, `raised`,
`hover`, `selected`, `hairline`, `transparent`. Any other color is ignored.

What `api` gives render:

- `api.spaces`: spaces as `{id, name, folder, active, tabs}`
- `api.agents`: agents as `{id, title, message, state, word, app, workspace, tab, focused}`
- `api.inputs`: the text of each input, by its id
- `api.focused`: the pane id with the keyboard, or 0
- `api.props`: the layout block's `props` object
- `api.state`: this mod's state, loaded from `state.json`
- `api.now`: milliseconds since the epoch
- `api.animate(ms)`: ask the window to draw this block again every `ms`
  (16–1000); with `api.now` and transforms, a mod can move or fade

A click's API also has:

- `api.saveState()`: write `state.json` now (a click saves automatically)

- `api.run(action, args)`: run any `wada-terminal do` action; it is validated
  and returns the action's result or throws
- `api.send(terminal, text)`: type text into a pane, without Return; it throws
  for an unknown id or a pane that is not running

Limits: 1000 nodes, depth 32, 200 children, text 4000 bytes. A broken script
keeps the last good panel; its block shows the error and the load error is
written to `mods/<name>/error`. A render is interrupted after 500 ms and a
click after 2 s, so a runaway loop shows an error rather than freezing the
window. Edit a mod the person asked about, save, and check the window (use
`wada-terminal shot` when you need to see it).

### keys.json

Keys are command ids from `wada-terminal commands`, with or without the
`terminal.` prefix. A value is one key or a list; an empty list leaves the
command in the menu without a shortcut.

```json
{
  "new-tab": "cmd+t",
  "split-right": ["cmd+d", "ctrl+shift+d"],
  "find": "cmd+f",
  "links": "cmd+shift+l",
  "index": "cmd+shift+i"
}
```

The key syntax is the one the commands print: `cmd+t`, `cmd+shift+d`,
`ctrl+alt+k`. The shell gives a menu command's first key to its menu entry.
An unknown command id is an error and changes nothing.

### themes/atom-one-dark.json

```json
{
  "background": "#282c34",
  "foreground": "#abb2bf",
  "selection": "#3e4451",
  "cursor": "#abb2bf",
  "ansi": ["#282c34", "#e06c75", "... sixteen colors ..."]
}
```

All colors are `#rrggbb`. `selection` and `cursor` are optional. The theme
applies to the terminal cells; the window's own chrome keeps following the
system palette. `theme: "auto"` ignores theme files.

## Seeing the window

`wada-terminal shot [file.png]` saves the window's last frame, including
terminals, overlays and themes, at the window's pixel size. It captures the
app itself, so it works while covered and needs no screen-recording
permission. Use it to check a visual change; a still-working screen is not
proof that a file loaded.

## Agent state

A program that speaks the Program Status Protocol (OSC 7501) is an agent to the
window, whatever it is called:

```sh
printf '\033]7501;state=working:app=cargo:progress=40\033\\'
printf '\033]7501;state=blocked:kind=permission:title=Rml4IGxvZ2lu:msg=QWxsb3cgdGhlIGVkaXQ/\033\\'
printf '\033]7501;state=done\033\\'
```

States are `idle`, `working`, `done`, `blocked` and `error`; a blocked record
may say `kind=permission`, `question` or `auth`. `title` and `msg` are base64
text for people. A report without an id is the program's own; a program that
never reports falls back to its screen.

## Rules

- Change only the file the person asked about. Saved work is theirs.
- Never stop, signal or restart a window or a core you did not start. A core is
  someone's running work; `core kill` ends that work.
- Mods are trusted JavaScript. There is no Node, no imports, no network and no
  web view; do not try to reach files or the internet from a mod.
- Files are the interface. Prefer them over controlling the pointer or typing
  into a terminal you were not asked to touch.
- A machine is someone's work on another computer. Do not start, stop or
  reconfigure a machine you were not asked to use.
