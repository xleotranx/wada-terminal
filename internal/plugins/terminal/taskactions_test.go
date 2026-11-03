package terminal

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"wada-terminal/internal/tasks"
)

func TestTaskPrompt(t *testing.T) {
	task := tasks.Task{Name: "Fix login", Brief: "Keep sessions\nAdd a regression test"}
	if _, err := taskPrompt(task, false); err == nil {
		t.Fatal("sent a multiline brief without bracketed paste")
	}
	got, err := taskPrompt(task, true)
	if err != nil || got != "\x1b[200~Fix login\n\nKeep sessions\nAdd a regression test\x1b[201~\r" {
		t.Fatalf("unexpected prompt %q: %v", got, err)
	}
	got, err = taskPrompt(tasks.Task{Name: "Old task"}, false)
	if err != nil || got != "Old task\r" {
		t.Fatalf("old task prompt %q: %v", got, err)
	}
	if _, err := taskPrompt(tasks.Task{Name: "Task", Brief: "bad\x1b[201~"}, true); err == nil {
		t.Fatal("accepted terminal control characters")
	}
	if _, err := taskPrompt(tasks.Task{Name: "Task", Brief: strings.Repeat("a", 32001)}, true); err == nil {
		t.Fatal("accepted an oversized brief")
	}
}

func TestTaskSubmissionState(t *testing.T) {
	task := tasks.Task{Name: "Fix login", Brief: "Keep sessions"}
	if got := taskSubmissionLabel(task); got != "Not sent with Send task" {
		t.Fatal(got)
	}
	now := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	task.SentAt, task.SentName, task.SentBrief = &now, task.Name, task.Brief
	if got := taskSubmissionLabel(task); !strings.Contains(got, "Submitted to terminal") {
		t.Fatal(got)
	}
	task.Brief = "New criteria"
	if got := taskSubmissionLabel(task); !strings.Contains(got, "changed since") {
		t.Fatal(got)
	}
	if err := (&session{}).writeTaskInput([]byte("test")); err == nil {
		t.Fatal("reported success for a stopped session")
	}
}

func TestAgentReadinessDoesNotGuess(t *testing.T) {
	s := &session{alive: true, kind: kindCodex}
	if got := taskAgentState(s); !strings.Contains(got, "unknown") {
		t.Fatal(got)
	}
	s.reported, s.word = true, "idle"
	if got := taskAgentState(s); !strings.Contains(got, "reports idle") {
		t.Fatal(got)
	}
	s.word = "blocked"
	if got := taskAgentState(s); !strings.Contains(got, "needs input") {
		t.Fatal(got)
	}
}

func TestTaskStatusUsesEvidence(t *testing.T) {
	s := &session{alive: true, done: true}
	w := &workspace{task: &tasks.Task{Run: tasks.Run{Dir: "/worktree"}}, tabs: []*tab{{root: leafOf(s)}}}
	if got := taskStatus(w); got != "Open" {
		t.Fatalf("guessed status without report: %s", got)
	}
	w.task.Ready = true
	if got := taskStatus(w); got != "Ready to review" {
		t.Fatal(got)
	}
	s.reported = true
	s.word = "working"
	if got := taskStatus(w); got != "Working" {
		t.Fatal(got)
	}
	s.word = "blocked"
	if got := taskStatus(w); got != "Needs you" {
		t.Fatal(got)
	}
}

func TestQueuedTaskRestoresWithoutTerminal(t *testing.T) {
	p := &Plugin{}
	if err := p.queueTask("Fix login", "Keep sessions", "/repo", "codex"); err != nil {
		t.Fatal(err)
	}
	w := p.current
	if taskStatus(w) != "Queued" || len(w.tabs) != 0 || w.task.Brief != "Keep sessions" || w.task.Agent != "codex" {
		t.Fatalf("queued task not restored: %#v", w)
	}
	data, err := json.Marshal(p.workspaceSpecs())
	if err != nil {
		t.Fatal(err)
	}
	var specs []workspaceSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		t.Fatal(err)
	}
	restored := p.buildWorkspace(specs[0], nil, false, 0)
	if taskStatus(restored) != "Queued" || len(restored.tabs) != 0 || restored.task.Agent != "codex" {
		t.Fatalf("queued task lost in persistence: %#v", restored)
	}
}

func TestFinishedTaskRestoresWithoutTerminal(t *testing.T) {
	p := &Plugin{}
	task := &tasks.Task{Name: "Finished", Repo: "/repo", Run: tasks.Run{Branch: "wada/task-1234567890abcdef", Finished: true, BranchKept: true}}
	w := p.buildWorkspace(workspaceSpec{Name: task.Name, Dir: task.Repo, Task: task}, nil, false, 0)
	if taskStatus(w) != "Finished" || len(w.tabs) != 0 || !w.task.BranchKept {
		t.Fatalf("bad finished task: %#v", w)
	}
}

func TestSelectedTaskUsesPreviousRun(t *testing.T) {
	w := &workspace{task: &tasks.Task{Name: "Task", Repo: "/repo", Run: tasks.Run{Branch: "current", Dir: "/work"}, Runs: []tasks.Run{{Branch: "earlier", Finished: true, BranchKept: true}}}, review: &taskReview{history: 1}}
	selected := selectedTask(w)
	if selected.Branch != "earlier" || !selected.Finished || w.task.Branch != "current" || w.task.Finished {
		t.Fatalf("selected wrong run: %+v", selected)
	}
}
