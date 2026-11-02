package pty

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ForegroundName is the program in the terminal's foreground process group:
// what a person would say the pane is running now. It reads the group with ps,
// because a session starts no goroutine and the kernel exposes it per call.
func (s *Session) ForegroundName() (string, bool) {
	pid := s.PID()
	if pid <= 0 {
		return "", false
	}
	out, err := exec.Command("ps", "-o", "tpgid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pgid <= 0 {
		return "", false
	}
	out, err = exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pgid)).Output()
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", false
	}
	return filepath.Base(name), true
}
