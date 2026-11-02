//go:build !unix

package core

import "os/exec"

// setDetached is a no-op where a detached child needs another mechanism.
func setDetached(cmd *exec.Cmd) {}
