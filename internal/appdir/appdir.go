// Package appdir finds the folder an app keeps its files in. The shell uses
// the same rule for its settings database; the command line and a core
// process need it before there is a shell or a window.
package appdir

import (
	"os"
	"path/filepath"

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
