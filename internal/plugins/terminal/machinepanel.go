package terminal

import (
	"fmt"
	"sort"

	"wada-terminal/internal/core"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// machinesPanel lists every configured machine and the terminals its core
// holds, including ones started outside this window with `core --on`.
func (t *Plugin) machinesPanel(c *ui.Context, ch chrome) ui.Element {
	pal := ch.pal
	names := make([]string, 0, len(t.machineDefs))
	for name := range t.machineDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	panel := ui.Column(c).MinWidth(0).MinHeight(0).Background(pal.DrawerBg)
	panel.Children(func() {
		ui.Row(c).Height(30).FillWidth().Shrink(0).Padding(0, 8).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Text(c, "Machines").FontSize(12).FontWeight(600).TextColor(pal.Text)
			ui.Text(c, fmt.Sprint(len(names))).FontSize(9.5).TextColor(pal.Faint)
		})
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 8, 8, 8).Gap(1).Children(func() {
				if len(names) == 0 {
					ui.Text(c, "No machines in machines.json.").FontSize(10.5).TextColor(pal.Faint).
						Padding(0, 4, 0, 4)
					return
				}
				for _, name := range names {
					t.machineSection(c, ch, name)
				}
			})
		})
	})
	return panel
}

// machineSection is one machine's header and the live terminals under it.
func (t *Plugin) machineSection(c *ui.Context, ch chrome, name string) {
	pal := ch.pal
	conn := t.machineConns[name]
	ui.Row(c).Height(24).FillWidth().Shrink(0).Padding(0, 4).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, name).FontSize(11).FontWeight(600).TextColor(pal.Text).Grow(1).
			SingleLine().Ellipsis("…")
		status := "not connected"
		color := pal.Faint
		if conn != nil && conn.client != nil {
			status = fmt.Sprint(len(conn.terminals))
			color = pal.GreenBadge
		}
		ui.Text(c, status).FontSize(9).TextColor(color)
	})
	if conn == nil || conn.client == nil {
		return
	}
	spaces := map[string][]core.Record{}
	for _, rec := range conn.terminals {
		if !rec.Alive {
			continue
		}
		space := rec.Labels["space"]
		spaces[space] = append(spaces[space], rec)
	}
	if len(spaces) == 0 {
		ui.Text(c, "No running terminals.").FontSize(9.5).TextColor(pal.Faint).Padding(0, 4, 3, 4)
		return
	}
	keys := make([]string, 0, len(spaces))
	for key := range spaces {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "") != (keys[j] == "") {
			return keys[i] != ""
		}
		return keys[i] < keys[j]
	})
	for _, space := range keys {
		recs := spaces[space]
		sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
		if space != "" {
			ui.Text(c, space).FontSize(9).FontWeight(600).TextColor(pal.Muted).
				Padding(0, 6, 1, 6).SingleLine().Ellipsis("…")
		}
		for _, rec := range recs {
			t.machineTerminalRow(c, ch, name, rec)
		}
	}
}

// machineTerminalRow opens a machine terminal in this window when clicked.
func (t *Plugin) machineTerminalRow(c *ui.Context, ch chrome, machine string, rec core.Record) {
	pal := ch.pal
	row := ui.Box(c).Height(34).FillWidth().Shrink(0).Radius(6).Padding(0, 5)
	if row.Hovered() {
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer).OnClick(func() { t.openMachineTerminal(machine, rec) })
	title := rec.Title
	if title == "" {
		title = rec.Name
	}
	if title == "" {
		title = fmt.Sprintf("t%d", rec.ID)
	}
	state := rec.Word
	if state == "" {
		state = "running"
	}
	if rec.Foreground != "" {
		state = rec.Foreground
	}
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Width(6).Height(6).Shrink(0).Radius(3).Background(machineStateColor(ch, rec.Word))
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, title).FontSize(10.5).FontWeight(600).TextColor(pal.Text).
					SingleLine().Ellipsis("…")
				subtitle := fmt.Sprintf("t%d · %s", rec.ID, state)
				ui.Text(c, subtitle).FontSize(8.5).TextColor(pal.Faint).
					SingleLine().Ellipsis("…")
			})
		})
	})
}

// machineStateColor colors a machine terminal by its reported word.
func machineStateColor(ch chrome, word string) ui.Color {
	switch word {
	case "working":
		return ch.pal.GreenBadge
	case "blocked", "error":
		return ch.pal.Waiting
	case "done":
		return ch.pal.Link
	default:
		return ch.pal.Faint
	}
}

// openMachineTerminal shows a machine's terminal: it focuses a pane already
// attached to it, or makes a machine workspace and attaches a new pane.
func (t *Plugin) openMachineTerminal(machine string, rec core.Record) {
	space := rec.Labels["space"]
	for _, w := range t.workspaces {
		for i, tb := range w.tabs {
			for _, s := range tb.root.sessions(nil) {
				if s.machine == machine && s.remote && s.coreID == rec.ID {
					t.current, w.active, w.focus = w, i, s
					t.seen(s)
					t.saveWorkspaces()
					return
				}
			}
		}
	}
	client, err := t.ensureMachine(machine)
	if err != nil {
		t.host.Toast(ui.Toast{Title: "Machine " + machine, Description: err.Error(),
			Type: components.ToastError})
		return
	}
	s := t.attachRecord(rec, machine, client)
	if s == nil {
		t.host.Toast(ui.Toast{Title: "Machine " + machine,
			Description: "The terminal could not be attached."})
		return
	}
	s.foreground = rec.Foreground
	t.adoptAgent(s)
	// A labelled terminal belongs to its core's space; opening it on a
	// workspace of that name keeps the space together.
	if space != "" {
		for _, w := range t.workspaces {
			if w.machine == machine && w.name == space {
				w.addTab(s, true)
				t.current, w.focus = w, s
				t.seen(s)
				t.saveWorkspaces()
				return
			}
		}
	}
	name := rec.Title
	if space != "" {
		name = space
	}
	if name == "" {
		name = rec.Name
	}
	if name == "" {
		name = fmt.Sprintf("t%d", rec.ID)
	}
	w := &workspace{id: t.nextID, name: t.uniqueName(name), dir: rec.Dir,
		machine: machine, client: client, color: len(t.workspaces) % 5}
	t.nextID++
	w.addTab(s, true)
	t.workspaces = append(t.workspaces, w)
	t.current = w
	t.saveWorkspaces()
}
