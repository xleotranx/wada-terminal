package terminal

import (
	"context"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/native"
	"wada-terminal/internal/tasks"
)

type finishTaskUI struct {
	open                       bool
	workspace                  *workspace
	job                        kernel.Task
	plan                       tasks.FinishPlan
	loaded, discard, ignoredOK bool
	problem                    string
}

func (t *Plugin) openFinishTask(w *workspace) {
	if w == nil || w.finishing {
		return
	}
	f := &t.finishUI
	if f.job.Running() {
		f.open = true
		return
	}
	*f = finishTaskUI{open: true, workspace: w}
	if w.task != nil && w.task.Dir == "" {
		return
	}
	t.loadFinishTask()
}
func (t *Plugin) loadFinishTask() {
	f := &t.finishUI
	w := f.workspace
	if w == nil || w.task == nil {
		return
	}
	if w.review != nil && w.review.checkJob.Running() {
		f.problem = "A check is still running. Wait for it to finish, then refresh."
		return
	}
	task := *w.task
	root := filepath.Join(t.host.DataDir(), "tasks")
	f.loaded = false
	f.problem = ""
	f.ignoredOK = false
	kernel.Go(t.ctx, &f.job, func(ctx context.Context) (tasks.FinishPlan, error) { return tasks.InspectFinish(ctx, task, root) }, func(plan tasks.FinishPlan, err error) {
		if err != nil {
			f.problem = err.Error()
		} else {
			f.plan = plan
			f.loaded = true
		}
		t.repaint()
	})
}
func (t *Plugin) finishTask() {
	f := &t.finishUI
	w := f.workspace
	if !f.loaded || f.job.Running() || w == nil || w.task == nil {
		return
	}
	if f.plan.Ignored && !f.ignoredOK {
		return
	}
	if w.review != nil && w.review.checkJob.Running() {
		f.problem = "A check is still running. Wait for it to finish, then refresh."
		return
	}
	if w.review != nil {
		w.review.job.Cancel()
	}
	task := *w.task
	version := f.plan.Version
	save := !f.discard
	root := filepath.Join(t.host.DataDir(), "tasks")
	// Stop task-owned programs before the worker saves or removes their folder.
	w.finishing = true
	for _, s := range w.sessions(nil) {
		s.kill()
	}
	kernel.Go(t.ctx, &f.job, func(ctx context.Context) (tasks.FinishResult, error) {
		return tasks.Finish(ctx, task, root, version, save)
	}, func(result tasks.FinishResult, err error) {
		w.finishing = false
		if err != nil {
			f.problem = err.Error()
			f.loaded = false
			f.open = true
			t.repaint()
			return
		}
		f.open = false
		w.tabs = nil
		w.focus = nil
		w.active = 0
		w.dir = task.Repo
		w.task.Dir = ""
		w.task.Finished = true
		w.task.FinishedAt = time.Now()
		w.task.BranchKept = result.BranchKept
		w.task.Discarded = !save
		w.review = nil
		t.saveWorkspaces()
		description := "Temporary folder removed. The task remains in Tasks."
		if result.BranchKept {
			description += " Saved commits remain on " + task.Branch + "."
		} else {
			description += " The merged task branch was removed too."
		}
		t.host.Toast(ui.Toast{Title: "Task finished", Description: description})
		t.repaint()
	})
}
func (t *Plugin) finishTaskDialog(c *ui.Context) {
	f := &t.finishUI
	native.Dialog(c, &f.open, 580, "Finish task", func() {
		pal := chromeFor(c).pal
		ui.Text(c, "Finish task").FontSize(20).Bold().TextColor(pal.Text)
		if f.workspace == nil || f.workspace.task == nil {
			return
		}
		ui.Text(c, f.workspace.task.Name).FontSize(14).Bold().TextColor(pal.Text)
		if f.workspace.task.Dir == "" {
			message := "Remove this queued task? No workspace has been created."
			if f.workspace.task.Finished {
				message = "Remove this task record? Any retained branch will remain in the repository."
			}
			ui.Text(c, message).FontSize(12).TextColor(pal.Muted)
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { f.open = false })
				ui.PrimaryButton(c, "Remove task").OnClick(func() { f.open = false; t.dropWorkspace(f.workspace) })
			})
			return
		}
		ui.Text(c, "This stops the task's terminals and removes its temporary folder. The task record remains.").FontSize(12).TextColor(pal.Muted)
		ui.Text(c, "Changed files are saved as a commit on the task branch. Unmerged commits stay there for recovery; your original project is unchanged.").FontSize(12).TextColor(pal.Muted)
		if f.job.Running() {
			ui.Text(c, "Checking files or finishing task…").TextColor(pal.Muted)
			return
		}
		if f.problem != "" {
			ui.Text(c, f.problem).FontSize(12).TextColor(pal.Waiting)
		}
		if f.loaded {
			label := "No unsaved changes."
			if f.plan.Missing {
				label = "The temporary folder is already gone. The task record and branch are retained."
			}
			if f.plan.Dirty {
				label = "Unsaved changes will be saved before cleanup."
			}
			ui.Text(c, label).FontSize(12).TextColor(pal.Text)
			if f.plan.Files != "" {
				ui.Scroll(c).FillWidth().MaxHeight(180).Children(func() { ui.Text(c, reviewPreview(f.plan.Files)).Font("monospace").FontSize(11).TextColor(pal.Muted) })
			}
			if f.plan.Ignored {
				ui.Checkbox(c, &f.ignoredOK, "Remove the listed ignored local files too (they will not be saved)")
			}
			if f.plan.Dirty {
				ui.Checkbox(c, &f.discard, "Discard unsaved changes instead of saving them")
			}
			if f.discard {
				ui.Text(c, "Unsaved files will be permanently removed. Existing commits are kept if unmerged.").FontSize(12).TextColor(pal.Waiting)
			}
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "Cancel").OnClick(func() { f.open = false })
			if !f.loaded {
				ui.Button(c, "Refresh").OnClick(t.loadFinishTask)
			}
			label := "Finish task"
			if f.plan.Dirty {
				label = "Save work & finish"
			}
			if f.discard {
				label = "Discard changes & finish"
			}
			ui.PrimaryButton(c, label).Disabled(!f.loaded || (f.plan.Ignored && !f.ignoredOK)).OnClick(t.finishTask)
		})
	})
}
