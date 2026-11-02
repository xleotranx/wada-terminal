package terminal

import (
	"path/filepath"

	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
	"wada-terminal/internal/vt"
)

// workspaceSpec is what the window saves of a workspace between runs: its
// name, folder, tabs, and the layout of each tab. A pane names its terminal in
// the core, so a new window can attach to the same program instead of running
// a new one. Older saved data has only a name and folder: such a workspace
// opens with one shell, as it did before the core existed.
type workspaceSpec struct {
	Name    string    `json:"name"`
	Dir     string    `json:"dir"`
	Machine string    `json:"machine,omitempty"`
	Color   int       `json:"color,omitempty"`
	Active  int       `json:"active,omitempty"`
	Tabs    []tabSpec `json:"tabs,omitempty"`
}

// tabSpec is one tab: its split tree and the terminal with the keyboard.
type tabSpec struct {
	Active bool      `json:"active,omitempty"`
	Focus  int       `json:"focus,omitempty"`
	Root   *nodeSpec `json:"root,omitempty"`
}

// nodeSpec is a leaf pane or a split of two nodes.
type nodeSpec struct {
	Split *splitSpec `json:"split,omitempty"`
	Pane  *paneSpec  `json:"pane,omitempty"`
}

// splitSpec keeps how a tab was divided.
type splitSpec struct {
	Vertical bool      `json:"vertical,omitempty"`
	Ratio    float32   `json:"ratio,omitempty"`
	A        *nodeSpec `json:"a,omitempty"`
	B        *nodeSpec `json:"b,omitempty"`
}

// paneSpec names a terminal and knows the kind, folder and size it had when
// the layout was saved.
type paneSpec struct {
	Terminal   int      `json:"terminal,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Dir        string   `json:"dir,omitempty"`
	Cols       int      `json:"cols,omitempty"`
	Rows       int      `json:"rows,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	AgentName  string   `json:"agentName,omitempty"`
	AgentArgv  []string `json:"agentArgv,omitempty"`
	AgentGlyph string   `json:"agentGlyph,omitempty"`
	AgentColor string   `json:"agentColor,omitempty"`
}

// saveWorkspaces writes what the window remembers: every workspace, its tabs,
// their splits, and the core terminal under each pane.
func (t *Plugin) saveWorkspaces() {
	if t.saved == nil {
		return // a session made outside a window, as in a test
	}
	if t.core != nil && t.savedInstance != nil {
		_ = t.savedInstance.Set(t.core.Instance())
	}
	t.recordMachineInstances()
	t.saved.Set(t.workspaceSpecs())
	t.publishRemoteLayouts()
}

// workspaceSpecs describes every space, tab, split and terminal.
func (t *Plugin) workspaceSpecs() []workspaceSpec {
	specs := make([]workspaceSpec, 0, len(t.workspaces))
	for _, w := range t.workspaces {
		spec := workspaceSpec{Name: w.name, Dir: w.dir, Machine: w.machine, Color: w.color, Active: w.active}
		for _, tb := range w.tabs {
			spec.Tabs = append(spec.Tabs, tabSpec{
				Active: tb == w.currentTab(), Focus: t.terminalID(tb.focus),
				Root: t.saveNode(tb.root),
			})
		}
		specs = append(specs, spec)
	}
	return specs
}

// saveNode turns a tab's tree into what is stored.
func (t *Plugin) saveNode(n *node) *nodeSpec {
	if n == nil {
		return nil
	}
	if n.leaf != nil {
		pane := &paneSpec{
			Terminal: t.terminalID(n.leaf),
			Kind:     n.leaf.kind.apiName(),
			Dir:      n.leaf.cwd(),
			Cols:     n.leaf.cols,
			Rows:     n.leaf.rows,
		}
		if n.leaf.agent {
			pane.Agent = n.leaf.agentID
			pane.AgentName = n.leaf.name
			pane.AgentArgv = append([]string(nil), n.leaf.argv...)
			pane.AgentGlyph = n.leaf.agentGlyph
			pane.AgentColor = n.leaf.agentColor
		}
		return &nodeSpec{Pane: pane}
	}
	return &nodeSpec{Split: &splitSpec{
		Vertical: n.split.vertical,
		Ratio:    n.split.ratio,
		A:        t.saveNode(n.split.a),
		B:        t.saveNode(n.split.b),
	}}
}

// terminalID is how the layout names a pane: the core's id when this window
// is a client of one, else the session's own id.
func (t *Plugin) terminalID(s *session) int {
	if s == nil {
		return 0
	}
	if s.remote && s.coreID > 0 {
		return s.coreID
	}
	return s.id
}

// restore rebuilds every workspace from what the window saved, attaching to
// the core's live terminals when they are still there. A terminal that is
// gone starts a shell in its folder, as herdr-app does after a core restart.
func (t *Plugin) restore() {
	specs := t.saved.Get()
	if len(specs) == 0 {
		t.seedDemo()
		t.saveWorkspaces()
		return
	}
	for i, spec := range specs {
		client, sameCore := t.coreForSpec(spec)
		t.workspaces = append(t.workspaces, t.buildWorkspace(spec, client, sameCore, i))
	}
	t.current = t.workspaces[0]
	if tb := t.current.currentTab(); tb != nil {
		t.current.focus = tb.focus
	}
	// Record the restored layout at once: it may name new terminals for
	// panes the core had lost, and it ties the layout to this core instance.
	t.saveWorkspaces()
}

// restoreNode rebuilds a split tree, or one pane.
func (t *Plugin) restoreNode(ns *nodeSpec, machine string, client *core.Client, sameCore bool) *node {
	if ns == nil {
		return nil
	}
	if ns.Pane != nil {
		if s := t.restorePane(ns.Pane, machine, client, sameCore); s != nil {
			return leafOf(s)
		}
		return nil
	}
	if ns.Split == nil {
		return nil
	}
	a := t.restoreNode(ns.Split.A, machine, client, sameCore)
	b := t.restoreNode(ns.Split.B, machine, client, sameCore)
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	}
	ratio := ns.Split.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	return &node{split: &branch{vertical: ns.Split.Vertical, ratio: ratio, a: a, b: b}}
}

// restorePane attaches to the saved terminal, or starts a shell when the core
// no longer has it (or this window is not using a core).
func (t *Plugin) restorePane(ps *paneSpec, machine string, client *core.Client, sameCore bool) *session {
	if sameCore && ps.Terminal > 0 && client != nil {
		terminals := t.coreTerminals
		if machine != "" {
			if conn := t.machineConns[machine]; conn != nil {
				terminals = conn.terminals
			}
		}
		if rec, ok := terminals[ps.Terminal]; ok && rec.Alive {
			if s := t.attachRecord(rec, machine, client); s != nil {
				return s
			}
		}
	}
	dir := ps.Dir
	if dir == "" && machine == "" {
		dir = launchDir()
	}
	cols, rows := ps.Cols, ps.Rows
	if cols < 2 || rows < 2 {
		cols, rows = t.defaultSize()
	}
	if ps.Agent != "" {
		def := config.Agent{Name: ps.AgentName, Command: ps.AgentArgv, Glyph: ps.AgentGlyph,
			Color: ps.AgentColor}
		if configured, ok := t.agentDefs[ps.Agent]; ok {
			def = configured
		}
		if def.Name == "" {
			def.Name = ps.Agent
		}
		if len(def.Command) == 0 {
			def.Command = []string{ps.Agent}
		}
		if def.Glyph == "" {
			def.Glyph = "✦"
		}
		return t.startProgram(def, ps.Agent, machine, client, dir, cols, rows)
	}
	return t.startAt(machine, client, kindShell, dir, cols, rows)
}

// attachRecord makes a session for a terminal the core already runs, with the
// screen the program last drew.
func (t *Plugin) attachRecord(rec core.Record, machine string, client *core.Client) *session {
	k := kindFromName(rec.Kind)
	sp := k.spec()
	cols, rows := rec.Cols, rec.Rows
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	s := &session{t: t, id: t.nextID, kind: k, dir: rec.Dir, cols: cols, rows: rows,
		machine: machine, client: client,
		screen: vt.New(cols, rows), alive: true, name: sp.name, detail: sp.detail,
		mouseBtn: -1, seen: true, wordProgress: -1}
	t.nextID++
	if rec.Name != "" {
		s.name = rec.Name
	}
	if len(rec.Argv) > 0 {
		s.detail = filepath.Base(rec.Argv[0])
	}
	if !t.attachRemote(s, rec.ID) {
		s.alive = false
		s.status = "The core has this terminal, but it could not be attached."
		return nil
	}
	return s
}

// focusFor picks the saved focused session, or the first one.
func focusFor(sessions []*session, saved int) *session {
	if len(sessions) == 0 {
		return nil
	}
	for _, s := range sessions {
		if s.id == saved || (s.remote && s.coreID == saved) {
			return s
		}
	}
	return sessions[0]
}

// kindFromName turns a saved or reported kind back into the session's kind.
func kindFromName(name string) kind {
	switch name {
	case "claude":
		return kindClaude
	case "codex":
		return kindCodex
	case "opencode":
		return kindOpenCode
	default:
		return kindShell
	}
}

// coreForSpec picks the core a saved space runs on and whether its terminal
// ids still mean what they meant.
func (t *Plugin) coreForSpec(spec workspaceSpec) (*core.Client, bool) {
	if spec.Machine == "" {
		return t.core, t.core != nil && t.savedInstance != nil && t.savedInstance.Get() == t.core.Instance()
	}
	conn := t.machineConns[spec.Machine]
	if conn == nil {
		return nil, false
	}
	return conn.client, conn.client != nil && t.machineInstance(spec.Machine) == conn.instance
}

// buildWorkspace makes a live space from a saved one.
func (t *Plugin) buildWorkspace(spec workspaceSpec, client *core.Client, sameCore bool, color int) *workspace {
	w := &workspace{id: t.nextID, name: t.uniqueName(spec.Name), dir: spec.Dir,
		machine: spec.Machine, client: client, color: spec.Color}
	t.nextID++
	if w.color == 0 {
		w.color = (color + 1) % 5
	}
	if w.dir == "" && w.machine == "" {
		w.dir = homeDir()
	}
	for _, ts := range spec.Tabs {
		tb := &tab{id: t.nextID}
		t.nextID++
		tb.root = t.restoreNode(ts.Root, spec.Machine, client, sameCore)
		if tb.root == nil {
			cols, rows := t.defaultSize()
			tb.root = leafOf(t.startAt(spec.Machine, client, kindShell, w.dir, cols, rows))
		}
		tb.focus = focusFor(tb.root.sessions(nil), ts.Focus)
		w.tabs = append(w.tabs, tb)
	}
	if len(w.tabs) == 0 {
		cols, rows := t.defaultSize()
		w.addTab(t.startAt(spec.Machine, client, kindShell, w.dir, cols, rows), true)
	}
	w.active = min(max(spec.Active, 0), len(w.tabs)-1)
	w.focus = w.tabs[w.active].focus
	return w
}
