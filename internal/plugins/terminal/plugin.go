// Package terminal is the app's terminal: workspaces of sessions, each a real
// program on a pseudo-terminal, in tabs and split panes.
//
// A session owns a pseudo-terminal, a screen that parses what the program
// writes, and a worker that reads it; the page paints the screen's cells in
// the window's font and gives the keyboard's input back to the program.
package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
	"wada-terminal/internal/tasks"

	"github.com/xleotranx/wada-kit/contract/shell"
	"github.com/xleotranx/wada-kit/kernel"

	"github.com/egoist/mygo"
	mterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// tab is one tab of a workspace: a layout of sessions, one of them focused.
type tab struct {
	name  string // stable arrangement name, independent of the focused program
	id    int
	root  *node
	focus *session
}

// workspace is one folder the window works in: its tabs, the tab on show, and
// the session with the keyboard.
type workspace struct {
	finishing bool
	task      *tasks.Task
	review    *taskReview
	id        int
	name      string
	dir       string // where its sessions start
	machine   string // the SSH machine its terminals run on, empty on this computer
	client    *core.Client
	color     int // an index into the palette's project colors
	tabs      []*tab
	active    int
	focus     *session
}

// Plugin is the terminal: its workspaces, their tabs, and the programs their
// panes run.
type Plugin struct {
	taskInboxOpen  bool
	taskInboxQuery string
	finishUI       finishTaskUI
	integrateUI    integrationUI
	tasksUI        taskUI
	ctx            *kernel.Context
	host           shell.Host
	services       ui.Services

	sidebar        *kernel.Setting[bool]
	font           *kernel.Setting[float64]
	saved          *kernel.Setting[[]workspaceSpec]
	savedInstance  *kernel.Setting[string]
	savedInstances *kernel.Setting[map[string]string]
	layoutStacks   *kernel.Setting[map[string]string]
	floatOffsets   *kernel.Setting[map[string][2]float32]
	floatDrag      map[string]*ui.Point
	floatHover     map[string]bool
	layoutSizes    *kernel.Setting[map[string]float32]
	search         searchState
	links          linkState
	pageW, pageH   float32

	// core is the persistent terminal server, when one is running. Its
	// terminals outlive this window; the saved layout names them by id.
	core          *core.Client
	coreTerminals map[int]core.Record

	// Machines: the files' definitions and the cores this window connected
	// to. A machine's terminals live in its core, not this computer's.
	machineDefs  config.Machines
	machineConns map[string]*machineConn
	agentDefs    config.Agents
	agentIcons   map[string]*ui.SVG

	// The interface as files: where they live, the person's settings, the
	// layout's sidebar, a theme file's palette, and the commands registered
	// from keys.json under their own scope so a save can rebind them.
	configDir    string
	sidebarPx    int
	sidebarAt    string
	theme        *surface
	themeName    string
	layoutName   string
	windowLayout *config.Layout
	keyScope     *kernel.Scope
	keys         config.Keys

	// Mods: the loaded JavaScript panels, the tree each layout block last
	// rendered, and what their console.log calls have said.
	mods           map[string]*loadedMod
	modViews       map[*config.Block]*modView
	modInputs      map[*config.Block]map[string]*string
	modAutoFocused map[*config.Block]map[string]bool
	modFocus       map[*config.Block]map[string]bool
	modLogs        map[string][]string

	// Animating mods ask the window to draw at their own pace. A worker
	// ticks and posts an invalidation only while a frame saw an animation,
	// and sleeps while none does, so an idle window wakes for nothing.
	modAnimated      atomic.Bool
	modAnimatedFrame bool
	modAnimatedWake  chan struct{}

	workspaces []*workspace
	current    *workspace
	nextID     int
	drag       *branch // the split whose divider a drag holds
	pick       kernel.Task
	// workspaceMenu is the switcher at the top of the column being open.
	workspaceMenu bool

	// The Sessions: the overlay over the page, the row it has chosen,
	// whether the chosen row should be scrolled into view, and the watcher
	// that settles a working agent once its pane is quiet.
	indexOpen     bool
	indexSel      int
	indexReveal   bool
	agentWake     chan struct{}
	agentWatching atomic.Bool

	// family is the family list the terminals paint with, worked out once.
	family string

	// paneFocusAsked is the pane last given the keyboard, and overlayWasOpen
	// whether an overlay held it in the last frame (noteOverlay).
	paneFocusAsked *session
	overlayWasOpen bool
}

// defaultFontSize is the terminals' text size until the user zooms.
const defaultFontSize = 12.5

// terminalPage is the terminals' page in shell.Pages; another page, such as
// Settings, replaces it until the window shows this one again.
const terminalPage = "terminal.page"

// New makes the terminal plugin.
func New() kernel.Plugin { return &Plugin{} }

func (*Plugin) Manifest() kernel.Manifest {
	return kernel.Manifest{
		ID: "terminal", Title: "Terminal", Locked: true,
		Description: "Agent and shell sessions on real pseudo-terminals, in workspaces, tabs and split panes.",
		Requires:    []kernel.Named{shell.HostKey},
	}
}

func (t *Plugin) Setup(c *kernel.Context) error {
	t.ctx = c
	t.host = kernel.Require(c, shell.HostKey)
	t.sidebar = kernel.NewSetting(c, "sidebar", true)
	t.font = kernel.NewSetting(c, "font-size", defaultFontSize)
	t.saved = kernel.NewSetting(c, "workspaces", []workspaceSpec{})
	t.savedInstance = kernel.NewSetting(c, "core-instance", "")
	t.savedInstances = kernel.NewSetting(c, "machine-instances", map[string]string{})
	t.layoutStacks = kernel.NewSetting(c, "layout-stacks", map[string]string{})
	t.floatOffsets = kernel.NewSetting(c, "float-offsets", map[string][2]float32{})
	t.layoutSizes = kernel.NewSetting(c, "layout-sizes", map[string]float32{})
	t.floatDrag = map[string]*ui.Point{}
	t.floatHover = map[string]bool{}
	t.machineConns = map[string]*machineConn{}
	// The window is a solid surface with the agents' panes in it, as the
	// design shows. Done once, so a choice made in Settings stands.
	if applied := kernel.NewSetting(c, "solid-window", false); !applied.Get() {
		t.host.SetBackdrop("solid")
		applied.Set(true)
	}

	kernel.Contribute(c, shell.Pages, terminalPage, 0,
		shell.Page{Title: t.pageTitle, Render: t.page})
	kernel.Contribute(c, shell.Strip, "terminal.tabs", 0, shell.View{Grow: true,
		Render: func(c *ui.Context) {
			if t.hasTabsBlock(t.layoutTree()) || t.hasWorkspaceNavigation(t.layoutTree()) {
				return // sidebar navigation or an explicit tabs block owns switching
			}
			t.strip(c)
		}})
	kernel.Contribute(c, shell.TitleBar, "terminal.actions", 0, shell.View{
		Render: func(c *ui.Context) {
			if t.hasTitleBlock(t.layoutTree()) {
				return // the layout places the actions itself
			}
			t.actions(c)
		}})
	kernel.Contribute(c, shell.Overlays, "terminal.links", 5,
		shell.View{Render: t.linkDialog})
	kernel.Contribute(c, shell.Overlays, "terminal.sessions", 10,
		shell.View{Render: t.sessions})

	kernel.Contribute(c, shell.Overlays, "terminal.tasks", 15, shell.View{Render: t.taskDialog})
	kernel.Contribute(c, shell.Overlays, "terminal.task-brief", 16, shell.View{Render: t.taskBriefDialog})
	kernel.Contribute(c, shell.Overlays, "terminal.finish-task", 20, shell.View{Render: t.finishTaskDialog})
	kernel.Contribute(c, shell.Overlays, "terminal.integrate-task", 21, shell.View{Render: t.integrationDialog})
	t.agentWake = make(chan struct{}, 1)
	t.modAnimatedWake = make(chan struct{}, 1)
	t.ctx.Spawn(func(ctx context.Context) { t.watchAgents(ctx) })
	t.ctx.Spawn(t.watchTaskChanges)
	t.ctx.Spawn(func(ctx context.Context) { t.watchForegrounds(ctx) })
	t.startModAnimation()
	t.loadConfig()
	t.connectCore()
	t.connectSavedMachines()
	t.restore()
	t.startControl()
	t.watchConfig()
	if os.Getenv("WADATERM_INDEX") != "" {
		// A capture can show the index without opening it by hand.
		t.openIndex()
	}
	if os.Getenv("WADATERM_TASKS") != "" {
		t.openTaskInbox()
	}
	if os.Getenv("WADATERM_WORKSPACES") != "" {
		// A capture can show the workspace menu open.
		t.workspaceMenu = true
	}
	return nil
}

// dark reports whether the window shows the dark appearance, as a program
// starting now should be told: the setting, or what a capture fixed it to.
func (t *Plugin) dark() bool {
	if v := os.Getenv("WADATERM_APPEARANCE"); v == "light" {
		return false
	}
	return t.host.Appearance() != "light"
}

// surface is the terminal palette of that appearance.
func (t *Plugin) surface() surface {
	if t.theme != nil {
		return *t.theme
	}
	if t.dark() {
		return darkSurface
	}
	return lightSurface
}

// fontFamily is the family list the cells paint with: the system's monospaced
// font, with a Nerd Font after it when one is installed, which is where a
// terminal program's icons live.
func (t *Plugin) fontFamily() string {
	if t.family != "" {
		return t.family
	}
	family := "monospace"
	for _, f := range ui.FontFamilies() {
		if strings.Contains(strings.ToLower(f.Name), "nerd font") {
			family = "monospace, " + f.Name
			break
		}
	}
	t.family = family
	return family
}

// hasIconFont reports whether the cells paint with a font beside the system's
// monospaced one, which is the Nerd Font that carries a program's icons.
func (t *Plugin) hasIconFont() bool {
	return strings.Contains(t.fontFamily(), ",")
}

// focused reports whether the keyboard is on a session.
func (t *Plugin) focused(s *session) bool {
	return t.current != nil && t.current.focus == s
}

// currentTab is the tab on show of the workspace on show.
func (t *Plugin) currentTab() *tab {
	if t.current == nil {
		return nil
	}
	return t.current.currentTab()
}

// pageTitle is the window's title: the workspace on show.
func (t *Plugin) pageTitle() string {
	if t.current == nil {
		return ""
	}
	return t.current.name
}

// commandSpec is one command the terminal contributes: its ID, where it
// appears, its default keys, and what it runs. keys.json rebinds it.
type commandSpec struct {
	id    string
	order int
	title string
	menu  string
	keys  []string
	run   func()
}

// commandSpecs is every command, in menu order. The list is the source for
// registration and for checking keys.json, so a misspelled command is an
// error rather than a silent no-op.
func (t *Plugin) commandSpecs() []commandSpec {
	specs := []commandSpec{
		{"new-task", -1, "New Task…", "File", []string{"cmd+shift+t"}, t.openTaskDialog},
		{"new-workspace", 0, "New Workspace…", "File", []string{"cmd+shift+n"}, t.pickWorkspace},
		{"close-workspace", 1, "Close Workspace", "File", nil, t.closeCurrentWorkspace},
		{"next-workspace", 2, "Next Workspace", "File", []string{"cmd+shift+]"}, func() { t.stepWorkspace(1) }},
		{"prev-workspace", 3, "Previous Workspace", "File", []string{"cmd+shift+["}, func() { t.stepWorkspace(-1) }},
		{"new-tab", 10, "New Tab", "File", []string{"cmd+t"}, func() { t.newTab(kindShell) }},
		{"split-right", 11, "Split Right", "File", []string{"cmd+d"}, func() { t.splitFocused(true) }},
		{"split-down", 12, "Split Down", "File", []string{"cmd+shift+d"}, func() { t.splitFocused(false) }},
		{"close-pane", 13, "Close Pane", "File", []string{"cmd+shift+w"}, t.closeFocused},
		{"claude", 20, "New Claude Code Session", "Session", nil, func() { t.newTab(kindClaude) }},
		{"codex", 21, "New Codex Session", "Session", nil, func() { t.newTab(kindCodex) }},
		{"opencode", 22, "New OpenCode Session", "Session", nil, func() { t.newTab(kindOpenCode) }},
		{"shell", 23, "New Shell Session", "Session", nil, func() { t.newTab(kindShell) }},
		{"restart", 24, "Restart Pane", "Session", nil, t.restartFocused},
		{"sidebar", 40, "Toggle Sidebar", "View", []string{"cmd+b"}, t.toggleSidebar},
		{"task-inbox", 39, "Tasks", "View", []string{"cmd+shift+j"}, t.openTaskInbox},
		{"font-larger", 41, "Larger Text", "View", []string{"cmd+="}, func() { t.zoomFont(1) }},
		{"font-smaller", 42, "Smaller Text", "View", []string{"cmd+-"}, func() { t.zoomFont(-1) }},
		{"font-reset", 43, "Actual Size", "View", []string{"cmd+0"}, func() { t.setFont(defaultFontSize) }},
		{"find", 45, "Find…", "Edit", []string{"cmd+f"}, t.openSearch},
		{"links", 47, "Links…", "View", []string{"cmd+shift+l"}, t.openLinks},
		{"index", 44, "Sessions", "View", []string{"cmd+shift+i"}, t.openIndex},
	}
	for _, name := range t.agentDefs.AgentNames() {
		id := name
		def := t.agentDefs[name]
		specs = append(specs, commandSpec{
			id: "agent-" + id, order: 25, title: "New " + def.Name + " Session",
			menu: "Session", run: func() { t.newAgent(id) },
		})
	}
	for i := 1; i <= 9; i++ {
		n := i
		specs = append(specs, commandSpec{
			id: "tab-" + strconv.Itoa(n), order: 50 + n, title: "Show Tab " + strconv.Itoa(n),
			keys: []string{"cmd+" + strconv.Itoa(n)},
			run:  func() { t.selectTab(n - 1) },
		})
	}
	return specs
}

// commands contributes the menus and shortcuts, with keys.json's overrides.
func (t *Plugin) commands(o kernel.Owner, overrides map[string][]string) {
	for _, spec := range t.commandSpecs() {
		keys := spec.keys
		if bound, ok := overrides[spec.id]; ok {
			keys = bound
		}
		_ = kernel.Contribute(o, kernel.Commands, "terminal."+spec.id, spec.order, kernel.Command{
			Title: spec.title, Menu: spec.menu, Keys: keys, Run: spec.run,
		})
	}
}

// registerCommands applies the keys in t.keys. It validates them before
// touching the old command scope, so a broken keys.json changes nothing.
func (t *Plugin) registerCommands() error {
	overrides, err := normalizeKeys(t.keys, t.commandSpecs())
	if err != nil {
		return err
	}
	if t.keyScope != nil {
		t.keyScope.Dispose()
	}
	t.keyScope = t.ctx.NewScope()
	t.commands(t.keyScope, overrides)
	return nil
}

// normalizeKeys checks a keys.json against the command list. Command ids may
// carry the terminal. prefix or leave it out; both name the same command.
func normalizeKeys(keys config.Keys, specs []commandSpec) (map[string][]string, error) {
	known := make(map[string]bool, len(specs))
	for _, spec := range specs {
		known[spec.id] = true
	}
	out := make(map[string][]string, len(keys))
	for name, bound := range keys {
		id := strings.TrimPrefix(name, "terminal.")
		if !known[id] {
			return nil, fmt.Errorf("no command %q in keys.json", name)
		}
		if _, twice := out[id]; twice {
			return nil, fmt.Errorf("keys.json names %q twice", name)
		}
		for _, key := range bound {
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t") {
				return nil, fmt.Errorf("keys.json has an invalid key %q for %q", key, name)
			}
		}
		out[id] = []string(bound)
	}
	return out, nil
}

// seedDemo opens the sessions the window starts with the first time: an agent
// in the folder the app was started in, and a tab that splits into two others.
// A home workspace joins them when the app was started elsewhere.
func (t *Plugin) seedDemo() {
	dir := launchDir()
	w := &workspace{id: t.nextID, name: filepath.Base(dir), dir: dir, color: 0}
	t.nextID++
	first := t.start(kindClaude, w.dir, 80, 24)
	w.addTab(first, true)

	left := t.start(kindOpenCode, w.dir, 80, 24)
	right := t.start(kindCodex, w.dir, 80, 24)
	root, _ := splitAt(leafOf(left), left, right, false)
	t.nextID++
	w.tabs = append(w.tabs, &tab{id: t.nextID, name: "Agent terminals", root: root, focus: left})
	t.nextID++
	w.selectTab(0)
	t.workspaces = append(t.workspaces, w)
	t.current = w
	t.taskInboxOpen = false
	if home := homeDir(); home != "" && home != dir {
		t.addWorkspace(filepath.Base(home), home)
		t.current = w
	}
	// WADATERM_TAB picks the tab that shows first, so a capture can show a
	// split without typing at the window.
	if n, err := strconv.Atoi(os.Getenv("WADATERM_TAB")); err == nil {
		w.selectTab(n - 1)
	}
}

// addWorkspace makes a workspace of a folder, with a shell in its first tab,
// and shows it.
func (t *Plugin) addWorkspace(name, dir string) *workspace {
	return t.addWorkspaceOn(name, dir, "")
}

// addWorkspaceOn makes a space whose terminals run on a machine. A machine
// that cannot be reached still makes the space, with panes that say why.
func (t *Plugin) addWorkspaceOn(name, dir, machine string) *workspace {
	if dir == "" && machine == "" {
		dir = homeDir()
	}
	client := t.core
	var err error
	if machine != "" {
		client, err = t.ensureMachine(machine)
	}
	name = t.uniqueName(name)
	w := &workspace{id: t.nextID, name: name, dir: dir, machine: machine, client: client,
		color: len(t.workspaces) % 5}
	t.nextID++
	cols, rows := t.defaultSize()
	s := t.startAt(machine, client, kindShell, dir, cols, rows)
	w.addTab(s, true)
	t.workspaces = append(t.workspaces, w)
	t.current = w
	t.taskInboxOpen = false
	if err != nil {
		t.host.Toast(ui.Toast{Title: "Machine " + machine, Description: err.Error(),
			Type: components.ToastError})
	}
	t.saveWorkspaces()
	return w
}

// uniqueName keeps two workspaces of the same folder apart.
func (t *Plugin) uniqueName(name string) string {
	if name == "" || name == "/" {
		name = "Workspace"
	}
	taken := map[string]bool{}
	for _, w := range t.workspaces {
		taken[w.name] = true
	}
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := name + " " + strconv.Itoa(i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// pickWorkspace asks for a folder and opens a workspace of it. The dialog
// blocks, so it runs in a worker and its answer comes back on the UI thread.
func (t *Plugin) pickWorkspace() {
	kernel.Go(t.ctx, &t.pick, func(context.Context) (string, error) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: "New Workspace", ButtonLabel: "Open", Directory: true, CreateDirectories: true,
		})
		if err != nil || len(paths) == 0 {
			return "", err
		}
		return paths[0], nil
	}, func(dir string, err error) {
		if err != nil {
			t.host.Toast(ui.Toast{Title: "No workspace opened", Description: err.Error(),
				Type: components.ToastError})
			return
		}
		if dir == "" {
			return
		}
		t.addWorkspace(filepath.Base(dir), dir)
	})
}

// openWorkspaceDir opens a workspace of a folder, for tests and captures that
// have no dialog to ask.
func (t *Plugin) openWorkspaceDir(dir string) *workspace {
	return t.addWorkspace(filepath.Base(dir), dir)
}

// closeCurrentWorkspace closes the workspace on show and shows a neighbour.
func (t *Plugin) closeCurrentWorkspace() {
	if t.current != nil && t.current.task != nil {
		t.openFinishTask(t.current)
		return
	}
	if len(t.workspaces) <= 1 {
		t.host.Toast(ui.Toast{Title: "Keep one workspace",
			Description: "Open another workspace before closing this one."})
		return
	}
	t.closeWorkspace(t.current)
}

// closeWorkspace ends every session of a workspace, drops it, and shows the
// one beside it.
func (t *Plugin) closeWorkspace(w *workspace) {
	if w != nil && w.task != nil {
		t.openFinishTask(w)
		return
	}
	t.dropWorkspace(w)
}

// dropWorkspace removes UI state only after normal close or successful task cleanup.
func (t *Plugin) dropWorkspace(w *workspace) {
	idx := -1
	for i, other := range t.workspaces {
		if other == w {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	for _, tb := range w.tabs {
		for _, s := range tb.root.sessions(nil) {
			s.kill()
		}
	}
	t.workspaces = append(t.workspaces[:idx], t.workspaces[idx+1:]...)
	if len(t.workspaces) == 0 {
		t.current = nil
	} else if w == t.current {
		t.current = t.workspaces[min(idx, len(t.workspaces)-1)]
	}

	if t.current != nil && t.current.focus == nil {
		if tb := t.current.currentTab(); tb != nil {
			t.current.focus = tb.focus
		}
	}
	t.saveWorkspaces()
}

// stepWorkspace shows the workspace delta places along the tree.
func (t *Plugin) stepWorkspace(delta int) {
	if len(t.workspaces) == 0 {
		return
	}
	idx := 0
	for i, w := range t.workspaces {
		if w == t.current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(t.workspaces)) % len(t.workspaces)
	t.current = t.workspaces[idx]
	t.taskInboxOpen = false
	t.workspaceMenu = false
	if t.current.focus == nil {
		if tb := t.current.currentTab(); tb != nil {
			t.current.focus = tb.focus
		}
	}
	t.saveWorkspaces()
}

// toggleSidebar shows or hides the column beside the panes, and closes the
// workspace menu it holds: a menu over a column that is gone points at
// nothing.
func (t *Plugin) toggleSidebar() {
	t.workspaceMenu = false
	t.sidebar.Set(!t.sidebar.Get())
}

// addTab appends a tab of one session to the workspace.
func (w *workspace) addTab(s *session, selectIt bool) *tab {
	tb := &tab{id: 0, root: leafOf(s), focus: s}
	w.tabs = append(w.tabs, tb)
	if selectIt {
		w.active = len(w.tabs) - 1
		w.focus = s
	}
	return tb
}

// currentTab is the tab on show of the workspace, if any.
func (w *workspace) currentTab() *tab {
	if w == nil || w.active < 0 || w.active >= len(w.tabs) {
		return nil
	}
	return w.tabs[w.active]
}

// selectTab shows the tab at i of the workspace.
func (w *workspace) selectTab(i int) {
	if i < 0 || i >= len(w.tabs) {
		return
	}
	w.active = i
	w.focus = w.tabs[i].focus
}

// sessions appends every session of the workspace, tab after tab.
func (w *workspace) sessions(out []*session) []*session {
	for _, tb := range w.tabs {
		out = tb.root.sessions(out)
	}
	return out
}

// newTab opens a session of kind k in a new tab of the workspace on show.
func (t *Plugin) newTab(k kind) {
	w := t.current
	if w == nil || (w.task != nil && w.task.Dir == "") {
		return
	}
	t.taskInboxOpen = false
	cols, rows := t.defaultSize()
	s := t.startAt(w.machine, w.client, k, w.dir, cols, rows)
	w.addTab(s, true)
	t.saveWorkspaces()
}

// splitFocused splits the focused pane, side by side when vertical is set,
// with a new shell.
func (t *Plugin) splitFocused(vertical bool) {
	w := t.current
	if w == nil {
		return
	}
	tb := w.currentTab()
	if tb == nil || tb.focus == nil {
		return
	}
	cols, rows := t.defaultSize()
	next := t.startAt(w.machine, w.client, kindShell, tb.focus.dir, cols, rows)
	if root, ok := splitAt(tb.root, tb.focus, next, vertical); ok {
		tb.root = root
		if tb.name == "" {
			tb.name = fmt.Sprintf("Split %d", w.active+1)
		}
		tb.focus = next
		w.focus = next
		t.saveWorkspaces()
	}
}

// defaultSize is the size a new session starts at: the focused pane's, so a
// split does not resize everything twice.
func (t *Plugin) defaultSize() (int, int) {
	if w := t.current; w != nil && w.focus != nil && w.focus.cols > 2 && w.focus.rows > 2 {
		return w.focus.cols, w.focus.rows
	}
	return 80, 24
}

// selectTab shows the tab at i of the workspace on show.
func (t *Plugin) selectTab(i int) {
	if t.current != nil {
		t.current.selectTab(i)
		t.seen(t.current.focus)
		t.saveWorkspaces()
	}
}

// focusOn shows the workspace and tab of s and gives it the keyboard.
func (t *Plugin) focusOn(s *session) {
	if s == nil {
		return
	}
	for _, w := range t.workspaces {
		for i, tb := range w.tabs {
			if tb.root.contains(s) {
				t.taskInboxOpen = false
				t.current = w
				w.active = i
				tb.focus = s
				w.focus = s
				t.seen(s)
				t.saveWorkspaces()
				return
			}
		}
	}
}

// closeTab closes every session of a tab and chooses what shows next.
func (t *Plugin) closeTab(i int) {
	w := t.current
	if w == nil || i < 0 || i >= len(w.tabs) {
		return
	}
	for _, s := range w.tabs[i].root.sessions(nil) {
		s.kill()
	}
	w.tabs = append(w.tabs[:i], w.tabs[i+1:]...)
	if i < w.active {
		w.active--
	}
	if w.active >= len(w.tabs) {
		w.active = max(len(w.tabs)-1, 0)
	}
	if len(w.tabs) == 0 {
		w.focus = nil
		t.saveWorkspaces()
		return
	}
	w.focus = w.tabs[w.active].focus
	t.saveWorkspaces()
}

// closeFocused closes the focused session and chooses what shows next.
func (t *Plugin) closeFocused() {
	if w := t.current; w != nil && w.focus != nil {
		t.close(w.focus)
	}
}

// close ends a session, takes it out of its layout and drops its tab when
// nothing is left in it.
func (t *Plugin) close(s *session) {
	w, tb, idx := t.tabOf(s)
	if w == nil {
		return
	}
	tb.root = removeAt(tb.root, s)
	s.kill()
	if tb.root == nil {
		w.tabs = append(w.tabs[:idx], w.tabs[idx+1:]...)
		if w.active >= len(w.tabs) {
			w.active = max(len(w.tabs)-1, 0)
		}
		if len(w.tabs) == 0 {
			w.focus = nil
			if len(w.tabs) == 0 && w == t.current {
				t.host.Toast(ui.Toast{Title: "The workspace is empty",
					Description: "Press ⌘T for a new tab, or open another workspace."})
			}
			t.saveWorkspaces()
			return
		}
		w.focus = w.tabs[w.active].focus
		t.saveWorkspaces()
		return
	}
	rest := tb.root.sessions(nil)
	tb.focus = rest[len(rest)-1]
	if w.active == idx {
		w.focus = tb.focus
	}
	t.saveWorkspaces()
}

// restartFocused runs the focused pane's program again in its place.
func (t *Plugin) restartFocused() {
	if w := t.current; w != nil && w.focus != nil {
		t.restart(w.focus)
	}
}

// tabOf is the workspace, tab and index of the tab that shows s, if any.
func (t *Plugin) tabOf(s *session) (*workspace, *tab, int) {
	for _, w := range t.workspaces {
		for i, tb := range w.tabs {
			if tb.root.contains(s) {
				return w, tb, i
			}
		}
	}
	return nil, nil, -1
}

// splitAt splits the pane of s with a new shell beside or below it.
func (t *Plugin) splitAt(s *session, vertical bool) {
	_, tb, index := t.tabOf(s)
	if tb == nil {
		return
	}
	cols, rows := t.defaultSize()
	next := t.startAt(s.machine, s.client, kindShell, s.dir, cols, rows)
	if root, ok := splitAt(tb.root, s, next, vertical); ok {
		tb.root = root
		if tb.name == "" {
			tb.name = fmt.Sprintf("Split %d", index+1)
		}
		tb.focus = next
		t.focusOn(next)
		t.saveWorkspaces()
	}
}

// restart runs a stopped session's program again in its place, in the same
// kind and folder.
func (t *Plugin) restart(old *session) {
	w, tb, idx := t.tabOf(old)
	if tb == nil {
		return
	}
	k, dir, cols, rows := old.kind, old.dir, old.cols, old.rows
	old.kill()
	var next *session
	if old.agent {
		def := config.Agent{Name: old.name, Command: old.argv, Glyph: old.agentGlyph,
			Color: old.agentColor}
		if configured, ok := t.agentDefs[old.agentID]; ok {
			def = configured
		}
		next = t.startProgram(def, old.agentID, old.machine, old.client, dir, cols, rows)
	} else {
		next = t.startAt(old.machine, old.client, k, dir, cols, rows)
	}
	tb.root = replaceAt(tb.root, old, next)
	tb.focus = next
	if w.active == idx {
		w.focus = next
	}
	t.saveWorkspaces()
}

// closed records that a session's program ended.
func (t *Plugin) closed(s *session, err error) {
	s.alive = false
	switch {
	case err == nil:
		s.status = "The program exited."
	default:
		s.status = "The program exited: " + err.Error()
	}
	t.ctx.Invalidate()
}

// allSessions lists every session open, workspace after workspace.
func (t *Plugin) allSessions() []*session {
	var out []*session
	for _, w := range t.workspaces {
		out = w.sessions(out)
	}
	return out
}

// zoomFont changes the font size by step, within what stays readable.
func (t *Plugin) zoomFont(step int) {
	size := t.font.Get() + float64(step)
	t.setFont(min(max(size, 9), 22))
}

func (t *Plugin) setFont(size float64) {
	t.font.Set(size)
	font := t.terminalFont()
	for _, s := range t.allSessions() {
		if s.term != nil {
			s.term.SetFont(font)
		}
	}
	t.ctx.Invalidate()
}

// terminalFont is the font every terminal shows its text in.
func (t *Plugin) terminalFont() mterm.Font {
	return mterm.Font{Family: t.fontFamily(), Size: float32(t.font.Get())}
}

// launchDir is the folder the app was started in, which is the first
// workspace's folder; home when there is none, as for a bundled app.
func launchDir() string {
	if wd, err := os.Getwd(); err == nil && wd != "" && wd != "/" {
		return wd
	}
	return homeDir()
}

// homeDir is where sessions start.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

// dragDivider moves the divider of a split as the pointer drags it. The
// pointer is captured by the divider, so its moves come here even when it
// leaves the bar.
func (t *Plugin) dragDivider(b *branch, ev ui.InputEvent, vertical bool) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if b.usable <= 0 {
			return false
		}
		t.drag = b
		return true
	case ui.InputPointerMove:
		if t.drag != b || b.usable <= 0 {
			return false
		}
		// The divider follows the pointer, so each move adds how far the
		// pointer is into the bar it has not caught up with yet.
		b.dragBy(ev.X, ev.Y, vertical)
		if t.ctx != nil {
			t.ctx.Invalidate()
		}
		return true
	case ui.InputPointerUp:
		if t.drag == b {
			t.drag = nil
			t.saveWorkspaces()
			return true
		}
	}
	return false
}
