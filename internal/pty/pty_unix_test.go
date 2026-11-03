//go:build unix

package pty

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// readUntil reads the session into out until it holds want, or fails.
func readUntil(t *testing.T, s *Session, out *bytes.Buffer, want string) {
	t.Helper()
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("never saw %q; got %q", want, out.String())
		}
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				t.Fatalf("the terminal closed before %q; got %q", want, out.String())
			}
			t.Fatalf("read: %v; got %q", err, out.String())
		}
	}
}

// TestSessionEcho runs a shell on a terminal: it reports its size, reads a
// line and prints it back.
func TestSessionEcho(t *testing.T) {
	sh := findShell(t)
	s, err := Start(Options{
		Argv: []string{sh, "-c", `stty size; read -r line; printf 'got:%s\n' "$line"`},
		Env:  os.Environ(), Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var out bytes.Buffer
	readUntil(t, s, &out, "24 80")
	if _, err := s.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, s, &out, "got:hello")
	if err := s.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// TestResize checks that the size the program sees changes with Resize.
func TestResize(t *testing.T) {
	sh := findShell(t)
	s, err := Start(Options{Argv: []string{sh, "-c", `stty size; sleep 1`}, Env: os.Environ(), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	readUntil(t, s, &out, "30 100")
}

// TestCloseEndsRead checks that closing the terminal ends a blocked read.
func TestCloseEndsRead(t *testing.T) {
	sh := findShell(t)
	s, err := Start(Options{Argv: []string{sh, "-c", "sleep 30"}, Env: os.Environ(), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := s.Read(buf); err != nil {
				done <- err
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not end after Close")
	}
}

func TestCloseEndsInProgressWait(t *testing.T) {
	s, err := Start(Options{Argv: []string{"/bin/sleep", "30"}, Env: os.Environ(), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		waiting := s.waited
		s.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Wait did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not end the running process while Wait was in progress")
	}
}

func findShell(t *testing.T) string {
	t.Helper()
	for _, sh := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	t.Skip("no shell to test with")
	return ""
}
