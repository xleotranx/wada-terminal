package terminal

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/xleotranx/wada-kit/kernel"
	"wada-terminal/internal/tasks"
)

// taskUI is UI-thread state. Workers receive copies of the task metadata.
type taskUI struct {
	open                              bool
	name, brief, repo, agent, problem string
	editOpen                          bool
	editTask                          *workspace
	editBrief, editProblem            string
	create                            kernel.Task
}
type taskReview struct {
	job         kernel.Task
	checkJob    kernel.Task
	changes     tasks.Changes
	problem     string
	changesView bool
	command     string
	history     int // 0 is the current run; otherwise index + 1 in Runs
	retryAgent  string
}

func selectedTask(w *workspace) tasks.Task {
	task := *w.task
	if w.review != nil && w.review.history > 0 && w.review.history <= len(task.Runs) {
		task.Run = task.Runs[w.review.history-1]
	}
	return task
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
	t.tasksUI.brief = ""
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
func (t *Plugin) createTask(name, brief, repo, agent string) error {
	return t.startTask(nil, name, brief, repo, agent)
}

func (t *Plugin) queueTask(name, brief, repo, agent string) error {
	if err := validateTaskBrief(brief); err != nil {
		return err
	}
	if t.current != nil && t.current.machine != "" {
		return fmt.Errorf("tasks currently require a local repository")
	}
	task, err := tasks.Draft(name, brief, repo, agent)
	if err != nil {
		return err
	}
	w := &workspace{id: t.nextID, name: t.uniqueName(task.Name), dir: task.Repo, client: t.core, color: len(t.workspaces) % 5, task: &task}
	t.nextID++
	t.workspaces = append(t.workspaces, w)
	t.current = w
	t.taskInboxOpen = false
	t.tasksUI.open = false
	t.saveWorkspaces()
	return nil
}

func (t *Plugin) startTask(queued *workspace, name, brief, repo, agent string) error {
	if queued != nil && queued.task != nil && queued.task.Dir != "" {
		return fmt.Errorf("finish the current run before starting another")
	}
	if err := validateTaskBrief(brief); err != nil {
		return err
	}
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
	if queued != nil {
		queued.finishing = true
	}
	kernel.Go(t.ctx, &t.tasksUI.create, func(ctx context.Context) (tasks.Task, error) { return tasks.Create(ctx, repo, root, name) }, func(task tasks.Task, err error) {
		if queued != nil {
			queued.finishing = false
		}
		if err != nil {
			if queued != nil {
				if queued.review == nil {
					queued.review = &taskReview{}
				}
				queued.review.problem = err.Error()
			} else {
				t.tasksUI.problem = err.Error()
				t.tasksUI.open = true
			}
			t.repaint()
			return
		}
		task.Brief = brief
		task.Run.Brief = brief
		task.Agent = agent
		w := queued
		if w == nil {
			w = &workspace{id: t.nextID, name: t.uniqueName(task.Name), client: t.core, color: len(t.workspaces) % 5}
			t.nextID++
			t.workspaces = append(t.workspaces, w)
		}
		if queued != nil && queued.task != nil {
			previous := queued.task
			if previous.Finished {
				task.Runs = append(append([]tasks.Run(nil), previous.Runs...), previous.Run)
			} else {
				task.Runs = append([]tasks.Run(nil), previous.Runs...)
			}
			task.Name, task.Brief = previous.Name, previous.Brief
		}
		w.task, w.dir = &task, task.Dir
		w.review = nil
		cols, rows := t.defaultSize()
		var s *session
		if strings.HasPrefix(agent, "custom:") {
			s = t.startProgram(def, strings.TrimPrefix(agent, "custom:"), "", t.core, task.Dir, cols, rows)
		} else {
			s = t.startAt("", t.core, k, task.Dir, cols, rows)
		}
		w.addTab(s, true)
		t.current = w
		t.taskInboxOpen = false
		t.tasksUI.open = false
		t.saveWorkspaces()
		t.repaint()
	})
	return nil
}

func validateTaskBrief(brief string) error {
	if len(brief) > 32000 || strings.IndexFunc(brief, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r'
	}) >= 0 {
		return fmt.Errorf("task brief must be at most 32,000 bytes and contain no terminal control characters")
	}
	return nil
}

// A successful write records submission to the terminal transport, not that
// the program consumed or accepted the prompt.
func (s *session) writeTaskInput(b []byte) error {
	if s == nil || !s.alive {
		return fmt.Errorf("the selected terminal is not running")
	}
	if s.remote {
		if s.stream == nil {
			return fmt.Errorf("the terminal is disconnected")
		}
		_, err := s.stream.Write(b)
		return err
	}
	if s.proc == nil {
		return fmt.Errorf("the terminal is unavailable")
	}
	n, err := s.proc.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

func taskSubmissionLabel(task tasks.Task) string {
	if task.SentAt == nil {
		return "Not sent with Send task"
	}
	label := "Submitted to terminal transport " + task.SentAt.Local().Format("Jan 2 15:04")
	if task.SentProgram != "" {
		label += " · " + task.SentProgram
	}
	if task.SentName != task.Name || task.SentBrief != task.Brief {
		label += " · Task changed since submission"
	}
	return label
}

func taskAgentState(s *session) string {
	if s == nil {
		return "Select an agent terminal to send the task."
	}
	if !s.alive {
		return "The selected terminal has stopped."
	}
	if !s.isAgent() {
		return "The selected terminal is a shell; type the task manually."
	}
	if !s.reported {
		return "Agent readiness unknown; check the terminal prompt."
	}
	switch s.word {
	case "idle":
		return "Agent reports idle; check the prompt before sending."
	case "working":
		return "Agent reports working."
	case "blocked", "error":
		return "Agent reports it needs input."
	case "done":
		return "Agent reports its work is done; check the prompt before sending."
	default:
		return "Agent readiness unknown; check the terminal prompt."
	}
}

func (t *Plugin) sendTask(w *workspace) error {
	if w == nil || w.task == nil || w.focus == nil || !w.focus.alive || !w.focus.isAgent() {
		return fmt.Errorf("select a running agent terminal")
	}
	s := w.focus
	prompt, err := taskPrompt(*w.task, s.bracketed)
	if err != nil {
		return err
	}
	if err := s.writeTaskInput([]byte(prompt)); err != nil {
		return fmt.Errorf("terminal submission failed; some input may have been sent. Check the terminal before retrying: %w", err)
	}
	now := time.Now()
	w.task.SentAt, w.task.SentName, w.task.SentBrief = &now, w.task.Name, w.task.Brief
	w.task.SentProgram = s.name
	w.task.Ready = false
	t.saveWorkspaces()
	t.prompted(s)
	return nil
}

// taskPrompt refuses multiline input unless the program has enabled bracketed
// paste, so line breaks cannot accidentally submit partial prompts.
func taskPrompt(task tasks.Task, bracketed bool) (string, error) {
	if err := validateTaskBrief(task.Brief); err != nil {
		return "", err
	}
	if strings.IndexFunc(task.Name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("task name contains terminal control characters")
	}
	brief := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(task.Brief, "\r\n", "\n"), "\r", "\n"))
	message := task.Name
	if brief != "" {
		message += "\n\n" + brief
	}
	if strings.ContainsAny(message, "\n\t") {
		if !bracketed {
			return "", fmt.Errorf("the agent has not enabled multiline paste; wait until its prompt is ready")
		}
		return "\x1b[200~" + message + "\x1b[201~\r", nil
	}
	return message + "\r", nil
}
func (t *Plugin) reviewTask(w *workspace) {
	if w == nil || w.task == nil {
		return
	}
	if w.review == nil {
		w.review = &taskReview{}
	}
	r := w.review
	task := selectedTask(w)
	r.problem = ""
	kernel.Go(t.ctx, &r.job, func(ctx context.Context) (tasks.Changes, error) {
		if task.Finished {
			return tasks.ReviewFinished(ctx, task)
		}
		return tasks.Review(ctx, task)
	}, func(changes tasks.Changes, err error) {
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
	if w.task != nil && w.task.Finished {
		return "Finished"
	}
	if w.task != nil && w.task.Dir == "" {
		return "Queued"
	}
	working, done := false, false
	for _, s := range w.sessions(nil) {
		if !s.alive || !s.reported {
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
					return
				}
				// Nothing is being reviewed: a poll that found nothing must
				// not cost the window a frame.
				t.ctx.FrameDue()
			})
		}
	}
}
