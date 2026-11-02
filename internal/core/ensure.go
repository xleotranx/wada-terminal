package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Ensure makes sure a core is running on a data folder, starting one from this
// executable if not. It returns once the core answers, or with the reason it
// could not start. The window may open without a core; its panes then run in
// the window, as they did before the core existed.
func Ensure(dir string) error {
	if _, err := Connect(dir); err == nil {
		return nil
	}
	if dir == "" {
		return fmt.Errorf("core: no data folder")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "core.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--serve-core")
	cmd.Env = withEnv(os.Environ(), "WADATERM_DATA_DIR", dir)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	_ = cmd.Process.Release()
	logFile.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Connect(dir); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("core: it did not start within 5s; see %s", logPath)
}

// withEnv replaces a variable in an environment, rather than adding a second
// value for the same key.
func withEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+"="+value)
}
