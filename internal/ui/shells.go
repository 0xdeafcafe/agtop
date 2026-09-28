package ui

import (
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

// watchShells shows the open conversation what its Bash calls have
// running: the shells Claude Code runs them in and every process under
// them, so a chain can say which of its commands runs now.
func (m *Model) watchShells() {
	c := m.host
	if c == nil || c.sess == nil || m.snap == nil || m.snap.Table == nil || c.sess.Live() == nil {
		return
	}
	pid := c.sess.Info.ClaudePID
	if pid == 0 {
		if a := m.agentByKey(c.key); a != nil {
			pid = a.PID
		}
	}
	if pid == 0 {
		return
	}
	tab := m.snap.Table
	var shells []convo.Shell
	for _, k := range tab.Children[pid] {
		p := tab.Procs[k]
		if p == nil || !shellComm(p.Comm) {
			continue
		}
		cmd := proc.CommandLine(k)
		if !strings.Contains(cmd, " eval ") {
			continue // an MCP server or hook run through a shell, not a Bash call
		}
		shells = append(shells, convo.Shell{Cmd: cmd, Start: p.Start, Kids: m.shellKids(tab, k, 0)})
	}
	c.sess.WatchShells(shells, time.Now())
}

func shellComm(c string) bool {
	switch strings.TrimPrefix(c, "-") {
	case "zsh", "bash", "sh":
		return true
	}
	return false
}

// shellKids is the process tree under pid, with each process's words.
func (m *Model) shellKids(tab *proc.Table, pid, depth int) []convo.ShellProc {
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
		out = append(out, convo.ShellProc{Args: args, Start: p.Start, Kids: m.shellKids(tab, k, depth+1)})
	}
	return out
}
