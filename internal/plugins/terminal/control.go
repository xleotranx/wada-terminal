package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"wada-terminal/internal/control"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// startControl opens the window's control socket for agents and scripts: the
// window's half of herdr-app's `do`/`ls`/`status`. It listens in a worker, and
// every request is handled on the UI thread, where the window's state lives.
// A window whose data folder is unknown (a test with no DataDir) has no
// socket.
func (t *Plugin) startControl() {
	dir := t.host.DataDir()
	if dir == "" {
		return
	}
	scope := t.ctx.NewScope()
	scope.Spawn(func(ctx context.Context) {
		ln, err := control.Listen(dir)
		if err != nil {
			scope.Post(func() {
				t.host.Toast(ui.Toast{Title: "Agents cannot drive the window",
					Description: err.Error(), Type: components.ToastError})
			})
			return
		}
		// A second worker closes the listener when the window ends, which
		// unblocks the accept loop below.
		scope.Spawn(func(ctx context.Context) {
			<-ctx.Done()
			_ = ln.Close()
		})
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			scope.Spawn(func(ctx context.Context) { t.serveControl(ctx, conn) })
		}
	})
}

// serveControl answers one connection. The request is handed to the UI thread
// and its answer comes back here, so a worker never touches the window state.
func (t *Plugin) serveControl(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	req, err := control.ReadRequest(conn)
	if err != nil {
		control.WriteResponse(conn, control.Response{Error: err.Error()})
		return
	}
	reply := make(chan control.Response, 1)
	t.ctx.Post(func() { reply <- t.controlResponse(req) })
	select {
	case resp := <-reply:
		control.WriteResponse(conn, resp)
	case <-ctx.Done():
	}
}

// controlResponse is one request on the UI thread: the window's status, its
// sessions, the action list, or an action. Errors are answers, not failures of
// the window.
func (t *Plugin) controlResponse(req control.Request) control.Response {
	switch req.Method {
	case "status":
		return jsonAnswer(t.controlStatus())
	case "ls":
		return jsonAnswer(t.controlList())
	case "commands":
		return jsonAnswer(control.Actions)
	case "shot":
		capturer, ok := t.host.(interface {
			Capture() ([]byte, error)
		})
		if !ok {
			return control.Response{Error: "this window cannot be captured"}
		}
		png, err := capturer.Capture()
		if err != nil {
			return control.Response{Error: err.Error()}
		}
		return jsonAnswer(map[string]string{"png": base64.StdEncoding.EncodeToString(png)})
	case "do":
		action, ok := control.FindAction(req.Action)
		if !ok {
			return control.Response{Error: fmt.Sprintf("no action %q; run `commands` to list them", req.Action)}
		}
		if err := control.Validate(action, req.Args); err != nil {
			return control.Response{Error: err.Error()}
		}
		result, err := t.controlDo(action.Name, req.Args)
		if err != nil {
			return control.Response{Error: err.Error()}
		}
		return jsonAnswer(result)
	default:
		return control.Response{Error: fmt.Sprintf("no method %q; use status, ls, commands or do", req.Method)}
	}
}

// jsonAnswer marshals a result as a response.
func jsonAnswer(v any) control.Response {
	body, err := json.Marshal(v)
	if err != nil {
		return control.Response{Error: err.Error()}
	}
	return control.Response{Result: body}
}

// controlStatus builds the answer to `status`.
func (t *Plugin) controlStatus() control.Status {
	status := control.Status{Window: true, IndexOpen: t.indexOpen, Attention: t.attentionCount()}
	if dir := t.host.DataDir(); dir != "" {
		status.Socket = control.SocketPath(dir)
	}
	if t.core != nil {
		status.Core, status.CoreInstance = true, t.core.Instance()
	}
	if t.layoutStacks != nil {
		if stacks := t.layoutStacks.Get(); len(stacks) > 0 {
			status.Stacks = stacks
		}
	}
	if t.floatOffsets != nil {
		if floats := t.floatOffsets.Get(); len(floats) > 0 {
			status.Floats = floats
		}
	}
	if t.layoutSizes != nil {
		if sizes := t.layoutSizes.Get(); len(sizes) > 0 {
			status.Sizes = sizes
		}
	}
	for _, w := range t.workspaces {
		status.Spaces++
		space := t.controlSpace(w)
		if w == t.current {
			status.Space = &space
			status.Tab = w.active + 1
			if w.focus != nil {
				pane := t.controlPane(w.focus)
				status.Pane = &pane
			}
		}
		status.Tabs += len(w.tabs)
		for _, s := range w.sessions(nil) {
			status.Sessions++
			if s.isAgent() {
				status.Agents++
			}
		}
	}
	return status
}

// controlList builds the answer to `ls`: every space, its tabs, and the panes
// of each, in the order the window draws them.
func (t *Plugin) controlList() control.List {
	list := control.List{Spaces: make([]control.Space, 0, len(t.workspaces))}
	for _, w := range t.workspaces {
		list.Spaces = append(list.Spaces, t.controlSpace(w))
	}
	return list
}

// controlSpace is one space, with its tabs and their panes.
func (t *Plugin) controlSpace(w *workspace) control.Space {
	space := control.Space{ID: w.id, Name: w.name, Folder: w.dir, Machine: w.machine,
		Tabs: make([]control.Tab, 0, len(w.tabs)), Task: w.task}
	space.Active = w == t.current
	for i, tb := range w.tabs {
		tab := control.Tab{Index: i + 1, Active: i == w.active,
			Panes: make([]control.Pane, 0, len(tb.root.sessions(nil)))}
		for _, s := range tb.root.sessions(nil) {
			tab.Panes = append(tab.Panes, t.controlPane(s))
		}
		space.Tabs = append(space.Tabs, tab)
	}
	return space
}

// controlPane is one session as a client sees it: its identity, where it
// works, what it is doing, and its size.
func (t *Plugin) controlPane(s *session) control.Pane {
	pane := control.Pane{
		ID: s.id, Kind: s.kind.apiName(), Title: s.title(), Detail: s.subtitle(),
		Cwd: s.cwd(), Machine: s.machine, Focused: t.focused(s), Agent: s.isAgent(),
		App: s.app, Word: s.statusWord(), Message: s.statusMsg, State: s.apiState(),
		Cols: s.cols, Rows: s.rows, ScrollMode: s.scrollMode, Scrolled: s.screen.Scrolled(),
		Visual: s.viVisual, VisualLinewise: s.viLinewise,
		VisualFrom: min(s.viAnchor, s.viCursor), VisualTo: max(s.viAnchor, s.viCursor),
		VisualFromCol: visualFromCol(s), VisualToCol: s.viCursorCol,
	}
	if s.reported && s.wordProgress >= 0 && (s.word == "working" || s.word == "blocked") {
		pane.Progress = s.wordProgress
	}
	return pane
}

// controlDo runs one action and returns its result: ids for what it made, so
// a caller can use the new space, tab or pane next.
func (t *Plugin) controlDo(name string, args map[string]any) (any, error) {
	switch name {
	case "layout.resize":
		id, _ := control.String(args, "id")
		px, ok := control.Int(args, "px")
		if id == "" || !ok {
			return nil, fmt.Errorf("layout.resize needs id and px")
		}
		block, path := t.blockByID(t.layoutTree(), id, "root")
		if block == nil {
			return nil, fmt.Errorf("no block %q in this layout", id)
		}
		if block.Size == nil || block.Size.Px <= 0 {
			return nil, fmt.Errorf("block %q does not have a fixed size", id)
		}
		t.setLayoutSize(t.blockKey(block, path), float32(px), block)
		return map[string]any{"id": id, "px": t.blockPx(block, path)}, nil

	case "layout.stack":
		stack, _ := control.String(args, "stack")
		active, _ := control.String(args, "active")
		if stack == "" || active == "" {
			return nil, fmt.Errorf("layout.stack needs stack and active")
		}
		shown, err := t.switchStack(stack, active)
		if err != nil {
			return nil, err
		}
		return map[string]any{"stack": stack, "active": shown}, nil

	case "layout.stack.next":
		stack, _ := control.String(args, "stack")
		if stack == "" {
			return nil, fmt.Errorf("layout.stack.next needs a stack")
		}
		step := 1
		if n, ok := control.Int(args, "step"); ok {
			step = n
		}
		shown, err := t.cycleStack(stack, step)
		if err != nil {
			return nil, err
		}
		return map[string]any{"stack": stack, "active": shown}, nil

	case "task.new":
		name, _ := control.String(args, "name")
		brief, _ := control.String(args, "brief")
		folder, _ := control.String(args, "folder")
		agent, _ := control.String(args, "agent")
		if agent == "" {
			agent = "shell"
		}
		if err := t.createTask(name, brief, folder, agent); err != nil {
			return nil, err
		}
		return map[string]any{"started": true}, nil
	case "task.finish":
		if t.current == nil || t.current.task == nil {
			return nil, fmt.Errorf("select a task workspace first")
		}
		t.openFinishTask(t.current)
		return map[string]any{"confirmation": true}, nil
	case "task.review":
		if t.current == nil || t.current.task == nil {
			return nil, fmt.Errorf("select a task workspace first")
		}
		if t.current.review == nil {
			t.current.review = &taskReview{}
		}
		t.current.review.changesView = true
		t.reviewTask(t.current)
		return map[string]any{"started": true}, nil
	case "task.check":
		if t.current == nil || t.current.task == nil {
			return nil, fmt.Errorf("select a task workspace first")
		}
		if t.current.finishing {
			return nil, fmt.Errorf("task is finishing")
		}
		if t.current.review != nil && t.current.review.checkJob.Running() {
			return nil, fmt.Errorf("a check is already running for this task")
		}
		command, _ := control.String(args, "command")
		if strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("enter a check command")
		}
		t.checkTask(t.current, command)
		return map[string]any{"started": true}, nil
	case "space.new":
		machine, _ := control.String(args, "machine")
		if machine == "local" {
			machine = ""
		}
		folder, _ := control.String(args, "folder")
		if folder == "" && machine == "" {
			folder = t.focusedDir()
		}
		label, _ := control.String(args, "name")
		if label == "" {
			if machine != "" && folder == "" {
				label = machine
			} else {
				label = filepath.Base(folder)
			}
		}
		w := t.addWorkspaceOn(label, folder, machine)
		result := map[string]any{"space": w.id}
		if machine != "" {
			result["machine"] = machine
		}
		return result, nil

	case "space.close":
		w := t.current
		if id, ok := control.Int(args, "id"); ok {
			w = t.workspaceByID(id)
		}
		if w == nil {
			return nil, fmt.Errorf("no space")
		}
		if w.task != nil {
			t.openFinishTask(w)
			return map[string]any{"space": w.id, "confirmation": true}, nil
		}
		if len(t.workspaces) <= 1 {
			return nil, fmt.Errorf("keep one space")
		}
		t.closeWorkspace(w)
		return map[string]any{"space": w.id}, nil

	case "space.select":
		w, err := t.spaceFrom(args)
		if err != nil {
			return nil, err
		}
		t.current = w
		if w.focus == nil {
			if tb := w.currentTab(); tb != nil {
				w.focus = tb.focus
			}
		}
		t.seen(w.focus)
		t.saveWorkspaces()
		return map[string]any{"space": w.id}, nil

	case "space.export":
		machine, _ := control.String(args, "machine")
		if machine == "local" {
			machine = ""
		}
		n, err := t.exportSpaces(machine)
		if err != nil {
			return nil, err
		}
		return map[string]any{"spaces": n, "machine": machine}, nil

	case "space.import":
		machine, _ := control.String(args, "machine")
		if machine == "local" {
			machine = ""
		}
		n, err := t.importSpaces(machine)
		if err != nil {
			return nil, err
		}
		return map[string]any{"spaces": n, "machine": machine}, nil

	case "space.next":
		t.stepWorkspace(1)
		return map[string]any{"space": t.current.id}, nil

	case "space.prev":
		t.stepWorkspace(-1)
		return map[string]any{"space": t.current.id}, nil

	case "tab.new":
		w := t.current
		k, err := kindFrom(args)
		if err != nil {
			return nil, err
		}
		folder, _ := control.String(args, "folder")
		if folder == "" {
			folder = t.focusedDir()
		}
		cols, rows := t.defaultSize()
		s := t.startAt(w.machine, w.client, k, folder, cols, rows)
		w.addTab(s, true)
		t.saveWorkspaces()
		return map[string]any{"space": w.id, "tab": w.active + 1, "pane": s.id}, nil

	case "tab.close":
		w := t.current
		index := w.active
		if n, ok := control.Int(args, "index"); ok {
			index = n - 1
		}
		if index < 0 || index >= len(w.tabs) {
			return nil, fmt.Errorf("no tab %d", index+1)
		}
		t.closeTab(index)
		return map[string]any{"space": w.id, "tab": w.active + 1}, nil

	case "tab.select":
		w := t.current
		n, _ := control.Int(args, "index")
		if n < 1 || n > len(w.tabs) {
			return nil, fmt.Errorf("no tab %d", n)
		}
		w.selectTab(n - 1)
		t.seen(w.focus)
		return map[string]any{"space": w.id, "tab": n}, nil

	case "tab.next":
		return t.stepTab(1), nil

	case "tab.prev":
		return t.stepTab(-1), nil

	case "pane.split":
		direction, _ := control.String(args, "direction")
		if direction != "right" && direction != "down" {
			return nil, fmt.Errorf("direction is right or down, not %q", direction)
		}
		k, err := kindFrom(args)
		if err != nil {
			return nil, err
		}
		s, err := t.paneFrom(args)
		if err != nil {
			return nil, err
		}
		_, tb, _ := t.tabOf(s)
		cols, rows := t.defaultSize()
		next := t.startAt(s.machine, s.client, k, s.cwd(), cols, rows)
		root, ok := splitAt(tb.root, s, next, direction == "right")
		if !ok {
			next.kill()
			return nil, fmt.Errorf("the pane cannot be split")
		}
		tb.root = root
		tb.focus = next
		t.focusOn(next)
		t.saveWorkspaces()
		return map[string]any{"pane": next.id}, nil

	case "pane.close":
		s, err := t.paneFrom(args)
		if err != nil {
			return nil, err
		}
		t.close(s)
		return map[string]any{"pane": s.id}, nil

	case "pane.focus":
		s, err := t.paneFrom(args)
		if err != nil {
			return nil, err
		}
		t.focusOn(s)
		return map[string]any{"pane": s.id}, nil

	case "terminal.send":
		s, err := t.paneFrom(args)
		if err != nil {
			return nil, err
		}
		text, _ := control.String(args, "text")
		submit := true
		if v, ok := control.Bool(args, "submit"); ok {
			submit = v
		}
		if submit {
			text += "\r"
		}
		s.write([]byte(text))
		return map[string]any{"pane": s.id, "bytes": len(text)}, nil

	case "index.open":
		t.openIndex()
		return map[string]any{"open": true}, nil

	case "index.close":
		t.indexOpen = false
		return map[string]any{"open": false}, nil
	}
	return nil, fmt.Errorf("no action %q", name)
}

// spaceFrom finds a space by id or 1-based index.
func (t *Plugin) spaceFrom(args map[string]any) (*workspace, error) {
	if id, ok := control.Int(args, "id"); ok {
		if w := t.workspaceByID(id); w != nil {
			return w, nil
		}
		return nil, fmt.Errorf("no space %d", id)
	}
	if n, ok := control.Int(args, "index"); ok {
		if n >= 1 && n <= len(t.workspaces) {
			return t.workspaces[n-1], nil
		}
		return nil, fmt.Errorf("no space %d", n)
	}
	return nil, fmt.Errorf("name a space by id or index")
}

// paneFrom finds a pane by id, or the focused one when none is named.
func (t *Plugin) paneFrom(args map[string]any) (*session, error) {
	if id, ok := control.Int(args, "pane"); ok {
		if s := t.sessionByID(id); s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("no pane %d", id)
	}
	if t.current == nil || t.current.focus == nil {
		return nil, fmt.Errorf("no focused pane")
	}
	return t.current.focus, nil
}

// sessionByID finds a session by the id ls prints.
func (t *Plugin) sessionByID(id int) *session {
	for _, s := range t.allSessions() {
		if s.id == id {
			return s
		}
	}
	return nil
}

// workspaceByID finds a space by id.
func (t *Plugin) workspaceByID(id int) *workspace {
	for _, w := range t.workspaces {
		if w.id == id {
			return w
		}
	}
	return nil
}

// focusedDir is where a new session in the focused pane's place starts.
func (t *Plugin) focusedDir() string {
	if t.current != nil && t.current.focus != nil {
		return t.current.focus.cwd()
	}
	if t.current != nil {
		return t.current.dir
	}
	return launchDir()
}

// stepTab shows a tab delta places along the shown space's tabs.
func (t *Plugin) stepTab(delta int) map[string]any {
	w := t.current
	if w == nil || len(w.tabs) == 0 {
		return map[string]any{"tab": 0}
	}
	next := (w.active + delta + len(w.tabs)) % len(w.tabs)
	w.selectTab(next)
	t.seen(w.focus)
	t.saveWorkspaces()
	return map[string]any{"space": w.id, "tab": next + 1}
}

// kindFrom reads the action's kind, defaulting to a shell.
func kindFrom(args map[string]any) (kind, error) {
	name, _ := control.String(args, "kind")
	switch name {
	case "", "shell":
		return kindShell, nil
	case "claude":
		return kindClaude, nil
	case "codex":
		return kindCodex, nil
	case "opencode":
		return kindOpenCode, nil
	}
	return kindShell, fmt.Errorf("kind is claude, codex, opencode or shell, not %q", name)
}

// apiName is how a session's kind is spelled to a control client.
func (k kind) apiName() string {
	switch k {
	case kindClaude:
		return "claude"
	case kindCodex:
		return "codex"
	case kindOpenCode:
		return "opencode"
	default:
		return "shell"
	}
}

// apiState is how a session's state is spelled to a control client. A shell
// that is not an agent is running or stopped; an agent uses the four states.
func (s *session) apiState() string {
	if !s.alive {
		return "stopped"
	}
	if !s.isAgent() {
		return "running"
	}
	switch s.agentState() {
	case agentWorking:
		return "working"
	case agentBlocked:
		return "blocked"
	case agentDone:
		return "done"
	default:
		return "idle"
	}
}

// visualFromCol is the anchor's column, swapped with the cursor's when the
// selection runs upwards or backwards.
func visualFromCol(s *session) int {
	if s.viAnchor > s.viCursor || (s.viAnchor == s.viCursor && s.viAnchorCol > s.viCursorCol) {
		return s.viCursorCol
	}
	return s.viAnchorCol
}
