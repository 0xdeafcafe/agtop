// Package pgguard ends a process group when the process that started it
// goes away, however it goes. A host killed with SIGKILL can't stop the
// agent it runs, and an agent told nothing runs its turn to the end, still
// writing the transcript the next host resumes. Closing its stdin isn't
// enough either: an agent mid-turn only reads it once the turn is over.
//
// The guard is a shell in its own process group, reading a pipe only this
// process writes to. The pipe closes when this process exits, killed or
// not, and the shell then ends the group: SIGTERM first, SIGKILL if it
// hasn't gone a second later.
package pgguard

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

type Guard struct {
	cmd *exec.Cmd
	w   *os.File
}

const script = `read -r _
kill -s TERM -- "-$1" 2>/dev/null || exit 0
for _ in 1 2 3 4 5; do
  sleep 0.2
  kill -0 -- "-$1" 2>/dev/null || exit 0
done
kill -s KILL -- "-$1" 2>/dev/null
exit 0`

// Watch guards the process group led by pgid. With no shell to run there
// is no guard, and nil is returned: Release on it does nothing.
func Watch(pgid int) *Guard {
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	defer r.Close()
	cmd := exec.Command("/bin/sh", "-c", script, "agtop-guard", strconv.Itoa(pgid))
	cmd.Stdin = r
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		return nil
	}
	go func() { _ = cmd.Wait() }()
	return &Guard{cmd: cmd, w: w}
}

// Release lets the guard go once the group has ended on its own. It's
// killed before its pipe closes, so it never signals a group id that's
// gone and may be another's.
func (g *Guard) Release() {
	if g == nil {
		return
	}
	_ = g.cmd.Process.Kill()
	_ = g.w.Close()
}
