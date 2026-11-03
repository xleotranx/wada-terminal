// Package appdir finds the folder an app keeps its files in. The shell uses
// the same rule for its settings database; the command line and a core
// process need it before there is a shell or a window.
package appdir

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// Dir is the app's data folder: <Env>_DATA_DIR when set, or the folder in the
// user's config directory. A development run and a production build keep
// theirs apart, as the shell's store does.
func Dir(folder, env string) (string, error) {
	if env != "" {
		if dir := os.Getenv(env + "_DATA_DIR"); dir != "" {
			return dir, nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if mygo.IsDev() {
		folder += "-dev"
	}
	return filepath.Join(dir, folder), nil
}

// TerminalEnv routes commands back to the terminal's environment without
// making a newly launched app inherit this window's folder or MyGo mode.
func TerminalEnv(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, "WADATERM_DATA_DIR=") || strings.HasPrefix(entry, "WADATERM_CONTEXT_DATA_DIR=") || strings.HasPrefix(entry, "MYGO_ENV=") {
			continue
		}
		out = append(out, entry)
	}
	if dir != "" {
		if absolute, err := filepath.Abs(dir); err == nil {
			dir = absolute
		}
		out = append(out, "WADATERM_CONTEXT_DATA_DIR="+dir)
	}
	return out
}

// UseTerminalContext selects the invoking terminal's data for CLI commands.
// An explicit override still wins. Windows and cores do not call this.
func UseTerminalContext() {
	if os.Getenv("WADATERM_DATA_DIR") == "" {
		if dir := os.Getenv("WADATERM_CONTEXT_DATA_DIR"); dir != "" {
			_ = os.Setenv("WADATERM_DATA_DIR", dir)
		}
	}
}
