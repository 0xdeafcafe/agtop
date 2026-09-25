package host

import (
	"slices"
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/proc"
)

// endStrays ends any Claude Code still running this session for a host
// that is gone: one left by a host from before guards, or one whose guard
// was killed along with it. It has init as its parent, and the session id
// on its command line; two of them would write the same transcript.
func endStrays(sessionID string) []int {
	if sessionID == "" {
		return nil
	}
	var ended []int
	for pid, p := range proc.Snapshot(nil).Procs {
		if p.PPID != 1 || !isHeadlessClaude(proc.Args(pid), sessionID) {
			continue
		}
		endGroup(pid)
		ended = append(ended, pid)
	}
	return ended
}

// isHeadlessClaude says whether args run claude -p on sessionID, as
// headless.Start does.
func isHeadlessClaude(args []string, sessionID string) bool {
	if !slices.Contains(args, "-p") || !slices.Contains(args, "stream-json") {
		return false
	}
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "--resume" || args[i] == "--session-id") && args[i+1] == sessionID {
			return true
		}
	}
	return false
}

// endGroup ends pid, with its process group when it leads one, as a
// headless Claude does: SIGTERM, then SIGKILL if it's still there a
// second later.
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
