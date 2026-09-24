package actions

import (
	"os/exec"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/proc"
)

func TestEndTree(t *testing.T) {
	// A shell with a pipeline under it, one part of which ignores SIGTERM.
	c := exec.Command("/bin/sh", "-c", `sleep 60 | (trap "" TERM; sleep 61)`)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	go c.Wait()
	time.Sleep(300 * time.Millisecond)
	tab := proc.Snapshot(nil)
	root := tab.Procs[c.Process.Pid]
	if root == nil {
		t.Fatal("no root")
	}
	pids := tab.Descendants(c.Process.Pid)
	if len(pids) < 3 {
		t.Fatalf("want a tree, got %v", pids)
	}
	n, err := EndTree(c.Process.Pid, root.Start, 500*time.Millisecond)
	if err != nil || n != len(pids) {
		t.Fatalf("EndTree = %d, %v; want %d", n, err, len(pids))
	}
	time.Sleep(200 * time.Millisecond)
	now := proc.Snapshot(nil)
	for _, pid := range pids {
		if p := now.Procs[pid]; p != nil && p.Start.Equal(tab.Procs[pid].Start) && p.Comm == "sleep" {
			t.Errorf("%d (%s) still running", pid, p.Comm)
		}
	}
}
