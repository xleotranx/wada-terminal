# Contributing

Bug reports and focused pull requests are welcome. This is development
software; the [known gaps](README.md#known-gaps) say what it does not do yet.

## Build and test

macOS is the tested platform. The first build needs network access for the Go
modules and MyGo's libghostty-vt.

```sh
make run
make check   # go vet ./..., go tool mygo vet ., go test ./...
```

The pseudo-terminal tests open `/dev/ptmx`; run them outside a sandbox that
denies it.

## Before a pull request

- `make fmt` and `make check` pass.
- Behaviour changes come with a test; the Go tests sit beside the code, and
  `app_test.go` drives the window.
- The rules in [AGENTS.md](AGENTS.md) hold: plugin code uses `kernel.Context`
  and `contract/*` only, plugin state stays on the UI thread, a frame performs
  no I/O, and chrome colors come from `native.PaletteFor`.
- `README.md` changes with a feature or shortcut, and `AGENTS.md` with the
  source map.

One change per pull request, with what it does and why in the description.
