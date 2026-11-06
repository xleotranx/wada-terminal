package terminal

import (
	"testing"
	"wada-terminal/internal/tasks"
)

func TestQueuedTaskRejectsControlTerminalCreation(t *testing.T) {
	w := &workspace{task: &tasks.Task{Name: "queued"}}
	p := &Plugin{current: w}
	if _, err := p.controlDo("tab.new", nil); err == nil {
		t.Fatal("queued task opened a terminal")
	}
	if len(w.tabs) != 0 {
		t.Fatal("queued task gained a terminal")
	}
}
