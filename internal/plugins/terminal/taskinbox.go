package terminal

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"wada-terminal/internal/tasks"
)

var taskInboxOrder = []string{"Needs you", "Ready to review", "Working", "Queued", "Open", "Finished"}

func (t *Plugin) openTaskInbox() {
	t.taskInboxOpen = true
	t.workspaceMenu = false
	t.repaint()
}

func (t *Plugin) taskInboxGroups(query string) map[string][]*workspace {
	groups := make(map[string][]*workspace)
	query = strings.ToLower(strings.TrimSpace(query))
	for _, w := range t.workspaces {
		if w.task == nil {
			continue
		}
		text := strings.ToLower(w.task.Name + "\n" + w.task.Brief + "\n" + w.task.Repo + "\n" + w.task.Agent + "\n" + t.taskProgramLabel(w.task.Agent))
		if query != "" && !strings.Contains(text, query) {
			continue
		}
		status := taskStatus(w)
		groups[status] = append(groups[status], w)
	}
	return groups
}

func (t *Plugin) taskProgramLabel(agent string) string {
	labels, ids := t.taskAgents()
	for _, label := range labels {
		if ids[label] == agent {
			return label
		}
	}
	if agent == "" {
		return "Program not selected"
	}
	return agent
}

func taskInboxAction(status string) string {
	switch status {
	case "Needs you":
		return "Respond"
	case "Ready to review":
		return "Review"
	case "Working":
		return "Open terminal"
	case "Queued":
		return "Start…"
	case "Finished":
		return "Open result"
	default:
		return "Open task"
	}
}

func (t *Plugin) openInboxTask(w *workspace, status string) {
	if w == nil || w.task == nil {
		return
	}
	t.selectWorkspace(w)
	if w.review == nil {
		w.review = &taskReview{}
	}
	if w.review.job.Running() {
		w.review.job.Cancel()
	}
	if w.review.history != 0 {
		w.review.changes = tasks.Changes{}
	}
	w.review.history = 0
	w.review.changesView = false
	if status == "Needs you" {
		for _, s := range w.sessions(nil) {
			if s.alive && s.reported && (s.word == "blocked" || s.word == "error") {
				t.focusOn(s)
				break
			}
		}
	}
	if status == "Ready to review" {
		w.review.changesView = true
		t.reviewTask(w)
	}
	t.repaint()
}

// taskInbox is a view over live tasks; all state remains on the UI thread.
func (t *Plugin) taskInbox(c *ui.Context, ch chrome) {
	pal := ch.pal
	groups := t.taskInboxGroups(t.taskInboxQuery)
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(pal.PanelBg).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Padding(20, 24, 14, 24).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, "Tasks").FontSize(20).Bold().TextColor(pal.Text)
					ui.Text(c, "Work across your repositories").FontSize(12).TextColor(pal.Muted)
				})
				ui.Button(c, "Back to workspace").OnClick(func() { t.taskInboxOpen = false })
				ui.PrimaryButton(c, "New task…").OnClick(t.openTaskDialog)
			})
			ui.TextInput(c, &t.taskInboxQuery).Label("Search tasks").Placeholder("Search tasks, repositories or programs").FillWidth()
			ui.Text(c, fmt.Sprintf("%d tasks · %d need you · %d ready to review", total, len(groups["Needs you"]), len(groups["Ready to review"]))).FontSize(11).TextColor(pal.Muted)
		})
		ui.Box(c).FillWidth().Height(1).Shrink(0).Background(pal.Hairline)
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(12, 24, 24, 24).Gap(14).Children(func() {
				if total == 0 {
					if strings.TrimSpace(t.taskInboxQuery) == "" {
						ui.Text(c, "No tasks yet").FontSize(15).Bold().TextColor(pal.Text)
						ui.Text(c, "Create a task to capture work, then start an isolated attempt when you are ready.").FontSize(12).TextColor(pal.Muted)
					} else {
						ui.Text(c, "No matching tasks").FontSize(15).Bold().TextColor(pal.Text)
						ui.Text(c, "Try another name, repository, brief or program.").FontSize(12).TextColor(pal.Muted)
					}
					return
				}
				for _, status := range taskInboxOrder {
					group := groups[status]
					if len(group) == 0 {
						continue
					}
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Text(c, fmt.Sprintf("%s · %d", status, len(group))).FontSize(12).Bold().TextColor(pal.Muted).Padding(4, 0)
						for _, w := range group {
							t.taskInboxRow(c, ch, w, status)
						}
					})
				}
			})
		})
	})
}

func (t *Plugin) taskInboxRow(c *ui.Context, ch chrome, w *workspace, status string) {
	pal := ch.pal
	ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(12).Padding(10, 12).Radius(6).Background(pal.Raised).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, w.task.Name).FontSize(13).Bold().TextColor(pal.Text).SingleLine().Ellipsis("…")
			ui.Text(c, w.task.Repo+" · "+t.taskProgramLabel(w.task.Agent)).FontSize(11).TextColor(pal.Muted).SingleLine().Ellipsis("…")
		})
		action := taskInboxAction(status)
		ui.Button(c, action).Label(action + " · " + w.task.Name).Shrink(0).OnClick(func() { t.openInboxTask(w, status) })
	})
}
