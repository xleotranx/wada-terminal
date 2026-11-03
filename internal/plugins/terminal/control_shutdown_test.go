package terminal

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestControlConnectionClosesOnShutdown(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { (&Plugin{}).serveControl(ctx, server); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle control connection prevented shutdown")
	}
}
