package tasks

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IntegrationPlan records exactly which checkout and commits were inspected.
type IntegrationPlan struct {
	Target, Head, TaskHead string
	AlreadyIntegrated      bool
}

func InspectIntegration(ctx context.Context, task Task) (IntegrationPlan, error) {
	if !task.Finished || !task.BranchKept || !strings.HasPrefix(task.Branch, "wada/task-") || len(task.Branch) != len("wada/task-")+16 {
		return IntegrationPlan{}, fmt.Errorf("no retained task branch to integrate")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(task.Branch, "wada/task-")); err != nil {
		return IntegrationPlan{}, fmt.Errorf("invalid task branch")
	}
	if strings.TrimSpace(task.Repo) == "" {
		return IntegrationPlan{}, fmt.Errorf("original repository is unavailable")
	}
	branch, err := git(ctx, task.Repo, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return IntegrationPlan{}, fmt.Errorf("select a branch in the original checkout before integrating")
	}
	branch = strings.TrimSpace(branch)
	if !strings.HasPrefix(branch, "refs/heads/") || branch == "refs/heads/"+task.Branch {
		return IntegrationPlan{}, fmt.Errorf("select a different target branch in the original checkout")
	}
	status, err := git(ctx, task.Repo, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return IntegrationPlan{}, err
	}
	if status != "" {
		return IntegrationPlan{}, fmt.Errorf("the original checkout has uncommitted or untracked files; commit, stash, or move them before integrating")
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"} {
		path, err := git(ctx, task.Repo, "rev-parse", "--git-path", marker)
		if err != nil {
			return IntegrationPlan{}, err
		}
		path = strings.TrimSpace(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(task.Repo, path)
		}
		if _, err := os.Stat(path); err == nil {
			return IntegrationPlan{}, fmt.Errorf("finish the existing Git operation before integrating")
		} else if !os.IsNotExist(err) {
			return IntegrationPlan{}, err
		}
	}
	head, err := git(ctx, task.Repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return IntegrationPlan{}, err
	}
	taskHead, err := git(ctx, task.Repo, "rev-parse", "--verify", "refs/heads/"+task.Branch+"^{commit}")
	if err != nil {
		return IntegrationPlan{}, fmt.Errorf("the retained task branch is unavailable: %w", err)
	}
	plan := IntegrationPlan{Target: branch, Head: strings.TrimSpace(head), TaskHead: strings.TrimSpace(taskHead)}
	if _, err := git(ctx, task.Repo, "merge-base", "--is-ancestor", plan.TaskHead, plan.Head); err == nil {
		plan.AlreadyIntegrated = true
		return plan, nil
	}
	if _, err := git(ctx, task.Repo, "merge-base", "--is-ancestor", plan.Head, plan.TaskHead); err != nil {
		return IntegrationPlan{}, fmt.Errorf("the branches have diverged; merge the task branch manually and resolve any conflicts")
	}
	return plan, nil
}

// Integrate checks the preview again and lets Git protect ignored files.
func Integrate(ctx context.Context, task Task, expected IntegrationPlan) error {
	actual, err := InspectIntegration(ctx, task)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("the checkout or task branch changed; refresh the integration preview")
	}
	if actual.AlreadyIntegrated {
		return nil
	}
	if _, err := git(ctx, task.Repo, "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", actual.TaskHead); err != nil {
		return fmt.Errorf("integration failed; inspect the original checkout: %w", err)
	}
	return nil
}
