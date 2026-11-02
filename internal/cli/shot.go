package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"wada-terminal/internal/control"
)

// shotCommand asks the running window for a PNG of its last frame and writes
// it. The window captures its own rendering, so it works while covered and
// needs no screen-recording permission.
func shotCommand(folder, env string, args []string) int {
	path := "wada-shot.png"
	if len(args) > 1 {
		return configUsageError("shot takes at most one file")
	}
	if len(args) == 1 {
		path = args[0]
	}
	socket, err := socketPath(folder, env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal shot:", err)
		return 1
	}
	result, err := control.Call(socket, control.Request{Method: "shot"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal shot:", err)
		return 1
	}
	var answer struct {
		PNG string `json:"png"`
	}
	if err := json.Unmarshal(result, &answer); err != nil || answer.PNG == "" {
		fmt.Fprintln(os.Stderr, "wada-terminal shot: the window sent no picture")
		return 1
	}
	png, err := base64.StdEncoding.DecodeString(answer.PNG)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal shot:", err)
		return 1
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "wada-terminal shot:", err)
		return 1
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	fmt.Println(absolute)
	return 0
}
