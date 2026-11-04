# Performance

What a terminal spends its time on, and how to measure it.

## Measuring

```sh
# A window's idle cost, and its cost under output. Build once, then sample
# the two processes by name: the window, and its core.
go build -o /tmp/wt .
WADATERM_DATA_DIR=/tmp/wt-data /tmp/wt &
ps -o pid,time,%cpu,rss,comm -p "$(pgrep -f '^/tmp/wt$')","$(pgrep -f 'wt --serve-core')"
```

The window and the core are separate processes; a number that names one
without the other says nothing. `vmmap --summary <pid>` reports physical
footprint, which is what memory pressure counts; `ps`'s RSS also counts
shared libraries.

## The paths

- The emulator is Ghostty's, libghostty-vt, through MyGo's
  `plugins/terminal`: the core's terminal of each program parses its
  output, and a window's pane terminal parses the frames the core sends.
  Neither lays out text in Go.
- `internal/core`: after output, a terminal waits `frameGap` (4 ms) for more
  of it, then sends each viewer one frame of its `terminal.Mirror`, the
  change since the frame before, not the program's bytes. A program's
  synchronized update holds the frame back, up to a second. A viewer more
  than `viewerQueue` frames behind is dropped and can attach again from a
  first frame.
- `internal/status` scans every byte of a program's output for OSC 7501,
  bracketed paste and hyperlinks, in the core or, without one, on the pane's
  reader; it keeps no screen.
- `internal/plugins/terminal`: word of output reaches the UI thread at most
  once per post (`session.outputSoon`), for the quiet watcher; the pane's
  terminal redraws itself.
- Idle ticks sleep: the 60 Hz mod animation only runs while a mod animates,
  and the foreground, task and machine polls ask for no frame when they find
  nothing (`Scope.FrameDue`).
- The quiet watcher reads a pane's text (`Terminal.Text`, its scrollback and
  screen) once, when an agent goes quiet, not on every output.
