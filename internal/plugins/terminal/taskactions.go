package terminal

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xleotranx/wada-kit/kernel"
	"wada-terminal/internal/tasks"
)

// taskUI is UI-thread state. Workers receive copies of the task metadata.
type taskUI struct {
	open                       bool
	name, repo, agent, problem string
	create                     kernel.Task
}
type taskReview struct {
	job         kernel.Task
	checkJob    kernel.Task
	changes     tasks.Changes
	problem     string
	changesView bool
	command     string
}

func (t *Plugin) openTaskDialog() {
	if t.tasksUI.create.Running() {
		return
	}
	repo := t.focusedDir()
	if t.current != nil && t.current.task != nil {
		repo = t.current.task.Repo
	}
	t.tasksUI.open = true
	t.tasksUI.repo = repo
	t.tasksUI.name = ""
	t.tasksUI.agent = "Shell"
	t.tasksUI.problem = ""
	t.repaint()
}
func (t *Plugin) taskAgents() ([]string, map[string]string) {
	labels := []string{"Shell", "Claude Code", "Codex", "OpenCode"}
	ids := map[string]string{"Shell": "shell", "Claude Code": "claude", "Codex": "codex", "OpenCode": "opencode"}
	keys := make([]string, 0, len(t.agentDefs))
	for id := range t.agentDefs {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		label := t.agentDefs[id].Name + " (" + id + ")"
		labels = append(labels, label)
		ids[label] = "custom:" + id
	}
	return labels, ids
}
func (t *Plugin) createTask(name, repo, agent string) error {
	if t.tasksUI.create.Running() {
		return fmt.Errorf("a task is already being created")
	}
	if t.current != nil && t.current.machine != "" {
		return fmt.Errorf("task workspaces currently require a local repository")
	}
	var k kind
	switch agent {
	case "shell":
		k = kindShell
	case "claude":
		k = kindClaude
	case "codex":
		k = kindCodex
	case "opencode":
		k = kindOpenCode
	default:
		if !strings.HasPrefix(agent, "custom:") {
			return fmt.Errorf("unknown agent %q", agent)
		}
		def, ok := t.agentDefs[strings.TrimPrefix(agent, "custom:")]
		if !ok || len(def.Command) == 0 {
			return fmt.Errorf("unknown agent %q", agent)
		}
	}
	if !strings.HasPrefix(agent, "custom:") && k != kindShell && len(commandFor(k.spec())) == 0 {
		return fmt.Errorf("%s is not installed; choose another agent", k.spec().name)
	}
	def := t.agentDefs[strings.TrimPrefix(agent, "custom:")]
	if t.host.DataDir() == "" {
		return fmt.Errorf("task workspaces need an application data folder")
	}
	root := filepath.Join(t.host.DataDir(), "tasks")
	t.tasksUI.problem = ""
	kernel.Go(t.ctx, &t.tasksUI.create, func(ctx context.Context) (tasks.Task, error) { return tasks.Create(ctx, repo, root, name) }, func(task tasks.Task, err error) {
		if err != nil {
			t.tasksUI.problem = err.Error()
			t.tasksUI.open = true
			t.repaint()
			return
		}
		w := &workspace{id: t.nextID, name: t.uniqueName(task.Name), dir: task.Dir, client: t.core, color: len(t.workspaces) % 5, task: &task}
		t.nextID++
		cols, rows := t.defaultSize()
		var s *session
		if strings.HasPrefix(agent, "custom:") {
			s = t.startProgram(def, strings.TrimPrefix(agent, "custom:"), "", t.core, task.Dir, cols, rows)
		} else {
			s = t.startAt("", t.core, k, task.Dir, cols, rows)
		}
		w.addTab(s, true)
		t.workspaces = append(t.workspaces, w)
		t.current = w
		t.tasksUI.open = false
		t.saveWorkspaces()
		t.repaint()
	})
	return nil
}
func (t *Plugin) reviewTask(w *workspace) {
	if w == nil || w.task == nil {
		return
	}
	if w.review == nil {
		w.review = &taskReview{}
	}
	r := w.review
	task := *w.task
	r.problem = ""
	kernel.Go(t.ctx, &r.job, func(ctx context.Context) (tasks.Changes, error) { return tasks.Review(ctx, task) }, func(changes tasks.Changes, err error) {
		if err != nil {
			r.problem = err.Error()
		} else {
			r.changes = changes
		}
		t.repaint()
	})
}
func (t *Plugin) checkTask(w *workspace, command string) {
	if w == nil || w.task == nil {
		return
	}
	if w.review == nil {
		w.review = &taskReview{}
	}
	r := w.review
	if r.checkJob.Running() {
		return
	}
	task := *w.task
	r.command = command
	r.problem = ""
	kernel.Go(t.ctx, &r.checkJob, func(ctx context.Context) (tasks.Check, error) { return tasks.RunCheck(ctx, task, command) }, func(check tasks.Check, err error) {
		if err != nil {
			r.problem = err.Error()
		} else {
			w.task.Check = &check
			t.saveWorkspaces()
			t.reviewTask(w)
		}
		t.repaint()
	})
}
func taskStatus(w *workspace) string {
	working, done := false, false
	for _, s := range w.sessions(nil) {
		if !s.reported {
			continue
		}
		switch s.word {
		case "blocked", "error":
			return "Needs you"
		case "working":
			working = true
		case "done":
			done = true
		}
	}
	if working {
		return "Working"
	}
	if done || (w.task != nil && w.task.Ready) {
		return "Ready to review"
	}
	return "Open"
}

// Review I/O is scheduled from a scope worker, never from the build pass.
func (t *Plugin) watchTaskChanges(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.ctx.PostQuiet(func() {
				w := t.current
				if w != nil && w.task != nil && w.review != nil && w.review.changesView && !w.finishing && !w.review.job.Running() && !w.review.checkJob.Running() {
					t.reviewTask(w)
				}
			})
		}
	}
}
