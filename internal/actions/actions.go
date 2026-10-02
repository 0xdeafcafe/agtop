// Package actions is what rush does to processes and checkouts itself:
// ending process trees, notifications, git worktrees. What an agent does
// through its own program is its adapter's (agent.Stopper and the rest).
package actions

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/0xdeafcafe/rush/internal/proc"
)

// KillTree SIGKILLs a process and everything under it, deepest first. It
// samples the process table afresh, refuses if root is no longer the process
// the user chose (same start time), and never touches rush or its parents.
func KillTree(root int, rootStart time.Time) (int, error) {
	tab, protected, err := treeOf(root, rootStart)
	if err != nil {
		return 0, err
	}
	_ = proc.Kill(root, syscall.SIGSTOP) // freeze it so it can't spawn more while we walk
	n := 0
	for _, pid := range tab.Descendants(root) {
		if !protected[pid] && proc.Kill(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
	return n, nil
}

// EndTree asks a process and everything under it to stop (SIGTERM, all of
// them, so a shell's children don't outlive it with nobody to stop them),
// then SIGKILLs whatever is still there after grace. It reports how many
// processes it ended.
func EndTree(root int, rootStart time.Time, grace time.Duration) (int, error) {
	tab, protected, err := treeOf(root, rootStart)
	if err != nil {
		return 0, err
	}
	var pids []int
	for _, pid := range tab.Descendants(root) {
		if !protected[pid] && proc.Kill(pid, syscall.SIGTERM) == nil {
			pids = append(pids, pid)
		}
	}
	alive := func() []int {
		now := proc.Snapshot(nil)
		var out []int
		for _, pid := range pids {
			if p := now.Procs[pid]; p != nil && p.Start.Equal(tab.Procs[pid].Start) {
				out = append(out, pid)
			}
		}
		return out
	}
	left := pids
	for end := time.Now().Add(grace); len(left) > 0 && time.Now().Before(end); {
		time.Sleep(150 * time.Millisecond)
		left = alive()
	}
	for _, pid := range left {
		_ = proc.Kill(pid, syscall.SIGKILL)
	}
	return len(pids), nil
}

// SignalTree sends sig to a process and everything under it: SIGSTOP to
// pause the lot, SIGCONT to let it go on. Root goes first, so a paused
// tree can't start more while it's walked.
func SignalTree(root int, rootStart time.Time, sig syscall.Signal) (int, error) {
	tab, protected, err := treeOf(root, rootStart)
	if err != nil {
		return 0, err
	}
	_ = proc.Kill(root, sig)
	n := 1
	for _, pid := range tab.Descendants(root) {
		if pid != root && !protected[pid] && proc.Kill(pid, sig) == nil {
			n++
		}
	}
	return n, nil
}

// treeOf samples the process table for a tree about to be ended, refusing
// if root is no longer the process the user chose (same start time); the
// protected set is rush and its parents.
func treeOf(root int, rootStart time.Time) (*proc.Table, map[int]bool, error) {
	tab := proc.Snapshot(nil)
	p := tab.Procs[root]
	if p == nil || !p.Start.Equal(rootStart) {
		return nil, nil, fmt.Errorf("process %d has already exited", root)
	}
	protected := map[int]bool{1: true}
	for pid := os.Getpid(); pid > 1; {
		protected[pid] = true
		q := tab.Procs[pid]
		if q == nil {
			break
		}
		pid = q.PPID
	}
	if protected[root] {
		return nil, nil, errors.New("that would kill rush itself")
	}
	return tab, protected, nil
}

func Terminate(pid int) error { return proc.Kill(pid, syscall.SIGTERM) }

// Notify posts a macOS notification; it spawns only on state transitions.
func Notify(title, body string) {
	script := fmt.Sprintf("display notification %q with title %q", body, title)
	c := exec.Command("osascript", "-e", script)
	if c.Start() == nil {
		go func() { _ = c.Wait() }()
	}
}

// RepoRoot is the top of the git checkout dir is in, or "" if it isn't in
// one.
func RepoRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NewWorktree makes a git worktree for the checkout dir is in, where Claude
// Code puts its own (.claude/worktrees/<name>), on a new branch named after
// it, and returns the folder that matches dir inside it.
func NewWorktree(dir, name string) (string, error) {
	return NewWorktreeOn(dir, name, "worktree-"+name, "")
}

// NewWorktreeOn is NewWorktree on a branch of your choosing, made from base
// (HEAD when it's "").
func NewWorktreeOn(dir, name, branch, base string) (string, error) {
	root := RepoRoot(dir)
	if root == "" {
		return "", fmt.Errorf("%s isn't in a git repository", dir)
	}
	path := filepath.Join(root, ".claude", "worktrees", name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("a worktree named %s already exists", name)
	}
	args := []string{"-C", root, "worktree", "add", "-b", branch, path}
	if base != "" {
		args = append(args, base)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
	}
	if rel, err := filepath.Rel(root, dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		if st, err := os.Stat(filepath.Join(path, rel)); err == nil && st.IsDir() {
			return filepath.Join(path, rel), nil
		}
	}
	return path, nil
}
