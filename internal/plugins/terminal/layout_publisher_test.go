package terminal

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/kernel/kerneltest"
	"github.com/xleotranx/wada-kit/kittest"
	"wada-terminal/internal/core"
)

func TestRemoteLayoutPublishesLatestSnapshot(t *testing.T) {
	dir, err := os.MkdirTemp("", "wada-layout-publish-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	server, err := core.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := core.Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{machineConns: map[string]*machineConn{"box": {client: client}}, workspaces: []*workspace{{machine: "box", name: "old"}}}
	host := &kittest.Host{}
	h := kerneltest.Start(t, kerneltest.Config{}, host.New, func() kernel.Plugin { return &remoteLifecyclePlugin{p} })
	for i := 0; i < 50; i++ {
		p.publishRemoteLayouts()
	}
	p.workspaces[0].name = "latest"
	p.publishRemoteLayouts()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.Flush()
		body, err := client.Layout()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"name":"latest"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("latest snapshot was not saved: %s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
