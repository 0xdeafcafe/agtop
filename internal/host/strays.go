package host

import (
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

// endStrays ends any agent still running this session for a host that is
// gone: one left by a host from before guards, or one whose guard was
// killed along with it. It has init as its parent, and its adapter knows
// it by its command line.
func endStrays(kind, sessionID string) []int {
	if sessionID == "" {
		return nil
	}
	a, ok := agent.Get(agent.Migrated(kind))
	if !ok {
		return nil
	}
	o, ok := a.(agent.Orphans)
	if !ok {
		return nil
	}
	var ended []int
	for pid, p := range proc.Snapshot(nil).Procs {
		if p.PPID != 1 || !o.RunsSession(proc.Args(pid), sessionID) {
			continue
		}
		endGroup(pid)
		ended = append(ended, pid)
	}
	return ended
}

// endGroup ends pid, with its process group when it leads one, as a
// session's agent does: SIGTERM, then SIGKILL if it's still there a second
// later.
func endGroup(pid int) {
	target := pid
	if pg, err := syscall.Getpgid(pid); err == nil && pg == pid && pg != syscall.Getpgrp() {
		target = -pid
	}
	_ = syscall.Kill(target, syscall.SIGTERM)
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(target, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(target, syscall.SIGKILL)
}
