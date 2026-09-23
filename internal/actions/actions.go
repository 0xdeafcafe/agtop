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

	"github.com/0xdeafcafe/agents/internal/claude"
	"github.com/0xdeafcafe/agents/internal/daemon"
	"github.com/0xdeafcafe/agents/internal/proc"
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
	args = append(args, prompt)
	out, err := run(claudeCmd(acct, dir, args...))
	if err != nil {
		return "", err
	}
	return newID(out), nil
}

// Stop ends a session's process; its conversation is kept.
func Stop(acct claude.Account, short string) error {
	if err := (daemon.Client{Account: acct}).Kill(short); err == nil {
		return nil
	}
	_, err := run(claudeCmd(acct, "", "stop", short))
	return err
}

// Remove deletes a session, and its worktree when Claude Code deems it safe.
func Remove(acct claude.Account, short string) error {
	_, err := run(claudeCmd(acct, "", "rm", short))
	return err
}

func Reply(acct claude.Account, short, text string) error {
	return daemon.Client{Account: acct}.Reply(short, text)
}

// KillTree SIGKILLs a process and everything under it, deepest first.
func KillTree(tab *proc.Table, root int) int {
	n := 0
	for _, pid := range tab.Descendants(root) {
		if proc.Kill(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
	return n
}

func Terminate(pid int) error { return proc.Kill(pid, syscall.SIGTERM) }

// AttachFallback is the claude CLI's own attach, used when the socket refuses.
func AttachFallback(acct claude.Account, short string) *exec.Cmd {
	return claudeCmd(acct, "", "attach", short)
}

func Login(acct claude.Account) *exec.Cmd {
	return claudeCmd(acct, "", "auth", "login")
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
	if _, running := claude.ReadRoster(r.From).Workers[j.ID]; running || j.Live() {
		if err := Stop(r.From, j.ID); err != nil {
			return "", fmt.Errorf("could not stop the agent first: %w", err)
		}
		waitStopped(r.From, j.ID, 10*time.Second)
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
		args = append(args, r.Note)
	}
	out, err := run(claudeCmd(r.To, dir, args...))
	if err != nil {
		return "", err
	}
	// Resuming in the background forks the conversation into a new session.
	return newID(out), nil
}

// respawnFlags keeps the flags that describe the agent, not the old session.
func respawnFlags(flags []string) []string {
	var out []string
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case "--resume", "--session-id", "-r", "--fork-session":
			if flags[i] != "--fork-session" {
				i++
			}
			continue
		}
		out = append(out, flags[i])
	}
	return out
}

func waitStopped(acct claude.Account, short string, max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if _, ok := claude.ReadRoster(acct).Workers[short]; !ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
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
	tmp := dst + ".agents.tmp"
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
	_ = exec.Command("osascript", "-e", script).Start()
}
