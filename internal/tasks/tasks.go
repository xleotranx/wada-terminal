// Package tasks owns local Git worktrees and explicit validation receipts.
// It performs I/O only when called from a worker; it has no UI state.
package tasks

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type Task struct {
	Name  string `json:"name"`
	Brief string `json:"brief,omitempty"`
	Repo  string `json:"repo"`
	Run
	Runs []Run `json:"previousRuns,omitempty"`
}

// Run records one isolated attempt. The embedded run is the current attempt;
// earlier attempts remain in Runs after a retry.
type Run struct {
	SentAt           *time.Time `json:"sentAt,omitempty"`
	SentName         string     `json:"sentName,omitempty"`
	SentBrief        string     `json:"sentBrief,omitempty"`
	SentProgram      string     `json:"sentProgram,omitempty"`
	Brief            string     `json:"runBrief,omitempty"`
	StartedAt        time.Time  `json:"startedAt,omitempty"`
	FinishedAt       time.Time  `json:"finishedAt,omitempty"`
	IntegratedCommit string     `json:"integratedCommit,omitempty"`
	IntegratedTarget string     `json:"integratedTarget,omitempty"`
	Finished         bool       `json:"finished,omitempty"`
	BranchKept       bool       `json:"branchKept,omitempty"`
	Discarded        bool       `json:"discarded,omitempty"`
	Agent            string     `json:"agent,omitempty"`
	Ready            bool       `json:"ready,omitempty"`
	Dir              string     `json:"dir"`
	Branch           string     `json:"branch"`
	Base             string     `json:"base"`
	Check            *Check     `json:"check,omitempty"`
}

// ReviewFinished reads committed changes from a retained branch after cleanup.
func ReviewFinished(ctx context.Context, t Task) (Changes, error) {
	if !t.Finished || !t.BranchKept || !strings.HasPrefix(t.Branch, "wada/task-") || len(t.Branch) != len("wada/task-")+16 {
		return Changes{}, fmt.Errorf("no retained task branch to review")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(t.Branch, "wada/task-")); err != nil {
		return Changes{}, fmt.Errorf("invalid task branch")
	}
	if len(t.Base) != 40 && len(t.Base) != 64 {
		return Changes{}, fmt.Errorf("invalid task starting commit")
	}
	if _, err := hex.DecodeString(t.Base); err != nil {
		return Changes{}, fmt.Errorf("invalid task starting commit")
	}
	diff, err := git(ctx, t.Repo, "diff", "--no-ext-diff", "--no-textconv", "--binary", t.Base, "refs/heads/"+t.Branch, "--")
	if err != nil {
		return Changes{}, err
	}
	if diff == "" {
		diff = "No committed changes from the task's starting commit."
	}
	return Changes{Text: diff}, nil
}

// Draft validates a task without creating a branch or worktree.
func Draft(name, brief, repo, agent string) (Task, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Task{}, fmt.Errorf("enter a task name of 1–160 characters on one line")
	}
	if strings.TrimSpace(repo) == "" {
		return Task{}, fmt.Errorf("choose a local Git repository")
	}
	return Task{Name: name, Brief: brief, Repo: repo, Run: Run{Agent: agent}}, nil
}

type Check struct {
	Command  string    `json:"command"`
	ExitCode int       `json:"exitCode"`
	Output   string    `json:"output"`
	Finished time.Time `json:"finished"`
	Snapshot string    `json:"snapshot"`
}
type Changes struct{ Text, Snapshot string }

// cappedBuffer drains output while retaining only a bounded preview.
type cappedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := (1 << 20) - b.Len()
	if remain > 0 {
		b.Buffer.Write(p[:min(remain, n)])
	}
	if n > remain {
		b.truncated = true
	}
	return n, nil
}
func (b *cappedBuffer) text() string {
	if b.truncated {
		return b.String() + "\n[Output truncated]"
	}
	return b.String()
}
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var out cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", args[0], err, out.text())
	}
	if out.truncated {
		return "", fmt.Errorf("Git output exceeds review limit")
	}
	return out.String(), nil
}

func Create(ctx context.Context, repo, root, name string) (Task, error) {
	if strings.TrimSpace(repo) == "" {
		return Task{}, fmt.Errorf("choose a local Git repository")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Task{}, fmt.Errorf("enter a task name of 1–160 characters on one line")
	}
	top, err := git(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return Task{}, err
	}
	repo = strings.TrimSpace(top)
	base, err := git(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Task{}, fmt.Errorf("a task needs a repository with a commit: %w", err)
	}
	var id [8]byte
	if _, err = rand.Read(id[:]); err != nil {
		return Task{}, err
	}
	key := hex.EncodeToString(id[:])
	branch := "wada/task-" + key
	if err = os.MkdirAll(root, 0700); err != nil {
		return Task{}, err
	}
	dir, err := filepath.Abs(filepath.Join(root, key))
	if err != nil {
		return Task{}, err
	}
	if _, err = git(ctx, repo, "worktree", "add", "-b", branch, "--", dir, strings.TrimSpace(base)); err != nil {
		return Task{}, err
	}
	return Task{Name: name, Repo: repo, Run: Run{Dir: dir, Branch: branch, Base: strings.TrimSpace(base), StartedAt: time.Now()}}, nil
}

// Review includes commits since task creation, staged and unstaged edits, and
// untracked text files. External diff drivers and textconv are never executed.
func Review(ctx context.Context, t Task) (Changes, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if len(t.Base) != 40 && len(t.Base) != 64 {
		return Changes{}, fmt.Errorf("invalid task starting commit")
	}
	if _, err := hex.DecodeString(t.Base); err != nil {
		return Changes{}, fmt.Errorf("invalid task starting commit")
	}

	diff, err := git(ctx, t.Dir, "diff", "--no-ext-diff", "--no-textconv", "--binary", t.Base, "--")
	if err != nil {
		return Changes{}, err
	}
	files, err := git(ctx, t.Dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Changes{}, err
	}
	h := sha256.New()
	io.WriteString(h, diff)
	var view cappedBuffer
	view.Write([]byte(diff))
	complete := true
	for _, name := range strings.Split(files, "\x00") {
		if err := ctx.Err(); err != nil {
			return Changes{}, err
		}
		if name == "" {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return Changes{}, fmt.Errorf("invalid worktree path")
		}
		io.WriteString(h, "\x00"+name+"\x00")
		path := filepath.Join(t.Dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return Changes{}, err
		}
		fmt.Fprintf(h, "%v\x00%d\x00", info.Mode(), info.Size())
		fmt.Fprintf(&view, "\n--- Untracked: %s ---\n", name)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return Changes{}, err
			}
			io.WriteString(h, target)
			fmt.Fprintln(&view, "Symlink:", target)
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<20 {
			complete = false
			fmt.Fprintln(&view, "[File too large or non-regular; inspect separately]")
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return Changes{}, err
		}
		var preview cappedBuffer
		n, copyErr := io.Copy(io.MultiWriter(h, &preview), io.LimitReader(f, (64<<20)+1))
		err = copyErr
		if n > 64<<20 {
			complete = false
		}
		f.Close()
		if err != nil {
			return Changes{}, err
		}
		if strings.ContainsRune(preview.String(), '\x00') {
			fmt.Fprintln(&view, "[Binary file]")
		} else {
			view.Write([]byte(preview.text()))
		}
	}
	snapshot := ""
	if complete {
		snapshot = hex.EncodeToString(h.Sum(nil))
	}
	text := view.text()
	if text == "" {
		text = "No changes from the task's starting commit."
	}
	return Changes{Text: text, Snapshot: snapshot}, nil
}

// RunCheck executes only a command explicitly supplied by the user. The
// receipt is valid for review only when files stayed unchanged during the run.
func RunCheck(ctx context.Context, t Task, command string) (Check, error) {
	if strings.TrimSpace(command) == "" || len(command) > 4096 {
		return Check{}, fmt.Errorf("enter a check command (up to 4096 characters)")
	}
	before, err := Review(ctx, t)
	if err != nil {
		return Check{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = t.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var out cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	code := 0
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			code = exited.ExitCode()
		} else {
			return Check{}, err
		}
	}
	if ctx.Err() != nil {
		return Check{}, ctx.Err()
	}
	after, err := Review(ctx, t)
	if err != nil {
		return Check{}, err
	}
	snapshot := before.Snapshot
	if snapshot != after.Snapshot {
		snapshot = ""
	}
	return Check{Command: command, ExitCode: code, Output: out.text(), Finished: time.Now(), Snapshot: snapshot}, nil
}
