package terminal

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/native"
	"wada-terminal/internal/tasks"
)

func (t *Plugin) taskDialog(c *ui.Context) {
	native.Dialog(c, &t.tasksUI.open, 560, "New task", func() {
		pal := chromeFor(c).pal
		ui.Text(c, "New task").FontSize(20).Bold().TextColor(pal.Text)
		ui.Text(c, "Save an idea now. Starting it creates an isolated worktree from committed HEAD.").FontSize(12).TextColor(pal.Muted)
		ui.Text(c, "Task name").TextColor(pal.Text)
		ui.TextInput(c, &t.tasksUI.name).Label("Task name").FillWidth()
		ui.Text(c, "Brief (optional)").TextColor(pal.Text)
		ui.TextArea(c, &t.tasksUI.brief).Label("Task brief").Placeholder("Context, constraints, and what done looks like").Lines(3, 5).FillWidth()
		ui.Text(c, "Local repository").TextColor(pal.Text)
		ui.TextInput(c, &t.tasksUI.repo).Label("Repository folder").FillWidth()
		ui.Text(c, "Agent").TextColor(pal.Text)
		labels, ids := t.taskAgents()
		ui.Select(c, &t.tasksUI.agent, labels).FillWidth()
		ui.Text(c, "Choose the program to use when you start this task.").FontSize(11).TextColor(pal.Muted)
		if t.tasksUI.problem != "" {
			ui.Text(c, t.tasksUI.problem).FontSize(12).TextColor(pal.Waiting)
		}
		ui.Row(c).Gap(8).Children(func() {
			if t.tasksUI.create.Running() {
				ui.Text(c, "Creating worktree…").TextColor(pal.Muted)
				return
			}
			ui.Button(c, "Cancel").OnClick(func() {
				t.tasksUI.open = false
			})
			ui.PrimaryButton(c, "Add to queue").OnClick(func() {
				if err := t.queueTask(t.tasksUI.name, t.tasksUI.brief, t.tasksUI.repo, ids[t.tasksUI.agent]); err != nil {
					t.tasksUI.problem = err.Error()
				}
			})
		})
	})
}

func (t *Plugin) taskBriefDialog(c *ui.Context) {
	native.Dialog(c, &t.tasksUI.editOpen, 560, "Task brief", func() {
		pal := chromeFor(c).pal
		ui.Text(c, "Task brief").FontSize(20).Bold().TextColor(pal.Text)
		ui.Text(c, "Capture context, constraints, and what done looks like.").FontSize(12).TextColor(pal.Muted)
		ui.TextArea(c, &t.tasksUI.editBrief).Label("Brief editor").Lines(5, 10).FillWidth()
		if t.tasksUI.editProblem != "" {
			ui.Text(c, t.tasksUI.editProblem).TextColor(pal.Waiting)
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "Cancel").OnClick(func() { t.tasksUI.editOpen = false })
			ui.PrimaryButton(c, "Save brief").OnClick(func() {
				if err := validateTaskBrief(t.tasksUI.editBrief); err != nil {
					t.tasksUI.editProblem = err.Error()
					return
				}
				if w := t.tasksUI.editTask; w != nil && w.task != nil {
					w.task.Brief = t.tasksUI.editBrief
					t.saveWorkspaces()
				}
				t.tasksUI.editOpen = false
			})
		})
	})
}

func (t *Plugin) taskContent(c *ui.Context, ch chrome, w *workspace) ui.Element {
	if w.review == nil {
		w.review = &taskReview{}
		if w.task.Check != nil {
			w.review.command = w.task.Check.Command
		}
	}
	r := w.review
	pal := ch.pal
	shown := selectedTask(w)
	if shown.Finished {
		return ui.Column(c).MinWidth(0).MinHeight(0).Padding(24).Gap(12).Children(func() {
			t.taskHistory(c, ch, w)
			ui.Text(c, "FINISHED").FontSize(11).Bold().TextColor(pal.Muted)
			ui.Text(c, w.task.Name).FontSize(20).Bold().TextColor(pal.Text)
			ui.Text(c, w.task.Repo).FontSize(12).TextColor(pal.Muted)
			if shown.Agent != "" {
				ui.Text(c, "Program: "+shown.Agent).FontSize(12).TextColor(pal.Muted)
			}
			if shown.Run.Brief != "" {
				ui.Text(c, "Brief: "+shown.Run.Brief).FontSize(12).TextColor(pal.Muted)
			}
			ui.Text(c, taskSubmissionLabel(shown)+" · Agent acceptance is not confirmed").FontSize(11).TextColor(pal.Muted)
			if shown.BranchKept {
				ui.Text(c, "Retained branch: "+shown.Branch).FontSize(12).TextColor(pal.Text)
			} else {
				ui.Text(c, "The branch was already merged and was removed during cleanup.").FontSize(12).TextColor(pal.Muted)
			}
			if shown.Discarded {
				ui.Text(c, "Unsaved changes were discarded during cleanup.").FontSize(12).TextColor(pal.Waiting)
			}
			if shown.IntegratedCommit != "" {
				ui.Text(c, "Integrated "+shown.IntegratedCommit+" into "+shown.IntegratedTarget+". The branch may have changed since.").FontSize(12).TextColor(pal.Muted)
			}
			if check := shown.Check; check != nil {
				ui.Text(c, fmt.Sprintf("Last check: %s · exit %d · %s. This receipt predates cleanup; verify the retained branch again.", check.Command, check.ExitCode, check.Finished.Format("Jan 2 15:04"))).FontSize(12).TextColor(pal.Muted)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "Review saved changes").Disabled(!shown.BranchKept || r.job.Running()).OnClick(func() { r.changesView = true; t.reviewTask(w) })
				ui.PrimaryButton(c, "Integrate…").Disabled(!shown.BranchKept || w.finishing).OnClick(func() { t.openIntegration(w) })
				if r.history == 0 {
					ui.Button(c, "Remove record…").Disabled(w.finishing).OnClick(func() { t.openFinishTask(w) })
				}
			})
			if r.history == 0 {
				labels, ids := t.taskAgents()
				if r.retryAgent == "" {
					for _, label := range labels {
						if ids[label] == shown.Agent {
							r.retryAgent = label
							break
						}
					}
					if r.retryAgent == "" {
						r.retryAgent = labels[0]
					}
				}
				ui.Row(c).Gap(8).Children(func() {
					ui.Select(c, &r.retryAgent, labels)
					ui.Button(c, "Edit task brief…").Disabled(w.finishing).OnClick(func() {
						t.tasksUI.editTask = w
						t.tasksUI.editBrief = w.task.Brief
						t.tasksUI.editProblem = ""
						t.tasksUI.editOpen = true
					})
					ui.Button(c, "Start another attempt").Disabled(w.finishing || r.job.Running() || t.tasksUI.create.Running() || t.integrateUI.job.Running()).OnClick(func() {
						if err := t.startTask(w, w.task.Name, w.task.Brief, w.task.Repo, ids[r.retryAgent]); err != nil {
							r.problem = err.Error()
						}
					})
				})
				if w.finishing {
					ui.Text(c, "Preparing attempt…").TextColor(pal.Muted)
				}
			}
			if r.problem != "" {
				ui.Text(c, r.problem).FontSize(12).TextColor(pal.Waiting)
			}
			if r.job.Running() {
				ui.Text(c, "Reading saved changes…").TextColor(pal.Muted)
			}
			if r.changesView && r.changes.Text != "" {
				ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() { ui.Text(c, reviewPreview(r.changes.Text)).Font("monospace").FontSize(12).TextColor(pal.Text) })
			}
		})
	}
	if w.task.Dir == "" {
		return ui.Column(c).MinWidth(0).MinHeight(0).Padding(24).Gap(12).Children(func() {
			ui.Text(c, "QUEUED").FontSize(11).Bold().TextColor(pal.Muted)
			ui.Text(c, w.task.Name).FontSize(20).Bold().TextColor(pal.Text)
			ui.Text(c, w.task.Repo).FontSize(12).TextColor(pal.Muted)
			brief := strings.TrimSpace(w.task.Brief)
			if brief == "" {
				brief = "No brief yet. Add context and what done looks like."
			}
			ui.Text(c, brief).FontSize(13).TextColor(pal.Text)
			labels, ids := t.taskAgents()
			if r.retryAgent == "" {
				for _, label := range labels {
					if ids[label] == w.task.Agent {
						r.retryAgent = label
						break
					}
				}
				if r.retryAgent == "" {
					r.retryAgent = labels[0]
				}
			}
			ui.Text(c, "Program for this attempt").FontSize(12).TextColor(pal.Text)
			ui.Select(c, &r.retryAgent, labels)
			ui.Text(c, "Starting creates a branch and worktree from the repository's current committed HEAD.").FontSize(12).TextColor(pal.Muted)
			ui.Row(c).Gap(8).Children(func() {
				ui.PrimaryButton(c, "Start task").Disabled(w.finishing || t.tasksUI.create.Running()).OnClick(func() {
					if err := t.startTask(w, w.task.Name, w.task.Brief, w.task.Repo, ids[r.retryAgent]); err != nil {
						r.problem = err.Error()
					}
				})
				ui.Button(c, "Edit brief…").OnClick(func() {
					t.tasksUI.editTask = w
					t.tasksUI.editBrief = w.task.Brief
					t.tasksUI.editProblem = ""
					t.tasksUI.editOpen = true
				})
				ui.Button(c, "Remove…").Disabled(w.finishing).OnClick(func() { t.openFinishTask(w) })
			})
			if w.finishing {
				ui.Text(c, "Creating worktree…").TextColor(pal.Muted)
			}
			if r.problem != "" {
				ui.Text(c, r.problem).FontSize(12).TextColor(pal.Waiting)
			}
		})
	}
	return ui.Column(c).MinWidth(0).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Padding(10, 14).Gap(7).Children(func() {
			t.taskHistory(c, ch, w)
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, w.task.Name).FontSize(16).Bold().TextColor(pal.Text).Grow(1).SingleLine().Ellipsis("…")
				ui.Text(c, taskStatus(w)).FontSize(12).TextColor(pal.Muted)
			})
			ui.Text(c, w.task.Branch).Font("monospace").FontSize(10.5).TextColor(pal.Muted)
			program := w.task.Agent
			labels, ids := t.taskAgents()
			for _, label := range labels {
				if ids[label] == w.task.Agent {
					program = label
					break
				}
			}
			focus := "No terminal selected"
			if w.focus != nil {
				focus = "Selected: " + w.focus.name
			}
			ui.Text(c, "Started: "+program+" · "+focus+" · "+taskAgentState(w.focus)).FontSize(11).TextColor(pal.Muted)
			ui.Text(c, taskSubmissionLabel(*w.task)+" · Agent acceptance is not confirmed").FontSize(11).TextColor(pal.Muted)
			brief := strings.TrimSpace(w.task.Brief)
			if brief == "" {
				brief = "Add context and acceptance criteria in the task brief."
			}
			ui.Text(c, brief).FontSize(12).TextColor(pal.Muted).SingleLine().Ellipsis("…")
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				taskViewButton(c, "Terminal", !r.changesView).OnClick(func() { r.changesView = false })
				taskViewButton(c, "Changes", r.changesView).OnClick(func() { r.changesView = true; t.reviewTask(w) })
				ui.Button(c, "Task brief…").OnClick(func() {
					t.tasksUI.editTask = w
					t.tasksUI.editBrief = w.task.Brief
					t.tasksUI.editProblem = ""
					t.tasksUI.editOpen = true
				})
				ui.Box(c).Grow(1)
				ui.Button(c, "Finish task…").Disabled(w.finishing).OnClick(func() { t.openFinishTask(w) })
			})
			if r.changesView {
				ui.Button(c, "Refresh changes").OnClick(func() { t.reviewTask(w) })
			} else {
				label := "Send task"
				if w.task.SentAt != nil {
					label = "Send task again"
				}
				ui.Button(c, label).Disabled(w.focus == nil || !w.focus.alive || !w.focus.isAgent()).OnClick(func() {
					if err := t.sendTask(w); err != nil {
						r.problem = err.Error()
					} else {
						r.problem = ""
					}
				})
			}
			if !r.changesView && r.problem != "" {
				ui.Text(c, r.problem).FontSize(12).TextColor(pal.Waiting)
			}

		})
		if !r.changesView {
			t.content(c, ch).Grow(1).FillWidth()
			return
		}
		ui.Column(c).FillWidth().Shrink(0).Padding(8, 14).Gap(6).Children(func() {
			ui.Text(c, "Run a check in this task's worktree").FontSize(12).TextColor(pal.Text)
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.TextInput(c, &r.command).Grow(1)
				if r.checkJob.Running() {
					ui.Text(c, "Running…").TextColor(pal.Muted)
				} else {
					ui.Button(c, "Run check").OnClick(func() { t.checkTask(w, r.command) })
				}
			})
			label := "Checks: Not run"
			if check := w.task.Check; check != nil {
				label = fmt.Sprintf("Last check: exit %d · %s", check.ExitCode, check.Finished.Format("Jan 2 15:04"))
				if r.problem != "" || check.Snapshot == "" || r.changes.Snapshot == "" || check.Snapshot != r.changes.Snapshot {
					label += " · Changes differ or are not verified"
				}
				ui.Text(c, check.Command).Font("monospace").FontSize(11).TextColor(pal.Muted)
			}
			ui.Text(c, label).FontSize(12).TextColor(pal.Text)
			if r.problem != "" {
				ui.Text(c, r.problem).FontSize(12).TextColor(pal.Waiting)
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(14).Gap(12).Children(func() {
				if r.job.Running() {
					ui.Text(c, "Reading changes…").TextColor(pal.Muted)
				} else {
					text := r.changes.Text
					if text == "" {
						text = "Choose Refresh changes to load the task diff."
					}
					ui.Text(c, reviewPreview(text)).Font("monospace").FontSize(12).TextColor(pal.Text)
				}
				if check := w.task.Check; check != nil {
					ui.Text(c, "Check output").Bold().TextColor(pal.Text)
					ui.Text(c, reviewPreview(checkOutput(check.Output))).Font("monospace").FontSize(11).TextColor(pal.Muted)
				}
			})
		})
	})
}

func (t *Plugin) taskHistory(c *ui.Context, ch chrome, w *workspace) {
	if len(w.task.Runs) == 0 {
		return
	}
	r := w.review
	ui.Scroll(c).FillWidth().MaxHeight(100).Shrink(0).Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			ui.Text(c, "ATTEMPTS").FontSize(10).Bold().TextColor(ch.pal.Muted)
			ui.Button(c, fmt.Sprintf("Current attempt %d · %s", len(w.task.Runs)+1, w.task.Agent)).Disabled(r.history == 0 || r.job.Running() || w.finishing || t.integrateUI.job.Running()).OnClick(func() { r.history = 0; r.changes = tasks.Changes{}; r.changesView = false; r.problem = "" })
			for i := len(w.task.Runs) - 1; i >= 0; i-- {
				index := i
				run := w.task.Runs[i]
				ui.Button(c, fmt.Sprintf("Attempt %d · %s", i+1, run.Agent)).Disabled(r.history == i+1 || r.job.Running() || w.finishing || t.integrateUI.job.Running()).OnClick(func() { r.history = index + 1; r.changes = tasks.Changes{}; r.changesView = false; r.problem = "" })
			}
		})
	})
}
func reviewPreview(s string) string {
	if len(s) > 64000 {
		s = s[:64000] + "\n[Preview truncated]"
	}
	return strings.ToValidUTF8(s, "�")
}

func taskViewButton(c *ui.Context, label string, selected bool) ui.Element {
	if selected {
		return ui.PrimaryButton(c, label)
	}
	return ui.Button(c, label)
}
func checkOutput(output string) string {
	if output == "" {
		return "Command produced no output."
	}
	return output
}
