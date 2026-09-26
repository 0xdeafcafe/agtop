package headless

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// A guard ends Claude Code's process group when the process that started
// it goes away, however it goes: a host killed with SIGKILL can't stop its
// Claude, and Claude Code, told nothing, runs its turn to the end and keeps
// writing the transcript the next host resumes. Closing its stdin isn't
// enough either: claude -p only exits on EOF once the turn is over.
//
// The guard is a shell in its own process group, reading a pipe only this
// process writes to. The pipe closes when this process exits, killed or
// not, and the shell then ends the group, SIGTERM first and SIGKILL if it
// hasn't gone a second later.
type guard struct {
	cmd *exec.Cmd
	w   *os.File
}

const guardScript = `read -r _
kill -s TERM -- "-$1" 2>/dev/null || exit 0
for _ in 1 2 3 4 5; do
  sleep 0.2
  kill -0 -- "-$1" 2>/dev/null || exit 0
done
kill -s KILL -- "-$1" 2>/dev/null
exit 0`

// startGuard watches the process group led by pgid. Without a shell there
// is no guard, and the session runs as it did before.
func startGuard(pgid int) *guard {
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	defer r.Close()
	cmd := exec.Command("/bin/sh", "-c", guardScript, "agtop-guard", strconv.Itoa(pgid))
	cmd.Stdin = r
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		return nil
	}
	go func() { _ = cmd.Wait() }()
	return &guard{cmd: cmd, w: w}
}

// end lets the guard go once the session has ended on its own: it's killed
// before its pipe closes, so it never signals a group id that's gone.
func (g *guard) end() {
	if g == nil {
		return
	}
	_ = g.cmd.Process.Kill()
	_ = g.w.Close()
}
