package terminal

import (
	"testing"
	"wada-terminal/internal/tasks"
)

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
