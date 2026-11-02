//go:build unix

package core

import (
	"os/exec"
	"syscall"
)

// setDetached puts the core in its own session: closing the window does not
// hang it up, and it outlives the process that started it.
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
