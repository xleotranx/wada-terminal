package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("original\n"), 0600)
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "initial"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	return dir
}

func TestTaskIsolationAndCheckEvidence(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty source"), 0600)
	task, err := Create(ctx, dir, t.TempDir(), "Fix login; $(false)")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(task.Dir, "a.txt"))
	if string(got) != "original\n" {
		t.Fatalf("copied dirty source: %s", got)
	}
	if !strings.HasPrefix(task.Branch, "wada/task-") {
		t.Fatal(task.Branch)
	}
	os.WriteFile(filepath.Join(task.Dir, "a.txt"), []byte("task change\n"), 0600)
	os.WriteFile(filepath.Join(task.Dir, "new.txt"), []byte("new file\n"), 0600)
	review, err := Review(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review.Text, "task change") || !strings.Contains(review.Text, "new.txt") {
		t.Fatal(review.Text)
	}
	check, err := RunCheck(ctx, task, "printf checked; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	if check.ExitCode != 3 || !strings.Contains(check.Output, "checked") || check.Snapshot != review.Snapshot {
		t.Fatalf("%+v", check)
	}
	os.WriteFile(filepath.Join(task.Dir, "new.txt"), []byte("changed\n"), 0600)
	after, err := Review(ctx, task)
	if err != nil || after.Snapshot == check.Snapshot {
		t.Fatal("untracked changes did not invalidate check", err)
	}
	if _, err := Create(ctx, dir, t.TempDir(), "  "); err == nil {
		t.Fatal("accepted empty task")
	}
	if _, err := Create(ctx, t.TempDir(), t.TempDir(), "task"); err == nil {
		t.Fatal("accepted non-repository")
	}
}

func TestRejectsAmbiguousTaskInputs(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	for _, name := range []string{"", "\x1b[31mTask", "Task\rcommand"} {
		if _, err := Create(ctx, dir, t.TempDir(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := Create(ctx, "", t.TempDir(), "Task"); err == nil {
		t.Fatal("empty folder used process working directory")
	}
	if _, err := Review(ctx, Task{Run: Run{Dir: dir, Base: "--ext-diff"}}); err == nil {
		t.Fatal("accepted an option as the task base")
	}
}
