package terminal

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/kernel"
	"github.com/xleotranx/wada-kit/native"
	"wada-terminal/internal/tasks"
)

type integrationUI struct {
	open, loaded bool
	workspace    *workspace
	task         tasks.Task
	history      int
	job          kernel.Task
	plan         tasks.IntegrationPlan
	problem      string
}

func (t *Plugin) openIntegration(w *workspace) {
	if w == nil || w.task == nil || w.finishing || t.integrateUI.job.Running() {
		return
	}
	t.integrateUI = integrationUI{open: true, workspace: w, task: selectedTask(w), history: w.review.history}
	t.inspectIntegration()
}

func (t *Plugin) inspectIntegration() {
	f := &t.integrateUI
	if f.job.Running() || f.workspace == nil || f.workspace.task == nil {
		return
	}
	task := f.task
	f.loaded, f.problem = false, ""
	kernel.Go(t.ctx, &f.job, func(ctx context.Context) (tasks.IntegrationPlan, error) {
		return tasks.InspectIntegration(ctx, task)
	}, func(plan tasks.IntegrationPlan, err error) {
		if err != nil {
			f.problem = err.Error()
		} else {
			f.plan, f.loaded = plan, true
		}
		t.repaint()
	})
}

func (t *Plugin) integrateTask() {
	f := &t.integrateUI
	w := f.workspace
	if !f.loaded || f.job.Running() || w == nil || w.task == nil || w.finishing {
		return
	}
	task, plan := f.task, f.plan
	history := f.history
	w.finishing = true
	f.problem = ""
	kernel.Go(t.ctx, &f.job, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, tasks.Integrate(ctx, task, plan)
	}, func(_ struct{}, err error) {
		w.finishing = false
		if err != nil {
			f.loaded = false
			f.problem = err.Error()
			f.open = true
		} else {
			run := &w.task.Run
			if history > 0 && history <= len(w.task.Runs) {
				run = &w.task.Runs[history-1]
			}
			run.IntegratedCommit = plan.TaskHead
			run.IntegratedTarget = strings.TrimPrefix(plan.Target, "refs/heads/")
			t.saveWorkspaces()
			f.open = false
			t.host.Toast(ui.Toast{Title: "Task integrated", Description: "The original checkout contains the selected task commit."})
		}
		t.repaint()
	})
}

func (t *Plugin) integrationDialog(c *ui.Context) {
	f := &t.integrateUI
	native.Dialog(c, &f.open, 580, "Integrate task", func() {
		pal := chromeFor(c).pal
		ui.Text(c, "Integrate task").FontSize(20).Bold().TextColor(pal.Text)
		if f.workspace == nil || f.workspace.task == nil {
			return
		}
		task := &f.task
		ui.Text(c, task.Name).FontSize(14).Bold().TextColor(pal.Text)
		ui.Text(c, "Original checkout: "+task.Repo).FontSize(12).TextColor(pal.Muted)
		ui.Text(c, "Task branch: "+task.Branch).FontSize(12).TextColor(pal.Muted)
		if f.job.Running() {
			ui.Text(c, "Checking or integrating…").TextColor(pal.Muted)
			return
		}
		if f.problem != "" {
			ui.Text(c, f.problem).FontSize(12).TextColor(pal.Waiting)
		}
		if f.loaded {
			ui.Text(c, "Target branch: "+strings.TrimPrefix(f.plan.Target, "refs/heads/")).FontSize(12).TextColor(pal.Text)
			ui.Text(c, "Current commit: "+f.plan.Head).Font("monospace").FontSize(10).TextColor(pal.Muted)
			ui.Text(c, "Task commit: "+f.plan.TaskHead).Font("monospace").FontSize(10).TextColor(pal.Muted)
			if f.plan.AlreadyIntegrated {
				ui.Text(c, "The target already contains this task commit.").FontSize(12).TextColor(pal.Text)
			} else {
				ui.Text(c, "Fast-forward the target branch and update the original checkout. The task branch will be kept.").FontSize(12).TextColor(pal.Text)
			}
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "Cancel").OnClick(func() { f.open = false })
			ui.Button(c, "Refresh").OnClick(t.inspectIntegration)
			if f.loaded && !f.plan.AlreadyIntegrated {
				ui.PrimaryButton(c, "Confirm integration").OnClick(t.integrateTask)
			}
		})
	})
}
