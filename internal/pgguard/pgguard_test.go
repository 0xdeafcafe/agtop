package pgguard

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func goneWithin(pid int, d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !alive(pid) {
			return true
		}
	}
	return false
}

// A process that dies without a word takes the group it guarded with it,
// children and all. The test re-runs itself as that process.
func TestGroupEndsWithItsParent(t *testing.T) {
	if os.Getenv("PGGUARD_PARENT") == "1" {
		cmd := exec.Command("/bin/sh", "-c", "sleep 60 & echo $!; wait")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		Watch(cmd.Process.Pid)
		b := make([]byte, 32)
		n, _ := out.Read(b)
		os.Stdout.WriteString(strconv.Itoa(cmd.Process.Pid) + " " + strings.TrimSpace(string(b[:n])) + "\n")
		select {}
	}
	parent := exec.Command(os.Args[0], "-test.run=^TestGroupEndsWithItsParent$")
	parent.Env = append(os.Environ(), "PGGUARD_PARENT=1")
	out, _ := parent.StdoutPipe()
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	n, _ := out.Read(b)
	f := strings.Fields(string(b[:n]))
	if len(f) != 2 {
		_ = parent.Process.Kill()
		t.Fatalf("parent said %q", b[:n])
	}
	shell, _ := strconv.Atoi(f[0])
	child, _ := strconv.Atoi(f[1])
	_ = parent.Process.Kill()
	_ = parent.Wait()
	if !goneWithin(shell, 3*time.Second) || !goneWithin(child, 3*time.Second) {
		_ = syscall.Kill(-shell, syscall.SIGKILL)
		t.Fatalf("the group outlived its parent: %d %d", shell, child)
	}
}

// Released, the guard leaves the group alone.
func TestReleaseLeavesTheGroup(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	g := Watch(cmd.Process.Pid)
	if g == nil {
		t.Skip("no shell")
	}
	g.Release()
	time.Sleep(300 * time.Millisecond)
	if !alive(cmd.Process.Pid) {
		t.Fatal("a released guard ended the group")
	}
	var none *Guard
	none.Release()
}
