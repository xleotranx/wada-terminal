package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFinishSavesWorkAndRemovesFolder(t *testing.T) {
	ctx := context.Background()
	source := repo(t)
	root := t.TempDir()
	task, err := Create(ctx, source, root, "Fix login")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(task.Dir, "a.txt"), []byte("fixed\n"), 0600)
	os.WriteFile(filepath.Join(task.Dir, "new.txt"), []byte("new\n"), 0600)
	plan, err := InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Dirty {
		t.Fatal("dirty task appeared clean")
	}
	result, err := Finish(ctx, task, root, plan.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BranchKept {
		t.Fatal("unmerged work lost its recovery branch")
	}
	if _, err := os.Stat(task.Dir); !os.IsNotExist(err) {
		t.Fatal("working folder was not removed")
	}
	saved, err := exec.Command("git", "-C", source, "show", task.Branch+":new.txt").CombinedOutput()
	if err != nil || string(saved) != "new\n" {
		t.Fatalf("saved work: %s, %v", saved, err)
	}
	original, _ := os.ReadFile(filepath.Join(source, "a.txt"))
	if string(original) != "original\n" {
		t.Fatal("source changed")
	}
}

func TestFinishRejectsChangedOrForeignWorktree(t *testing.T) {
	ctx := context.Background()
	source := repo(t)
	root := t.TempDir()
	task, err := Create(ctx, source, root, "Task")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(task.Dir, "a.txt"), []byte("late edit"), 0600)
	if _, err := Finish(ctx, task, root, plan.Version, false); err == nil {
		t.Fatal("discarded edits made after confirmation preview")
	}
	foreign := task
	foreign.Dir = source
	if _, err := InspectFinish(ctx, foreign, root); err == nil {
		t.Fatal("accepted source repo for deletion")
	}
	foreign = task
	foreign.Branch = "main"
	if _, err := InspectFinish(ctx, foreign, root); err == nil {
		t.Fatal("accepted unrelated branch")
	}
	if _, err := InspectFinish(ctx, task, t.TempDir()); err == nil {
		t.Fatal("accepted folder outside app task root")
	}
	if _, err := os.Stat(filepath.Join(task.Dir, "a.txt")); err != nil {
		t.Fatal("refused cleanup damaged work")
	}
	plan, err = InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Finish(ctx, task, root, plan.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.BranchKept {
		t.Fatal("unchanged branch was not cleaned up")
	}
}

func TestFinishFailurePreservesFiles(t *testing.T) {
	ctx := context.Background()
	source := repo(t)
	root := t.TempDir()
	task, err := Create(ctx, source, root, "Task")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(task.Dir, "a.txt"), []byte("keep me"), 0600)
	// A failed save hook must never fall through to worktree removal.
	hook := filepath.Join(source, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectFinish(ctx, task, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Finish(ctx, task, root, plan.Version, true); err == nil {
		t.Fatal("ignored failed save")
	}
	content, err := os.ReadFile(filepath.Join(task.Dir, "a.txt"))
	if err != nil || string(content) != "keep me" {
		t.Fatal("failed finish lost work", err)
	}
	foreign := task
	foreign.Repo = repo(t)
	if _, err := InspectFinish(ctx, foreign, root); err == nil {
		t.Fatal("accepted registration in a different repository")
	}
	moved := task.Dir + "-moved"
	if err := os.Rename(task.Dir, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, task.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFinish(ctx, task, root); err == nil {
		t.Fatal("accepted symlink escaping the task root")
	}
}

func TestFinishAlreadyRemovedFolder(t *testing.T) {
	ctx := context.Background()
	source := repo(t)
	root := t.TempDir()
	task, err := Create(ctx, source, root, "Task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git(ctx, source, "worktree", "remove", "--", task.Dir); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectFinish(ctx, task, root)
	if err != nil || !plan.Missing {
		t.Fatal("could not finish stale task entry", err)
	}
	result, err := Finish(ctx, task, root, plan.Version, true)
	if err != nil || !result.BranchKept {
		t.Fatal("missing-folder cleanup changed branches", err)
	}
}
