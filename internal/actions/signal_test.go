package actions

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/proc"
)

// A paused tree stops (ps says T) and goes on again when let go.
func TestSignalTreePausesAndResumes(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	pid := cmd.Process.Pid
	var start time.Time
	for range 50 {
		if p := proc.Snapshot(nil).Procs[pid]; p != nil {
			start = p.Start
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	state := func() string {
		out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		return strings.TrimSpace(string(out))
	}
	if _, err := SignalTree(pid, start, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if s := state(); !strings.HasPrefix(s, "T") {
		t.Fatalf("paused, ps says %q", s)
	}
	if _, err := SignalTree(pid, start, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if s := state(); strings.HasPrefix(s, "T") {
		t.Fatalf("let go, ps still says %q", s)
	}
	if _, err := SignalTree(pid, start.Add(time.Second), syscall.SIGSTOP); err == nil {
		t.Fatal("a process started at another time isn't the one picked")
	}
}
