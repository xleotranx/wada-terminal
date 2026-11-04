# Task workspaces

`New task…` in the sidebar or File menu (`⌘⇧T`) adds a named task to the
queue without creating a worktree. Choose a local repository and a built-in
agent, a program from `agents.json` or `programs.json`, or a shell. Select the
queued task and click `Start task` to create a new branch and worktree from
the then-current committed HEAD under the data folder's `tasks/` directory.
Uncommitted source changes are left in their original checkout. A repository needs a commit;
remote task creation is not supported yet.

Add an optional brief with context, constraints, and acceptance criteria when
creating a task; `Task brief…` lets you review or edit it later. Open **Tasks**
from the sidebar or View menu (`⌘⇧J`) to see and search tasks across repositories.
The view groups them by **Needs you**, **Ready to review**, **Working**, **Queued**,
**Open**, and **Finished**. Each action opens the appropriate task or terminal;
`Start…` opens the queued task so you can choose a program before starting.
`Back to workspace` returns to the current workspace. Their terminals,
splits, task name, branch, starting commit and last check survive reopening the
window. The brief is saved with the task. `Terminal` returns to the agent;
`Send task` submits the task name and brief as a prompt when the focused agent is
ready. The task view identifies the started program and selected terminal,
shows an explicit agent status report when available, and otherwise says
readiness is unknown. Bracketed paste does not prove an agent is ready. After a
successful input write, Wada records when and where it submitted the task to the
terminal transport; this does not confirm the agent received or accepted it.
The receipt is kept with the attempt, and changes to the brief after submission
are shown. `Send task again` explicitly resubmits the current task. Briefs
require the agent to enable bracketed paste, so their line breaks cannot submit
partial prompts. This action is disabled for ordinary shells.
Agent startup uses the agent's existing authentication and configuration.

`Changes` reviews the task against its starting commit, including later commits,
staged and unstaged edits and untracked text files. It refreshes every two seconds
while visible. Reviews and command output have bounded previews; large files are identified; tracked binary changes use Git’s binary-patch
representation and untracked binaries are labelled. `Refresh changes` also works
on demand. Task status uses explicit program reports: **Working**, **Needs you**
or **Ready to review**; without a report it says **Open**, never guessed success.
`Finish task…` opens the completion and cleanup dialog.

Enter an explicit shell command such as `go test ./...` and click `Run check`.
The command runs in the task worktree, with a two-minute limit, and the receipt
shows its command, timestamp, actual exit code and output. The default is
`Not run`. A changed review snapshot marks an old check as unverified. Checks do
not automatically merge, commit, approve agent commands, or declare a task done.
`Finish task…` saves ordinary changed files as a commit on the task branch,
stops its terminals, and removes its temporary folder. The finished task remains
in Tasks with its brief, branch, and last check receipt. If the branch is
retained, `Review saved changes` shows its committed diff against the starting
commit. The receipt predates cleanup and is not proof that the retained branch
was checked. Removing the task record does not remove a retained branch.

If you need another approach, select a program and choose `Start another attempt`.
Wada creates a fresh worktree from the original checkout's current committed
HEAD, then keeps the earlier attempt, its brief, branch, check receipt, and
integration receipt under `ATTEMPTS`. Select an earlier attempt to review or
integrate its retained branch. The task brief can be edited for the next attempt;
the earlier attempt keeps the brief it started with. Attempts start one at a
time, and the current workspace must be finished before starting another.
If there is nothing to save, use `Finish task`. You can explicitly choose to discard
unsaved changes instead. Ignored local files are listed and need a separate
acknowledgment because Git does not save them. Unmerged commits keep their branch
for recovery; branches already merged into the original checkout are removed.
The original project is unchanged by finishing. To apply a retained branch,
choose `Integrate…` on the finished task. The preview shows the original
checkout, target branch and exact commits; confirming fast-forwards the target
and keeps the task branch. Wada requires a clean checkout and rechecks the
preview before applying it. Ignored files are protected from overwrite. If
the branches have diverged, merge manually and resolve any conflicts. The
integration receipt records what was applied; it is not a live branch status.
Cancel leaves files and terminals alone. A failed save or cleanup retains the task
and explains the problem. Closing a task from its menu opens the same finish
dialog. Closing only the window keeps core-owned terminals running.
After a lost core, restored panes are fresh shells, as with ordinary workspaces.

The window API also accepts asynchronous task actions:

```sh
wada-terminal do task.new --name "Fix login redirect" --brief "Keep sessions and add a regression test" --folder /path/to/repo --agent opencode
wada-terminal ls
wada-terminal do task.review
wada-terminal do task.finish  # opens confirmation in the app; does not delete by itself
wada-terminal do task.check --command "go test ./..."
```

`task.new` returns `started`, not a completed task. Inspect `status` or `ls` for
the new space's `task` metadata; creation failures open the task dialog with the
error. Select the intended task space before review/check actions. `task.check`
retains its result in that metadata when finished.
