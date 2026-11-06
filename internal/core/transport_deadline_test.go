package core

import (
	"testing"
	"time"
)

func TestCommandTransportDeadline(t *testing.T) {
	conn, err := (CommandTransport{Args: []string{"/bin/sh", "-c", "exec sleep 5"}}).Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	start := time.Now()
	_, err = conn.Read(make([]byte, 1))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("deadline not enforced: %v after %v", err, time.Since(start))
	}
}
