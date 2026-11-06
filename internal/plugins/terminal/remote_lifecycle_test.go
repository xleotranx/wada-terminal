package terminal

import (
	"os"
	"testing"
	"time"

	"github.com/xleotranx/wada-kit/contract/shell"
	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/kernel/kerneltest"
	"github.com/xleotranx/wada-kit/kittest"
	"wada-terminal/internal/core"
)

type remoteLifecyclePlugin struct{ p *Plugin }

func (*remoteLifecyclePlugin) Manifest() kernel.Manifest {
	return kernel.Manifest{ID: "remote-lifecycle", Requires: []kernel.Named{shell.HostKey}}
}
func (p *remoteLifecyclePlugin) Setup(c *kernel.Context) error {
	p.p.font = kernel.NewSetting(c, "font-size", defaultFontSize)
	p.p.ctx = c
	p.p.host = kernel.Require(c, shell.HostKey)
	return nil
}

// Closing before the worker delivers its result must end the program it
// started, even though the pane does not yet know the remote terminal ID.
func TestCloseDuringRemoteStart(t *testing.T) {
	dir, err := os.MkdirTemp("", "wada-pending-")
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
	p := &Plugin{}
	host := &kittest.Host{}
	h := kerneltest.Start(t, kerneltest.Config{}, host.New, func() kernel.Plugin { return &remoteLifecyclePlugin{p} })
	s := &session{t: p, client: client, kind: kindShell, alive: true, cols: 40, rows: 10}
	if !p.startRemote(s, []string{"/bin/cat"}) {
		t.Fatal("start was not scheduled")
	}
	s.kill()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.Flush()
		list, err := client.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Terminals) == 1 && !list.Terminals[0].Alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("closed pane left a program running: %+v", list)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A detected foreground agent is display metadata, not a replacement for the
// shell's launch command. Removing its entry must still permit a restart.
func TestRestartDetectedAgentAfterConfigurationRemoval(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	p := &Plugin{}
	host := &kittest.Host{}
	kerneltest.Start(t, kerneltest.Config{}, host.New, func() kernel.Plugin { return &remoteLifecyclePlugin{p} })
	old := &session{t: p, kind: kindShell, agent: true, detected: true, agentID: "removed", name: "Shell", cols: 40, rows: 10}
	tb := &tab{root: leafOf(old), focus: old}
	w := &workspace{tabs: []*tab{tb}, focus: old}
	p.workspaces = []*workspace{w}
	p.current = w
	p.restart(old)
	if tb.focus == old || tb.focus.agent || tb.focus.kind != kindShell || len(tb.focus.argv) == 0 {
		t.Fatalf("restart lost the original shell: %+v", tb.focus)
	}
}
