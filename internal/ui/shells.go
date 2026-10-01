package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/proc"
)

// watchShells shows the open conversation what its Bash calls have
// running: the shells Claude Code runs them in and every process under
// them, so a chain can say which of its commands runs now. A subagent
// runs in the same process, so the one opened is shown its own the same
// way. Each process's words are read off the UI's goroutine; they land as
// a shellsMsg.
func (m *Model) watchShells() tea.Cmd {
	c := m.host
	if c == nil || c.sess == nil || m.snap == nil || m.snap.Table == nil {
		return nil
	}
	subLive := c.subTail != nil && c.subTail.Sess.Live() != nil
	if c.sess.Live() == nil && len(c.sess.RunningJobs()) == 0 && !subLive {
		return nil
	}
	pid := c.sess.Info.ClaudePID
	if pid == 0 {
		if a := m.agentByKey(c.key); a != nil {
			pid = a.PID
		}
	}
	if pid == 0 {
		return nil
	}
	tab, key := m.snap.Table, c.key
	return func() tea.Msg {
		var shells []convo.Shell
		for _, k := range tab.Children[pid] {
			p := tab.Procs[k]
			if p == nil || !shellComm(p.Comm) {
				continue
			}
			args := proc.Args(k)
			command := shellCommand(args)
			cmd := strings.Join(args, " ")
			if command == "" {
				continue
			}
			// Native eval wrappers retain their existing hook-rewrite matching.
			// Other shells require an exact command match in WatchShells, so an
			// MCP server or hook cannot acquire a tool call by timing alone.
			if strings.Contains(cmd, " eval ") {
				command = ""
			}
			shells = append(shells, convo.Shell{Cmd: cmd, Command: command, Start: p.Start, Kids: shellKids(tab, k, 0)})
		}
		return shellsMsg{key: key, shells: shells}
	}
}

// shellsMsg is what the open conversation's shells run, read by watchShells.
type shellsMsg struct {
	key    string
	shells []convo.Shell
}

// onShells hands the conversation still open what its shells run.
func (m *Model) onShells(msg shellsMsg) {
	if c := m.host; c != nil && c.sess != nil && c.key == msg.key {
		c.sess.WatchShells(msg.shells, time.Now())
		if c.subTail != nil {
			c.subTail.Sess.WatchShells(msg.shells, time.Now())
		}
	}
}

func shellComm(c string) bool {
	switch strings.TrimPrefix(c, "-") {
	case "zsh", "bash", "sh":
		return true
	}
	return false
}

// shellKids is the process tree under pid, with each process's words.
func shellKids(tab *proc.Table, pid, depth int) []convo.ShellProc {
	if depth > 6 {
		return nil
	}
	var out []convo.ShellProc
	for _, k := range tab.Children[pid] {
		p := tab.Procs[k]
		if p == nil {
			continue
		}
		args := proc.Args(k)
		if len(args) == 0 {
			args = []string{p.Comm}
		}
		out = append(out, convo.ShellProc{PID: k, Args: args, Start: p.Start, Kids: shellKids(tab, k, depth+1)})
	}
	return out
}

// pickedShell is the tool call ID of the running Bash call picked: its
// task in the dock or the background view, or its step in the conversation.
func (m *Model) pickedShell(c *hostConn) string {
	if j := m.pickedJob(c); j != nil && j.Running() && c.sess.JobKind(j) == "shell" {
		return j.ToolUseID
	}
	if _, id, ok := strings.Cut(c.sel, ":s:"); ok {
		if st := c.sess.Step(id); st != nil && st.Status == convo.Running && st.Tool == "Bash" {
			return id
		}
	}
	return ""
}

// shellKey acts on a picked running Bash call: k kills the command of its
// chain that runs now and lets the chain go on, and on its step in the
// conversation, b and x do what they do on its task.
func (m *Model) shellKey(c *hostConn, s string, empty bool) (tea.Cmd, bool) {
	if !empty || s != "k" && s != "b" && s != "x" {
		return nil, false
	}
	id := m.pickedShell(c)
	if id == "" {
		return nil, false
	}
	if s == "k" {
		return m.killPart(c, id), true
	}
	if !strings.Contains(c.sel, ":s:") {
		return nil, false // the task's own keys
	}
	var job *convo.Job
	for _, j := range c.sess.RunningJobs() {
		if j.ToolUseID == id {
			job = j
		}
	}
	switch {
	case c.client == nil:
		m.flash("rush can background or stop a call only in a session it runs · k still kills the command running now", true)
		return nil, true
	case s == "b" && job != nil && job.Background:
		m.flash("that's already in the background", false)
		return nil, true
	case s == "b" && job != nil:
		return m.backgroundJob(c, job), true
	case s == "b":
		// Claude Code hasn't said it's a task yet; the call's ID will do.
		if c.sess.Info.Proto < 3 {
			return m.backgroundJob(c, nil), true
		}
		m.flash("moved the shell to the background · the turn carries on", false)
		cl := c.client
		return hostCmd(func() error { return cl.Background(id) }), true
	case job != nil:
		return m.stopJob(c, job), true
	}
	m.flash("Claude Code hasn't said it's a task yet · try again in a moment", false)
	return nil, true
}

// killPart ends the command of a running chain that runs now, and all it
// started, leaving the shell to go on as it would after a failure.
func (m *Model) killPart(c *hostConn, id string) tea.Cmd {
	rp, ok := c.sess.RunningPart(id)
	if !ok {
		m.flash("rush hasn't seen which command of it runs yet · x stops the whole call", true)
		return nil
	}
	what := rp.Command
	if f := strings.Fields(what); len(f) > 3 {
		what = strings.Join(f[:3], " ") + " …"
	}
	then := map[string]string{
		"stops":      "the chain stops there (&&) and Claude hears it failed",
		"carries on": "the chain carries on with the next command",
		"ends":       "it was the last command, so the call ends",
	}[rp.Then]
	m.flash("killing "+what+" · "+then, false)
	procs := rp.Procs
	return hostCmd(func() error {
		var errs []error
		for _, p := range procs {
			if _, err := actions.EndTree(p.PID, p.Start, 3*time.Second); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	})
}

// shellCommand reads argv, not a flattened command line: spaces and quotes in
// the script are significant. Interactive shells and script-file launches do
// not provide a command we can safely associate with a tool call.
func shellCommand(args []string) string {
	if len(args) < 3 || !shellComm(filepath.Base(args[0])) {
		return ""
	}
	for i := 1; i < len(args)-1; i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			return ""
		}
		if !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") {
			return args[i+1]
		}
	}
	return ""
}
