package terminal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"wada-terminal/internal/config"

	"github.com/xleotranx/wada-kit/contract/shell"
	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/kernel/kerneltest"
	"github.com/xleotranx/wada-kit/kittest"
)

type countedTerminal struct {
	kernel.Plugin
	setups *int
}

func (p *countedTerminal) Setup(c *kernel.Context) error {
	*p.setups++
	return p.Plugin.Setup(c)
}

func TestFailureDoesNotRepeatTerminalSetup(t *testing.T) {
	t.Setenv(config.EnvDir, filepath.Join(t.TempDir(), "config"))
	store := kerneltest.NewStore()
	// Restore an empty workspace so the real plugin activates without starting
	// programs or using the developer's sessions.
	if err := store.Save(context.Background(), []kernel.Value{{
		Namespace: "terminal", Key: "workspaces", JSON: []byte(`[{"name":"test","tabs":[]}]`),
	}}); err != nil {
		t.Fatal(err)
	}
	setups := 0
	host := &kittest.Host{}
	h := kerneltest.Start(t, kerneltest.Config{Store: store, Lenient: true,
		Restart: kernel.Restart{Max: 3, Within: time.Minute}}, host.New, func() kernel.Plugin {
		return &countedTerminal{Plugin: New(), setups: &setups}
	})
	pages := kernel.Items(h, shell.Pages)
	if len(pages) != 1 || pages[0].Plugin != "terminal" {
		t.Fatalf("terminal did not activate: %+v", h.Plugins())
	}
	kernel.Invoke(h, pages[0], func(shell.Page) { panic("view failed") })
	h.Flush()
	for _, st := range h.Plugins() {
		if st.Manifest.ID == "terminal" {
			if st.State != kernel.Failed || st.Restarting || setups != 1 {
				t.Fatalf("failure repeated process-owning Setup: %+v, setups=%d", st, setups)
			}
			return
		}
	}
	t.Fatal("terminal plugin missing")
}
