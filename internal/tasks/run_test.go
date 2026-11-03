package tasks

import (
	"encoding/json"
	"testing"
)

func TestTaskRunHistoryRoundTrip(t *testing.T) {
	task := Task{Name: "Fix login", Brief: "Keep sessions", Repo: "/repo", Run: Run{Agent: "codex", Dir: "/run2", Branch: "run2"}, Runs: []Run{{Agent: "claude", Finished: true, Branch: "run1", BranchKept: true}}}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var restored Task
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Name != task.Name || restored.Brief != task.Brief || restored.Run.Branch != "run2" || len(restored.Runs) != 1 || restored.Runs[0].Branch != "run1" {
		t.Fatalf("lost run history: %+v", restored)
	}
}
