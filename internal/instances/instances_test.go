package instances

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRegisterAndRemove(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	p := filepath.Join(Dir(), strconv.Itoa(os.Getpid()))
	done := Register()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("not listed after Register: %v", err)
	}
	done()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("still listed after it ended: %v", err)
	}
}

func TestReloadSignalsOnlyTheRunningView(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	// A view: a process whose arguments are the ones it listed.
	live := exec.Command("sleep", "30")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = live.Process.Kill(); _ = live.Wait() }()
	list := func(pid int, args ...string) string {
		p := filepath.Join(Dir(), strconv.Itoa(pid))
		if err := os.WriteFile(p, []byte(strings.Join(args, "\x00")), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A reused pid: the process there isn't running what was listed.
	reused := exec.Command("sleep", "31")
	if err := reused.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reused.Process.Kill(); _ = reused.Wait() }()
	stale := list(reused.Process.Pid, "rush")
	list(live.Process.Pid, "sleep", "30")

	// sleep dies of SIGUSR1: that is the signal arriving.
	time.Sleep(100 * time.Millisecond)
	if n := Reload(0); n != 1 {
		t.Fatalf("signalled %d views, want 1", n)
	}
	if err := live.Wait(); err == nil || !strings.Contains(err.Error(), "user defined signal 1") {
		t.Fatalf("live view wasn't sent SIGUSR1: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the stale entry wasn't dropped")
	}
	if reused.ProcessState != nil {
		t.Fatal("the reused pid was signalled")
	}
}
