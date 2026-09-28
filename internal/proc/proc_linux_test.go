//go:build linux

package proc

import (
	"os"
	"testing"
	"time"
)

func TestParseStatKeepsACommandWithParentheses(t *testing.T) {
	line := []byte("4242 (tmux: server (x)) S 1 4242 4242 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 1 0 12345 1000 200 18446744073709551615")
	s, ok := parseStat(line)
	if !ok {
		t.Fatal("not parsed")
	}
	if s.comm != "tmux: server (x)" || s.ppid != 1 || s.startTicks != 12345 {
		t.Fatalf("got %+v", s)
	}
	if s.cpu != 3*time.Second {
		t.Fatalf("cpu %v, want 3s", s.cpu)
	}
}

func TestSnapshotHasThisProcess(t *testing.T) {
	tab := Snapshot(nil)
	p := tab.Procs[os.Getpid()]
	if p == nil {
		t.Fatal("this process is not in the table")
	}
	if p.PPID != os.Getppid() {
		t.Fatalf("ppid %d, want %d", p.PPID, os.Getppid())
	}
	tab.Fill(nil, []int{p.PID})
	if p.Footprint == 0 {
		t.Fatal("no memory read")
	}
	if len(Args(p.PID)) == 0 {
		t.Fatal("no argv read")
	}
}
