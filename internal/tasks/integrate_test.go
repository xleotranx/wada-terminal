package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func finishedTask(t *testing.T) Task {
	t.Helper()
	ctx := context.Background()
	source, root := repo(t), t.TempDir()
	task, err := Create(ctx, source, root, "Fix login")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.Dir, "a.txt"), []byte("fixed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Finish(ctx, task, root, plan.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	task.Dir, task.Finished, task.BranchKept = "", true, result.BranchKept
	return task
}

func TestIntegrateFastForwardAndStalePreview(t *testing.T) {
	ctx := context.Background()
	task := finishedTask(t)
	plan, err := InspectIntegration(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.Repo, "local"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Integrate(ctx, task, plan); err == nil {
		t.Fatal("accepted a dirty checkout after preview")
	}
	if err := os.Remove(filepath.Join(task.Repo, "local")); err != nil {
		t.Fatal(err)
	}
	if err := Integrate(ctx, task, plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(task.Repo, "a.txt"))
	if string(got) != "fixed\n" {
		t.Fatalf("result: %s", got)
	}
	if _, err := exec.Command("git", "-C", task.Repo, "show-ref", "--verify", "refs/heads/"+task.Branch).CombinedOutput(); err != nil {
		t.Fatal("task branch was lost", err)
	}
}

func TestIntegrateRejectsDivergenceAndChangedBranch(t *testing.T) {
	ctx := context.Background()
	task := finishedTask(t)
	plan, err := InspectIntegration(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", task.Repo, "update-ref", "refs/heads/"+task.Branch, task.Base)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if err := Integrate(ctx, task, plan); err == nil {
		t.Fatal("accepted moved task branch")
	}
	task = finishedTask(t)
	if err := os.WriteFile(filepath.Join(task.Repo, "a.txt"), []byte("other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "a.txt"}, {"commit", "-m", "other"}} {
		if out, err := exec.Command("git", append([]string{"-C", task.Repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	if _, err := InspectIntegration(ctx, task); err == nil {
		t.Fatal("accepted divergent history")
	}
	got, _ := os.ReadFile(filepath.Join(task.Repo, "a.txt"))
	if string(got) != "other\n" {
		t.Fatal("changed divergent checkout")
	}
}

func TestIntegrateProtectsIgnoredFile(t *testing.T) {
	ctx := context.Background()
	source, root := repo(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".git", "info", "exclude"), []byte("/local.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	task, err := Create(ctx, source, root, "Add file")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.Dir, "local.txt"), []byte("task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-f", "local.txt"}, {"commit", "-m", "Add file"}} {
		if out, err := exec.Command("git", append([]string{"-C", task.Dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	plan, err := InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Finish(ctx, task, root, plan.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	task.Dir, task.Finished, task.BranchKept = "", true, result.BranchKept
	if err := os.WriteFile(filepath.Join(source, "local.txt"), []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := InspectIntegration(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := Integrate(ctx, task, preview); err == nil {
		t.Fatal("overwrote an ignored file")
	}
	got, err := os.ReadFile(filepath.Join(source, "local.txt"))
	if err != nil || string(got) != "keep\n" {
		t.Fatalf("ignored file lost: %q, %v", got, err)
	}
}
