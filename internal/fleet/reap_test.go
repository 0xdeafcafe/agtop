package fleet

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/proc"
)

// What an agent's Claude leaves running is ended once that Claude exits,
// and not before.
func TestReaper(t *testing.T) {
	// root stands in for an agtop host; its child for the Claude it runs,
	// which starts a background job and a foreground one.
	root := exec.Command("/bin/sh", "-c", `(sh -c "sleep 301 & sleep 302") & wait`)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	defer root.Process.Kill()
	go root.Wait()
	time.Sleep(300 * time.Millisecond)
	agent := &Agent{PID: root.Process.Pid}
	agent.DisplayName = "worker"
	var r Reaper
	tab := proc.Snapshot(nil)
	if left := r.Watch(tab, []*Agent{agent}); len(left) != 0 {
		t.Fatalf("nothing is left while its Claude runs: %+v", left)
	}
	var owner int
	var sleeps []int
	for _, n := range tab.Tree(root.Process.Pid) {
		switch {
		case n.Depth == 1:
			owner = n.PID
		case n.Depth == 2:
			sleeps = append(sleeps, n.PID)
		}
	}
	if owner == 0 || len(sleeps) != 2 {
		t.Fatalf("tree: %+v", tab.Tree(root.Process.Pid))
	}
	_ = syscall.Kill(owner, syscall.SIGKILL) // its Claude exits
	time.Sleep(300 * time.Millisecond)
	left := r.Watch(proc.Snapshot(nil), []*Agent{agent})
	if len(left) != 2 {
		t.Fatalf("want both sleeps left over, got %+v", left)
	}
	for _, l := range left {
		if l.Agent != "worker" {
			t.Errorf("agent %q", l.Agent)
		}
		l.End(200 * time.Millisecond)
	}
	now := proc.Snapshot(nil)
	for _, pid := range sleeps {
		if p := now.Procs[pid]; p != nil && p.Comm == "sleep" {
			t.Errorf("%d still running", pid)
		}
	}
	if left := r.Watch(now, []*Agent{agent}); len(left) != 0 {
		t.Errorf("reported twice: %+v", left)
	}
}

// Something detached while its Claude still runs is left alone.
func TestReaperLeavesDetachedWhileRunning(t *testing.T) {
	root := exec.Command("/bin/sh", "-c", `(sh -c "(sleep 303 &) ; sleep 304") & wait`)
	if err := root.Start(); err != nil {
		t.Fatal(err)
	}
	defer root.Process.Kill()
	go root.Wait()
	time.Sleep(300 * time.Millisecond)
	agent := &Agent{PID: root.Process.Pid}
	var r Reaper
	r.Watch(proc.Snapshot(nil), []*Agent{agent})
	time.Sleep(200 * time.Millisecond)
	tab := proc.Snapshot(nil)
	if left := r.Watch(tab, []*Agent{agent}); len(left) != 0 {
		t.Fatalf("its Claude still runs: %+v", left)
	}
	for pid, p := range tab.Procs {
		if p.Comm == "sleep" && p.PPID == 1 {
			if a := proc.CommandLine(pid); a == "sleep 303" {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
}
