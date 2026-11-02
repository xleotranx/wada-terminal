package core

import (
	"fmt"
	"io"
	"os/exec"
	"time"
)

// ConnectMachine opens a core on another machine. It first tries the proxy;
// when no core answers it starts one detached over SSH and tries again. The
// host and binary are the person's machines.json entry.
func ConnectMachine(host, bin string, stderr io.Writer) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("core: no SSH host")
	}
	if bin == "" {
		bin = "wada-terminal"
	}
	args := []string{"ssh", "-o", "BatchMode=yes", host, bin + " core-proxy"}
	probe := CommandTransport{Args: args, Stderr: io.Discard}
	if client, err := ConnectTransport(probe); err == nil {
		return client, nil
	}
	start := exec.Command("ssh", "-o", "BatchMode=yes", host,
		"nohup "+bin+" --serve-core >/dev/null 2>&1 &")
	start.Stderr = stderr
	_ = start.Run()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		client, err := ConnectTransport(probe)
		if err == nil {
			return client, nil
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("no answer")
	}
	return nil, fmt.Errorf("core on %s did not answer (is %s installed there?): %w", host, bin, last)
}
