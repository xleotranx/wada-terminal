// Package pty runs a program on a pseudo-terminal: what the program writes
// comes back as one stream, and what the window types and how it sizes the
// pane go the other way, so a full-screen program behaves as it does in a
// terminal.
//
// A Session starts no goroutine of its own: the plugin that owns it reads
// with a scope worker and waits for the process there, so the kernel can
// cancel and count that work.
package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	creackpty "github.com/creack/pty"
)

// ErrClosed is what a session reports once it is closed.
var ErrClosed = errors.New("pty: the session is closed")

// Options describes the program a session runs and the terminal it sees.
type Options struct {
	// Argv is the program and its arguments; Argv[0] is resolved with the
	// environment's PATH.
	Argv []string
	// Env is the environment, as os.Environ gives it.
	Env []string
	// Dir is the working directory; empty keeps the app's.
	Dir string
	// Cols and Rows size the terminal the program sees.
	Cols, Rows int
}

// Session is a program on a pseudo-terminal.
type Session struct {
	cmd    *exec.Cmd
	tty    *os.File
	mu     sync.Mutex
	gone   bool
	closed bool
	waited bool
	err    error
}

// Start starts the program on a new pseudo-terminal of the given size.
func Start(o Options) (*Session, error) {
	if len(o.Argv) == 0 {
		return nil, errors.New("pty: no command to run")
	}
	cmd := exec.Command(o.Argv[0], o.Argv[1:]...)
	cmd.Env = o.Env
	cmd.Dir = o.Dir
	tty, err := creackpty.StartWithSize(cmd, winsize(o.Cols, o.Rows))
	if err != nil {
		return nil, fmt.Errorf("pty: start %s: %w", o.Argv[0], err)
	}
	return &Session{cmd: cmd, tty: tty}, nil
}

// winsize converts a size in cells, keeping a terminal drawable.
func winsize(cols, rows int) *creackpty.Winsize {
	return &creackpty.Winsize{Cols: uint16(max(cols, 1)), Rows: uint16(max(rows, 1))}
}

// Read reads what the program wrote to the terminal. It returns io.EOF once
// the program closed the terminal.
func (s *Session) Read(p []byte) (int, error) { return s.tty.Read(p) }

// Write gives the program input, as keys typed into the terminal.
func (s *Session) Write(p []byte) (int, error) { return s.tty.Write(p) }

// Resize tells the program its terminal changed size, with SIGWINCH.
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gone {
		return ErrClosed
	}
	return creackpty.Setsize(s.tty, winsize(cols, rows))
}

// PID is the program's process ID.
func (s *Session) PID() int {
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Signal sends sig to the program's process group: the program starts a
// session of its own, so its children get the signal too.
func (s *Session) Signal(sig os.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := s.PID()
	if s.gone || pid == 0 {
		return ErrClosed
	}
	unixSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("pty: %v is not a unix signal", sig)
	}
	if err := syscall.Kill(-pid, unixSig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("pty: signal %s: %w", sig, err)
	}
	return nil
}

// Wait waits for the program to exit and reports how it did. It is safe to
// call once, from the worker that read the terminal.
func (s *Session) Wait() error {
	s.mu.Lock()
	if s.waited {
		err := s.err
		s.mu.Unlock()
		return err
	}
	s.waited = true
	s.mu.Unlock()
	err := s.cmd.Wait()
	s.mu.Lock()
	s.gone = true
	s.err = err
	s.mu.Unlock()
	return err
}

// Close hangs the terminal up and kills whatever the program left running.
// It is idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	running := !s.gone
	s.gone = true
	pid := s.PID()
	s.mu.Unlock()
	if running && pid > 0 {
		// The program leads the session it runs in, so its group goes too.
		_ = syscall.Kill(-pid, syscall.SIGHUP)
	}
	return s.tty.Close()
}
