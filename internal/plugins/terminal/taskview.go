package terminal

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xleotranx/wada-kit/native"
)

func (t *Plugin) taskDialog(c *ui.Context) {
	native.Dialog(c, &t.tasksUI.open, 560, "New task", func() {
		pal := chromeFor(c).pal
		ui.Text(c, "New task").FontSize(20).Bold().TextColor(pal.Text)
		ui.Text(c, "Start from committed HEAD in an isolated Git worktree.").FontSize(12).TextColor(pal.Muted)
		ui.Text(c, "Task name").TextColor(pal.Text)
		ui.TextInput(c, &t.tasksUI.name).FillWidth()
		ui.Text(c, "Local repository").TextColor(pal.Text)
		ui.TextInput(c, &t.tasksUI.repo).FillWidth()
		ui.Text(c, "Agent").TextColor(pal.Text)
		labels, ids := t.taskAgents()
		ui.Select(c, &t.tasksUI.agent, labels).FillWidth()
		ui.Text(c, "When the agent is ready, use Send task to submit the name as its prompt.").FontSize(11).TextColor(pal.Muted)
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
			ui.PrimaryButton(c, "Create task").OnClick(func() {
				if err := t.createTask(t.tasksUI.name, t.tasksUI.repo, ids[t.tasksUI.agent]); err != nil {
					t.tasksUI.problem = err.Error()
				}
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
	return ui.Column(c).MinWidth(0).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Padding(10, 14).Gap(7).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, w.task.Name).FontSize(16).Bold().TextColor(pal.Text).Grow(1).SingleLine().Ellipsis("…")
				ui.Text(c, taskStatus(w)).FontSize(12).TextColor(pal.Muted)
			})
			ui.Text(c, w.task.Branch).Font("monospace").FontSize(10.5).TextColor(pal.Muted)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				taskViewButton(c, "Terminal", !r.changesView).OnClick(func() { r.changesView = false })
				taskViewButton(c, "Changes", r.changesView).OnClick(func() { r.changesView = true; t.reviewTask(w) })
				ui.Box(c).Grow(1)
				ui.Button(c, "Finish task…").Disabled(w.finishing).OnClick(func() { t.openFinishTask(w) })
			})
			if r.changesView {
				ui.Button(c, "Refresh changes").OnClick(func() { t.reviewTask(w) })
			} else {
				ui.Button(c, "Send task").Disabled(w.focus == nil || !w.focus.alive || !w.focus.isAgent()).OnClick(func() {
					if s := w.focus; s != nil && s.alive && s.isAgent() {
						w.task.Ready = false
						t.saveWorkspaces()
						t.prompted(s)
						s.write([]byte(w.task.Name + "\r"))
					}
				})
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
func reviewPreview(s string) string {
	if len(s) > 64000 {
		s = s[:64000] + "\n[Preview truncated]"
	}
	return strings.ToValidUTF8(s, "�")
}

// taskList keeps sibling tasks reachable without mixing them with pane rows.
func (t *Plugin) taskList(c *ui.Context, ch chrome) {
	repo := ""
	if t.current != nil {
		repo = t.current.dir
		if t.current.task != nil {
			repo = t.current.task.Repo
		}
	}
	var list []*workspace
	for _, w := range t.workspaces {
		if w.task != nil && w.task.Repo == repo {
			list = append(list, w)
		}
	}
	if len(list) == 0 {
		return
	}
	pal := ch.pal
	ui.Text(c, "TASKS").FontSize(10).Bold().TextColor(pal.Muted).Padding(10, 7, 4, 7)
	ui.Scroll(c).FillWidth().MaxHeight(180).Shrink(0).Children(func() {
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			for _, w := range list {
				row := ui.Column(c).FillWidth().Padding(6, 8).Gap(3).Radius(6)
				if w == t.current {
					row.Background(pal.Selected)
				} else if row.Hovered() {
					row.Background(pal.Hover)
				}
				row.Children(func() {
					ui.Text(c, w.task.Name).FontSize(12).TextColor(pal.Text).SingleLine().Ellipsis("…")
					ui.Text(c, taskStatus(w)).FontSize(10).TextColor(pal.Muted)
				})
				row.Cursor(ui.CursorPointer).OnClick(func() { t.selectWorkspace(w) })
			}
		})
	})
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
