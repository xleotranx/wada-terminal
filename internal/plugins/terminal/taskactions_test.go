package terminal

import (
	"strings"
	"testing"
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

func TestTaskStatusUsesEvidence(t *testing.T) {
	s := &session{alive: true, done: true}
	w := &workspace{task: &tasks.Task{}, tabs: []*tab{{root: leafOf(s)}}}
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
