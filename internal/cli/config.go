package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wada-terminal/internal/config"
)

// configCommand prints where the interface files live and what is broken.
// It is how an agent finds the folder this version reads, rather than assuming
// a path beside the app.
func configCommand(args []string) int {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal config:", err)
		return 1
	}
	if len(args) > 1 {
		return configUsageError("config takes path, or nothing")
	}
	if len(args) == 1 {
		if args[0] != "path" {
			return configUsageError("config takes path, or nothing")
		}
		fmt.Println(dir)
		return 0
	}
	if err := config.Ensure(dir); err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal config:", err)
		return 1
	}
	var files, errors []string
	for _, pattern := range []string{"AGENTS.md", "settings.json", "keys.json", "layouts/*.json", "themes/*.json"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		files = append(files, matches...)
	}
	broken, _ := filepath.Glob(filepath.Join(dir, "*.error"))
	errors = append(errors, broken...)
	sort.Strings(files)
	sort.Strings(errors)
	return printJSON(struct {
		Dir    string   `json:"dir"`
		Files  []string `json:"files"`
		Errors []string `json:"errors,omitempty"`
	}{Dir: dir, Files: relative(dir, files), Errors: relative(dir, errors)})
}

// configUsageError prints a bad config argument.
func configUsageError(message string) int {
	fmt.Fprintln(os.Stderr, "wada-terminal config:", message)
	return 2
}

// relative shortens paths to names inside the config folder.
func relative(dir string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, strings.TrimPrefix(path, dir+string(filepath.Separator)))
	}
	return out
}
