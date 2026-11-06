package core

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// ConnectMachine opens a core on another machine. It first tries the proxy;
// when no core answers it starts one detached over SSH and tries again. The
// host and binary are the person's machines.json entry.
func ConnectMachine(host, bin string, stderr io.Writer) (*Client, error) {
	return ConnectMachineContext(context.Background(), host, bin, stderr)
}

// ConnectMachineContext bounds startup and cancels SSH attempts with the caller.
func ConnectMachineContext(ctx context.Context, host, bin string, stderr io.Writer) (*Client, error) {
	lifetime := ctx
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if host == "" {
		return nil, fmt.Errorf("core: no SSH host")
	}
	if bin == "" {
		bin = "wada-terminal"
	}
	args := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", host, bin + " core-proxy"}
	probe := CommandTransport{Args: args, Stderr: io.Discard, Context: ctx}
	if client, err := ConnectTransport(probe); err == nil {
		client.dial = (CommandTransport{Args: args, Stderr: io.Discard, Context: lifetime}).Dial
		return client, nil
	}
	start := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", host,
		"nohup "+bin+" --serve-core >/dev/null 2>&1 &")
	start.Stderr = stderr
	_ = start.Run()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		client, err := ConnectTransport(probe)
		if err == nil {
			client.dial = (CommandTransport{Args: args, Stderr: io.Discard, Context: lifetime}).Dial
			return client, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if last == nil {
		last = fmt.Errorf("no answer")
	}
	return nil, fmt.Errorf("core on %s did not answer (is %s installed there?): %w", host, bin, last)
}
