package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"wada-terminal/internal/core"
)

// spaceDocument is the layout document a core keeps: the spaces that run on
// that machine, with their tabs and splits.
type spaceDocument struct {
	Spaces []workspaceSpec `json:"spaces"`
}

// exportSpaces writes this window's spaces for machine into that core's
// layout document. An empty machine means this computer's core.
func (t *Plugin) exportSpaces(machine string) (int, error) {
	client := t.core
	if machine != "" {
		conn, err := t.ensureMachine(machine)
		if err != nil {
			return 0, err
		}
		client = conn
	}
	if client == nil {
		return 0, fmt.Errorf("no core to save on")
	}
	var doc spaceDocument
	for _, spec := range t.workspaceSpecs() {
		if spec.Machine == machine {
			doc.Spaces = append(doc.Spaces, spec)
		}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return 0, err
	}
	if err := client.SetLayout(body); err != nil {
		return 0, err
	}
	return len(doc.Spaces), nil
}

// importSpaces reads a core's layout document and makes the spaces this
// window does not already show.
func (t *Plugin) importSpaces(machine string) (int, error) {
	client := t.core
	if machine != "" {
		conn, err := t.ensureMachine(machine)
		if err != nil {
			return 0, err
		}
		client = conn
	}
	if client == nil {
		return 0, fmt.Errorf("no core to read")
	}
	body, err := client.Layout()
	if err != nil {
		return 0, err
	}
	return t.importDocument(machine, client, body)
}

// importDocument makes every space in body that this window does not show.
func (t *Plugin) importDocument(machine string, client *core.Client, body []byte) (int, error) {
	if len(body) == 0 {
		return 0, nil
	}
	var doc spaceDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0, fmt.Errorf("layout document: %w", err)
	}
	conn := t.machineConns[machine]
	sameCore := false
	if machine == "" {
		sameCore = t.core != nil && t.savedInstance != nil && t.savedInstance.Get() == t.core.Instance()
	} else if conn != nil && conn.client != nil {
		sameCore = t.machineInstance(machine) == conn.instance
		client = conn.client
	}
	added := 0
	for _, spec := range doc.Spaces {
		spec.Machine = machine
		if t.workspaceNamed(machine, spec.Name) != nil {
			continue
		}
		t.workspaces = append(t.workspaces, t.buildWorkspace(spec, client, sameCore, len(t.workspaces)))
		added++
	}
	if added > 0 {
		t.saveWorkspaces()
		if t.ctx != nil {
			t.ctx.Invalidate()
		}
	}
	return added, nil
}

// autoImportSpaces reads a connected machine's document in the background
// and adds the spaces this window does not have. It never blocks a frame.
func (t *Plugin) autoImportSpaces(machine string) {
	conn := t.machineConns[machine]
	if conn == nil || conn.client == nil {
		return
	}
	client := conn.client
	t.ctx.Spawn(func(context.Context) {
		body, _, err := client.LayoutWithin(time.Second)
		if err != nil || len(body) == 0 {
			return
		}
		t.ctx.Post(func() {
			_, _ = t.importDocument(machine, client, body)
		})
	})
}

// publishRemoteLayouts writes each connected machine's spaces into its core
// in the background, so a layout edit reaches the machine's own document.
func (t *Plugin) publishRemoteLayouts() {
	if t.ctx == nil {
		return
	}
	specs := t.workspaceSpecs()
	for machine, conn := range t.machineConns {
		if conn == nil || conn.client == nil {
			continue
		}
		doc := spaceDocument{}
		for _, spec := range specs {
			if spec.Machine == machine {
				doc.Spaces = append(doc.Spaces, spec)
			}
		}
		if len(doc.Spaces) == 0 {
			continue // never erase a document this window knows nothing about
		}
		body, err := json.Marshal(doc)
		if err != nil {
			continue
		}
		client := conn.client
		t.ctx.Spawn(func(context.Context) {
			_ = client.SetLayoutAtWithin(body, 0, time.Second)
		})
	}
}

// workspaceNamed finds a space by its machine and name.
func (t *Plugin) workspaceNamed(machine, name string) *workspace {
	for _, w := range t.workspaces {
		if w.machine == machine && w.name == name {
			return w
		}
	}
	return nil
}
