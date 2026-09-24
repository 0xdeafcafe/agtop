// Package actions changes things only through Claude Code itself: the
// daemon's control socket first, the claude CLI as the fallback.
package actions

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

var (
	shortID  = regexp.MustCompile(`\b[0-9a-f]{8}\b`)
	attachID = regexp.MustCompile(`claude attach ([0-9a-f]{8})`)
)

// newID reads the session id claude --bg prints in its "claude attach <id>" hint.
func newID(out string) string {
	if m := attachID.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return shortID.FindString(out)
}

func claudeCmd(acct claude.Account, dir string, args ...string) *exec.Cmd {
	c := exec.Command("claude", args...)
	c.Env = acct.Env()
	c.Dir = dir
	return c
}

func run(c *exec.Cmd) (string, error) {
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	s := strings.TrimSpace(out.String())
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return s, fmt.Errorf("%s", lastLine(s))
	}
	return s, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Dispatch starts a new background session and returns its short id.
func Dispatch(acct claude.Account, dir, prompt string, flags ...string) (string, error) {
	args := append([]string{"--bg"}, flags...)
	args = append(args, "--", prompt) // a prompt starting with - is still a prompt
	out, err := run(claudeCmd(acct, dir, args...))
	if err != nil {
		return "", err
	}
	return newID(out), nil
}

// Stop ends a session's process; its conversation is kept.
// Stop ends a session's process and keeps its conversation. When the daemon
// and the CLI both fail, or the process outlives them, pid gets a SIGTERM.
func Stop(acct claude.Account, short string, pid int) error {
	err := (daemon.Client{Account: acct}).Kill(short)
	if err != nil {
		_, err = run(claudeCmd(acct, "", "stop", short))
	}
	if pid == 0 || waitExit(pid, 5*time.Second) {
		return err
	}
	return Terminate(pid)
}

func waitExit(pid int, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// Remove deletes a session, and its worktree when Claude Code deems it safe.
func Remove(acct claude.Account, short string) error {
	_, err := run(claudeCmd(acct, "", "rm", short))
	return err
}

func Reply(acct claude.Account, short, text string) error {
	c := daemon.Client{Account: acct}
	if !c.Running() {
		return fmt.Errorf("the background agents daemon isn't running, so replies can't be sent; open the agent with enter instead")
	}
	return c.Reply(short, text)
}

// KillTree SIGKILLs a process and everything under it, deepest first. It
// samples the process table afresh, refuses if root is no longer the process
// the user chose (same start time), and never touches agtop or its parents.
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

// treeOf samples the process table for a tree about to be ended, refusing
// if root is no longer the process the user chose (same start time); the
// protected set is agtop and its parents.
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
		return nil, nil, fmt.Errorf("that would kill agtop itself")
	}
	return tab, protected, nil
}

func Terminate(pid int) error { return proc.Kill(pid, syscall.SIGTERM) }

// AttachFallback is the claude CLI's own attach, used when the socket refuses.
func AttachFallback(acct claude.Account, short string) *exec.Cmd {
	return claudeCmd(acct, "", "attach", short)
}

func Login(acct claude.Account) *exec.Cmd {
	return claudeCmd(acct, "", "auth", "login")
}

// Screen opens Claude Code's own screen for a slash command (/plugin,
// /hooks, …) in dir. Nothing is sent to a model, and no conversation is
// kept: it's a fresh Claude Code that starts on the command. hint is
// printed above it (how to get back).
func Screen(acct claude.Account, dir, command, hint string) *exec.Cmd {
	c := exec.Command("sh", "-c", `printf '\033[2J\033[H%s\n\n' "$1"; exec claude "/$2"`, "sh", hint, command)
	c.Env = acct.Env()
	c.Dir = dir
	return c
}

// Relaunch moves a conversation: stop it, make its transcript visible to the
// target account and folder, and resume it there with its original flags.
type Relaunch struct {
	From    claude.Account
	To      claude.Account
	Job     claude.Job
	Dir     string   // new working folder; empty keeps the old one
	AddDirs []string // extra folders to grant
	Note    string   // first message after resuming
}

func (r Relaunch) Run() (string, error) {
	j := r.Job
	if j.SessionID == "" || j.TranscriptPath == "" {
		return "", fmt.Errorf("this agent has no saved conversation to move")
	}
	dir := r.Dir
	if dir == "" {
		dir = j.Cwd
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("folder not found: %s", dir)
	}
	if _, running := claude.ReadRoster(r.From).Workers[j.ID]; running {
		if err := Stop(r.From, j.ID, 0); err != nil {
			return "", fmt.Errorf("could not stop the agent first: %w", err)
		}
		if !waitStopped(r.From, j.ID, 10*time.Second) {
			return "", fmt.Errorf("the agent is still stopping; try again in a moment")
		}
	}
	dst := filepath.Join(r.To.ProjectsDir(), claude.ProjectSlug(dir), j.SessionID+".jsonl")
	if dst != j.TranscriptPath {
		if err := copyFile(j.TranscriptPath, dst); err != nil {
			return "", fmt.Errorf("could not copy the conversation: %w", err)
		}
	}
	args := []string{"--bg", "--resume", j.SessionID}
	args = append(args, respawnFlags(j.RespawnFlags)...)
	for _, d := range r.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if r.Note != "" {
		args = append(args, "--", r.Note) // --add-dir takes many values; end them first
	}
	out, err := run(claudeCmd(r.To, dir, args...))
	if err != nil {
		return "", err
	}
	// Resuming in the background forks the conversation into a new session.
	m := attachID.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("resumed, but Claude Code didn't say the new session's id; it will appear in the list")
	}
	return m[1], nil
}

// respawnFlags keeps the flags that describe the agent, not the old session.
func respawnFlags(flags []string) []string {
	var out []string
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		switch {
		case f == "--resume" || f == "--session-id" || f == "-r":
			i++
			continue
		case f == "--fork-session" || f == "-c" || f == "--continue",
			strings.HasPrefix(f, "--resume=") || strings.HasPrefix(f, "--session-id="):
			continue
		}
		out = append(out, flags[i])
	}
	return out
}

func waitStopped(acct claude.Account, short string, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if _, ok := claude.ReadRoster(acct).Workers[short]; !ok {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".agtop.tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

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
