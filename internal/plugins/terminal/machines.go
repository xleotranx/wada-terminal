package terminal

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
)

// machineConn is one machine's core while the window is open: its client, the
// terminals it had when the window connected, and why it could not connect.
type machineConn struct {
	name          string
	client        *core.Client
	terminals     map[int]core.Record
	instance      string
	err           error
	at            time.Time
	connecting    bool
	waiters       []func(*core.Client, error)
	restore       []workspaceSpec
	savedInstance string
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
		if conn := t.machineConns[spec.Machine]; conn != nil && conn.connecting {
			for _, saved := range t.saved.Get() {
				if saved.Machine == spec.Machine {
					conn.restore = append(conn.restore, saved)
				}
			}
		}
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
		if conn.connecting {
			return nil, fmt.Errorf("machine %s is connecting", name)
		}
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
	conn := &machineConn{name: name, connecting: true, at: time.Now(), savedInstance: t.machineInstance(name)}
	t.machineConns[name] = conn
	t.ctx.Spawn(func(ctx context.Context) {
		client, err := core.ConnectMachineContext(ctx, definition.SSH, definition.Bin, os.Stderr)
		var list core.List
		if err == nil {
			list, err = client.List()
		}
		t.ctx.Post(func() {
			if t.machineConns[name] != conn {
				return
			}
			conn.connecting = false
			conn.err = err
			conn.at = time.Now()
			if err == nil {
				conn.client = client
				conn.instance = client.Instance()
				conn.terminals = map[int]core.Record{}
				for _, rec := range list.Terminals {
					conn.terminals[rec.ID] = rec
				}
				// Startup placeholders did not launch programs. Restore only after the
				// remote instance is known, so existing terminals keep their identities.
				for _, spec := range conn.restore {
					for i, w := range t.workspaces {
						if w.machine != name || w.name != spec.Name {
							continue
						}
						for _, s := range w.sessions(nil) {
							s.kill()
						}
						restored := t.buildWorkspace(spec, client, conn.savedInstance == conn.instance, i)
						restored.name = w.name
						t.workspaces[i] = restored
						if t.current == w {
							t.current = restored
						}
						break
					}
				}
				t.watchMachine(name, client)
			}
			conn.restore = nil
			for _, ready := range conn.waiters {
				ready(client, err)
			}
			conn.waiters = nil
			t.saveWorkspaces()
		})
	})
	return nil, fmt.Errorf("machine %s is connecting", name)
}

// whenMachine completes on the UI thread once a connection attempt ends.
func (t *Plugin) whenMachine(name string, ready func(*core.Client, error)) {
	client, err := t.ensureMachine(name)
	if conn := t.machineConns[name]; conn != nil && conn.connecting {
		conn.waiters = append(conn.waiters, ready)
		return
	}
	ready(client, err)
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
					conn := t.machineConns[name]
					if conn == nil || conn.client != client {
						return
					}
					if conn.err == nil && sameTerminals(conn.terminals, terminals) {
						t.adoptMachineForegrounds(name, terminals)
						return // a poll that found nothing draws nothing
					}
					conn.terminals = terminals
					conn.err = nil
					t.adoptMachineForegrounds(name, terminals)
					t.ctx.Invalidate()
				})
			}
		}
	})
}

// sameTerminals reports whether two lists of a machine's terminals are the
// same, so a poll that found nothing changes nothing on screen.
func sameTerminals(a, b map[int]core.Record) bool { return reflect.DeepEqual(a, b) }

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
func (t *Plugin) hasMachinesBlock(block *config.Block) bool { return t.visibleBlock(block, "machines") }

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
		t.whenMachine(name, func(_ *core.Client, err error) {
			if err == nil {
				t.autoImportSpaces(name)
			}
		})
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
