package proc

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestZombie(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no process backend")
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	for deadline := time.Now().Add(5 * time.Second); !Zombie(cmd.Process.Pid); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("an exited, unreaped child never read as a zombie")
		}
	}
	if Zombie(os.Getpid()) {
		t.Fatal("this running process read as a zombie")
	}
}
