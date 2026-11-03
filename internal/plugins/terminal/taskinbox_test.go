package terminal

import (
	"testing"

	"wada-terminal/internal/tasks"
)

func TestTaskInboxGroupsAndSearch(t *testing.T) {
	p := &Plugin{workspaces: []*workspace{
		{task: &tasks.Task{Name: "Queue task", Repo: "/one", Run: tasks.Run{Agent: "codex"}}},
		{task: &tasks.Task{Name: "Completed task", Repo: "/two", Run: tasks.Run{Finished: true}}},
		{task: &tasks.Task{Name: "Unreported task", Repo: "/one", Run: tasks.Run{Dir: "/run"}}},
	}}
	groups := p.taskInboxGroups("")
	if len(groups["Queued"]) != 1 || len(groups["Finished"]) != 1 || len(groups["Open"]) != 1 {
		t.Fatalf("wrong groups: %+v", groups)
	}
	groups = p.taskInboxGroups("CODEX")
	if len(groups["Queued"]) != 1 || len(groups["Finished"]) != 0 {
		t.Fatalf("search missed agent: %+v", groups)
	}
	groups = p.taskInboxGroups("/two")
	if len(groups["Finished"]) != 1 || len(groups["Queued"]) != 0 {
		t.Fatalf("search missed repository: %+v", groups)
	}
}

func TestTaskInboxActions(t *testing.T) {
	for status, want := range map[string]string{"Needs you": "Respond", "Ready to review": "Review", "Working": "Open terminal", "Queued": "Start…", "Open": "Open task", "Finished": "Open result"} {
		if got := taskInboxAction(status); got != want {
			t.Errorf("%s: %s", status, got)
		}
	}
}

func TestRespondOpensReportedBlockedPane(t *testing.T) {
	first := &session{alive: true, reported: true, word: "working"}
	blocked := &session{alive: true, reported: true, word: "blocked"}
	w := &workspace{task: &tasks.Task{Run: tasks.Run{Dir: "/run"}}, tabs: []*tab{{root: leafOf(first), focus: first}, {root: leafOf(blocked), focus: blocked}}, focus: first}
	p := &Plugin{workspaces: []*workspace{w}, current: w, taskInboxOpen: true}
	if got := taskStatus(w); got != "Needs you" {
		t.Fatal(got)
	}
	p.openInboxTask(w, taskStatus(w))
	if p.taskInboxOpen || w.focus != blocked || w.active != 1 {
		t.Fatal("did not open blocked pane")
	}
	blocked.alive = false
	if got := taskStatus(w); got != "Working" {
		t.Fatalf("stopped agent still claimed attention: %s", got)
	}
}
