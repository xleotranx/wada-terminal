//go:build !unix

package pty

import (
	"errors"
	"os"
)

// ErrUnsupported is what Start returns where the system has no pseudo-terminals.
var ErrUnsupported = errors.New("pty: this system has no pseudo-terminals")

// Options describes the program a session runs and the terminal it sees.
type Options struct {
	Argv       []string
	Env        []string
	Dir        string
	Cols, Rows int
}

// Session is a program on a pseudo-terminal.
type Session struct{}

func Start(Options) (*Session, error)        { return nil, ErrUnsupported }
func (*Session) Read([]byte) (int, error)    { return 0, ErrUnsupported }
func (*Session) Write(p []byte) (int, error) { return 0, ErrUnsupported }
func (*Session) Resize(cols, rows int) error { return ErrUnsupported }
func (*Session) PID() int                    { return 0 }
func (*Session) Signal(sig os.Signal) error  { return ErrUnsupported }
func (*Session) Wait() error                 { return ErrUnsupported }
func (*Session) Close() error                { return nil }
