package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FinishPlan is a preview, not permission to discard later edits. Version is
// rechecked after the UI stops the task's programs and before any file change.
type FinishPlan struct {
	Missing bool
	Version string
	Dirty   bool
	Ignored bool
	Files   string
}
type FinishResult struct{ BranchKept bool }

// ownedWorktree requires the app's saved task identity, containment within the
// app's task root, and Git's live registration of that exact linked worktree.
// No arbitrary path from a cleanup request is accepted and no recursive delete
// is used. The root itself and the repository's primary checkout are refused.
func ownedWorktree(ctx context.Context, t Task, root string) error {
	if root == "" {
		return fmt.Errorf("task data folder is unavailable")
	}
	allowed, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks(t.Dir)
	if err != nil {
		return fmt.Errorf("task folder is unavailable: %w", err)
	}
	allowed, err = filepath.Abs(allowed)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	key := filepath.Base(target)
	if filepath.Dir(target) != allowed || len(key) != 16 {
		return fmt.Errorf("cleanup refused: folder is not an owned task directory")
	}
	if _, err := hex.DecodeString(key); err != nil || t.Branch != "wada/task-"+key {
		return fmt.Errorf("cleanup refused: task identity does not match its folder")
	}
	admin, err := git(ctx, target, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	common, err := git(ctx, target, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	if strings.TrimSpace(admin) == strings.TrimSpace(common) {
		return fmt.Errorf("cleanup refused: this is a primary repository")
	}
	records, err := git(ctx, t.Repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(records, "\x00\x00") {
		path, branch := "", ""
		for _, field := range strings.Split(record, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				path = strings.TrimPrefix(field, "worktree ")
			}
			if strings.HasPrefix(field, "branch ") {
				branch = strings.TrimPrefix(field, "branch ")
			}
		}
		registered, err := filepath.EvalSymlinks(path)
		if err == nil && registered == target && branch == "refs/heads/"+t.Branch {
			return nil
		}
	}
	return fmt.Errorf("cleanup refused: Git does not register this folder and branch for the task")
}

func InspectFinish(ctx context.Context, t Task, root string) (FinishPlan, error) {
	// A previous interrupted finish (or manual cleanup) may have removed the
	// folder already. Only UI metadata can be removed in this case; retain refs.
	if t.Dir != "" {
		if _, err := os.Lstat(t.Dir); os.IsNotExist(err) {
			return FinishPlan{Missing: true, Version: "missing:" + t.Dir}, nil
		}
	}

	if err := ownedWorktree(ctx, t, root); err != nil {
		return FinishPlan{}, err
	}
	changes, err := Review(ctx, t)
	if err != nil {
		return FinishPlan{}, err
	}
	if changes.Snapshot == "" {
		return FinishPlan{}, fmt.Errorf("some files could not be checked; inspect them before finishing")
	}
	head, err := git(ctx, t.Dir, "rev-parse", "HEAD")
	if err != nil {
		return FinishPlan{}, err
	}
	status, err := git(ctx, t.Dir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return FinishPlan{}, err
	}
	ignored, err := git(ctx, t.Dir, "ls-files", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		return FinishPlan{}, err
	}
	version := sha256.Sum256([]byte(head + "\x00" + changes.Snapshot + "\x00" + status + "\x00" + ignored))
	files := status
	if ignored != "" {
		files += "\nIgnored local files (not saved in Git):\n" + ignored
	}
	return FinishPlan{Version: hex.EncodeToString(version[:]), Dirty: status != "", Ignored: ignored != "", Files: files}, nil
}

// Finish saves ordinary changed files if requested, removes the working folder
// with Git, then removes the branch only if its commits are already in the
// original checkout. Unmerged commits always retain a recovery branch.
func Finish(ctx context.Context, t Task, root, version string, save bool) (FinishResult, error) {
	plan, err := InspectFinish(ctx, t, root)
	if err != nil {
		return FinishResult{}, err
	}
	if version == "" || plan.Version != version {
		return FinishResult{}, fmt.Errorf("task files changed; review the updated finish dialog before continuing")
	}
	if plan.Missing {
		return FinishResult{BranchKept: true}, nil
	}
	if save && plan.Dirty {
		if _, err := git(ctx, t.Dir, "add", "--all", "--", "."); err != nil {
			return FinishResult{}, fmt.Errorf("work kept: could not stage changes: %w", err)
		}
		if _, err := git(ctx, t.Dir, "commit", "-m", "Finish task: "+t.Name); err != nil {
			return FinishResult{}, fmt.Errorf("work kept: could not save a commit: %w", err)
		}
	}
	// Revalidate ownership immediately before removal. Save mode never forces
	// removal, so new/uncommitted files left by hooks or another process block it.
	if err := ownedWorktree(ctx, t, root); err != nil {
		return FinishResult{}, err
	}
	head, err := git(ctx, t.Dir, "rev-parse", "HEAD")
	if err != nil {
		return FinishResult{}, err
	}
	args := []string{"worktree", "remove"}
	if !save {
		args = append(args, "--force")
	}
	args = append(args, "--", t.Dir)
	if _, err := git(ctx, t.Repo, args...); err != nil {
		return FinishResult{}, fmt.Errorf("task kept: cleanup failed: %w", err)
	}
	result := FinishResult{BranchKept: true}
	original, err := git(ctx, t.Repo, "rev-parse", "HEAD")
	if err != nil {
		return result, nil
	}
	if _, err := git(ctx, t.Repo, "merge-base", "--is-ancestor", strings.TrimSpace(head), strings.TrimSpace(original)); err != nil {
		return result, nil
	}
	// Another checkout must not lose the branch it is using.
	live, err := git(ctx, t.Repo, "worktree", "list", "--porcelain", "-z")
	if err != nil || strings.Contains(live, "branch refs/heads/"+t.Branch+"\x00") {
		return result, nil
	}
	// Expected old value prevents deleting a branch that moved during cleanup.
	if _, err := git(ctx, t.Repo, "update-ref", "-d", "refs/heads/"+t.Branch, strings.TrimSpace(head)); err == nil {
		result.BranchKept = false
	}
	return result, nil
}
