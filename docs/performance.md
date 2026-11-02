# Performance

What a terminal spends its time on, what was measured, and what a change to
these paths has to pay for. Numbers are from an Apple M4, macOS 27.

## Measuring

```sh
# The emulator's hot path: a pane's size, a program's output, nothing else.
go test ./internal/vt -run XXX -bench BenchmarkScreenStream -benchtime 3s -benchmem

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

## What was slow

A pane turned every read of its program into its own frame: the window was
woken for each two-kilobyte chunk, and each of those posts crossed the Go
and AppKit boundary. Under a flood that was most of both processes' CPU.

The emulator copied the grid and shifted the whole scrollback for every line
that scrolled: 187 KB of cells per line for a 114×42 pane, and 96 KB of
slice headers once the 4000-line scrollback was full. It also allocated a
string for every character it painted, which is what made it the limit at
about 8 MB/s.

## What changed

- `internal/core`: a viewer's writer gathers output for 4 ms while a program
  streams and sends one frame, reusing the buffer; output after a pause goes
  at once, so a prompt appears as soon as it is written. `maxPending` bounds
  a viewer that stopped reading.
- `internal/plugins/terminal`: `session.push` applies what arrived on the UI
  thread at most once per `feedGap` (8 ms), instead of once per read.
- `internal/vt`: the grid is rows, and the scrollback is a ring, so a scroll
  moves row slices and recycles the rows it empties. Single-cell writes skip
  the wide-character repair, single runes come from an interned table, and
  ASCII takes short paths through the width tables.
- Idle ticks sleep: the 60 Hz mod animation only runs while a mod animates,
  and the foreground, task and machine polls ask for no frame when they find
  nothing (`Scope.FrameDue`).

## Measurements

| | before | after |
|---|---|---|
| 200 MB of terminal output, end to end | 21.19 s (9.4 MB/s) | 3.16 s (63.3 MB/s) |
| CPU for that output: window + core | 113 % + 132 % | 83 % + 103 % |
| `BenchmarkScreenStream` | 8.0 MB/s, allocs per op | 97.8 MB/s, 0 allocs/op |
| idle CPU, window | 1.4 %, 1–2 frames/s | 0.2 %, no frames |
| idle footprint, window + core | 50.5 MB + 13.0 MB | 45.9 MB + 12.5 MB |
| core footprint after the flood | 58.5 MB | 51.7 MB |

Startup to a usable window stayed at about 0.4 s.

## Tried and dropped

- Raising `GOGC`/tuning the heap: the window's footprint is mostly the
  framework's font and GPU memory, not the Go heap.
- Trimming trailing blank cells out of scrollback rows: the row's backing
  array stays alive with the slice, so it costs allocation without saving
  memory.
- Packing `vt.Style` into a smaller cell: worth about 20 % of the
  scrollback, but it touches every style construction in the emulator and
  the painter.
