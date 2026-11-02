package terminal

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
)

// machineConn is one machine's core while the window is open: its client, the
// terminals it had when the window connected, and why it could not connect.
type machineConn struct {
	name      string
	client    *core.Client
	terminals map[int]core.Record
	instance  string
	err       error
	at        time.Time
}

// connectSavedMachines connects the cores of the machines the saved layout
// names, before restoring their terminals. A machine that is not named there
// waits until a space asks for it.
func (t *Plugin) connectSavedMachines() {
	if t.saved == nil || len(t.machineDefs) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, spec := range t.saved.Get() {
		if spec.Machine == "" || seen[spec.Machine] {
			continue
		}
		seen[spec.Machine] = true
		_, _ = t.ensureMachine(spec.Machine)
	}
}

// ensureMachine connects a machine's core, or returns the reason it cannot.
// It is called from setup and from an explicit action; SSH can take a moment,
// which is why the window does not connect machines no layout uses.
func (t *Plugin) ensureMachine(name string) (*core.Client, error) {
	if name == "" {
		return t.core, nil
	}
	if conn := t.machineConns[name]; conn != nil {
		if conn.client != nil {
			return conn.client, nil
		}
		if conn.err != nil && time.Since(conn.at) < 5*time.Second {
			return nil, conn.err
		}
	}
	definition, ok := t.machineDefs[name]
	if !ok && t.configDir != "" {
		// A machine added while the window is open is usable without a
		// restart; the watcher also reloads definitions, but an action can
		// arrive first.
		if machines, loadErr := config.LoadMachines(t.configDir); loadErr == nil {
			t.machineDefs = machines
			definition, ok = machines[name]
		}
	}
	if !ok {
		err := fmt.Errorf("no machine %q in machines.json", name)
		t.machineConns[name] = &machineConn{name: name, err: err, at: time.Now()}
		return nil, err
	}
	client, err := core.ConnectMachine(definition.SSH, definition.Bin, os.Stderr)
	conn := &machineConn{name: name, client: client, err: err, at: time.Now()}
	if err == nil {
		conn.instance = client.Instance()
		if list, listErr := client.List(); listErr == nil {
			conn.terminals = make(map[int]core.Record, len(list.Terminals))
			for _, rec := range list.Terminals {
				conn.terminals[rec.ID] = rec
			}
		}
	}
	t.machineConns[name] = conn
	if err == nil {
		t.watchMachine(name, client)
	}
	return client, err
}

// watchMachine refreshes a machine's terminal list in the background, so a
// terminal started with `core --on MACHINE run` shows up in the Machines
// panel without reopening the window.
func (t *Plugin) watchMachine(name string, client *core.Client) {
	t.ctx.Spawn(func(ctx context.Context) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				list, err := client.ListWithin(time.Second)
				if err != nil {
					continue
				}
				terminals := make(map[int]core.Record, len(list.Terminals))
				for _, rec := range list.Terminals {
					terminals[rec.ID] = rec
				}
				t.ctx.Post(func() {
					if conn := t.machineConns[name]; conn != nil && conn.client == client {
						conn.terminals = terminals
						conn.err = nil
					}
					t.adoptMachineForegrounds(name, terminals)
					t.ctx.Invalidate()
				})
			}
		}
	})
}

// machineInstance is the instance the saved layout was tied to for a machine.
func (t *Plugin) machineInstance(name string) string {
	if t.savedInstances == nil || name == "" {
		return ""
	}
	return t.savedInstances.Get()[name]
}

// recordMachineInstances writes every connected machine's instance into the
// saved layout, so a next window can tell a live terminal from a reused id.
func (t *Plugin) recordMachineInstances() {
	if t.savedInstances == nil {
		return
	}
	instances := map[string]string{}
	for name, conn := range t.machineConns {
		if conn != nil && conn.client != nil {
			instances[name] = conn.client.Instance()
		}
	}
	_ = t.savedInstances.Set(instances)
}

// hasMachinesBlock reports whether the layout asks for the Machines panel.
func (t *Plugin) hasMachinesBlock(block *config.Block) bool {
	if block == nil {
		return false
	}
	if block.Type == "machines" && !block.Hidden {
		return true
	}
	for i := range block.Children {
		if t.hasMachinesBlock(&block.Children[i]) {
			return true
		}
	}
	return false
}

// connectPanelMachines connects every configured machine when the layout
// shows the Machines panel, so the panel has terminals to list. Without the
// panel, machines connect only when a space asks for them.
func (t *Plugin) connectPanelMachines() {
	if !t.hasMachinesBlock(t.layoutTree()) {
		return
	}
	names := make([]string, 0, len(t.machineDefs))
	for name := range t.machineDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := t.ensureMachine(name); err == nil {
			t.autoImportSpaces(name)
		}
	}
}

// adoptMachineForegrounds feeds each attached remote pane its core terminal's
// foreground program, so programs.json can adopt an agent there too.
func (t *Plugin) adoptMachineForegrounds(machine string, terminals map[int]core.Record) {
	for _, s := range t.allSessions() {
		if s == nil || s.machine != machine || !s.remote {
			continue
		}
		rec, ok := terminals[s.coreID]
		if !ok || rec.Foreground == s.foreground {
			continue
		}
		s.foreground = rec.Foreground
		t.adoptAgent(s)
	}
}
