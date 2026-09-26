package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// busyClaude takes a message and works on it for a long time in a child
// process, as a Bash tool call would, so closing its stdin doesn't end it.
// It notes its pid and its child's.
const busyClaude = `#!/bin/sh
d="$(dirname "$0")"
echo $$ > "$d/claude.pid"
echo '{"type":"system","subtype":"init","session_id":"SID","model":"claude-haiku-4-5","permissionMode":"default","tools":["Bash"]}'
while read -r line; do
  case "$line" in
  *'"type":"user"'*)
    sleep 60 &
    echo $! > "$d/tool.pid"
    wait
    ;;
  esac
done
`

func readPID(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
	}
	t.Fatalf("no pid in %s", path)
	return 0
}

func goneWithin(pid int, d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !alive(pid) {
			return true
		}
	}
	return false
}

// A host killed outright, mid-turn, takes its Claude Code and what that
// runs with it: none of it is left running for a host that is gone.
func TestKilledHostEndsItsClaude(t *testing.T) {
	bin := setup(t)
	home := os.Getenv("AGTOP_HOME")
	if err := os.WriteFile(bin, []byte(busyClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Prompt: "work", Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	claudePID := readPID(t, filepath.Join(filepath.Dir(bin), "claude.pid"))
	toolPID := readPID(t, filepath.Join(filepath.Dir(bin), "tool.pid"))
	info, err := ReadInfo(cfg.ID)
	if err != nil || info.HostPID <= 0 {
		t.Fatalf("info: %+v %v", info, err)
	}
	if os.Getenv("AGTOP_HOME") != home || !strings.HasPrefix(home, "/tmp/agtop-host-") {
		t.Fatalf("not the test's own home: %q", home)
	}
	if err := syscall.Kill(info.HostPID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if !goneWithin(claudePID, 3*time.Second) {
		t.Fatalf("claude %d outlived its host", claudePID)
	}
	if !goneWithin(toolPID, 3*time.Second) {
		t.Fatalf("its tool %d outlived its host", toolPID)
	}
}

// A Claude Code left running a session by a host that is gone ends when
// a host starts for that session.
func TestStrayClaudeEndsWhenTheSessionStartsAgain(t *testing.T) {
	bin := setup(t)
	stuck := "#!/bin/sh\necho $$ > \"$(dirname \"$0\")/claude.pid\"\nwhile :; do sleep 0.2; done\n"
	if err := os.WriteFile(bin, []byte(stuck), 0o755); err != nil {
		t.Fatal(err)
	}
	sid, _ := NewSessionID()
	// Started in the background of a shell that exits at once, so init is
	// its parent, as a dead host's Claude has.
	sh := exec.Command("/bin/sh", "-c", `"$0" -p --input-format stream-json --output-format stream-json --resume "$1" </dev/null >/dev/null 2>&1 &`, bin, sid)
	if err := sh.Run(); err != nil {
		t.Fatal(err)
	}
	stray := readPID(t, filepath.Join(filepath.Dir(bin), "claude.pid"))
	other, _ := NewSessionID()
	if got := endStrays(other); len(got) != 0 {
		t.Fatalf("ended %v for another session", got)
	}
	if !alive(stray) {
		t.Fatal("a stray of another session was ended")
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if got := endStrays(sid); len(got) == 1 && got[0] == stray {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stray %d not found", stray)
		}
	}
	if !goneWithin(stray, 2*time.Second) {
		t.Fatalf("stray %d still running", stray)
	}
}

func TestIsHeadlessClaude(t *testing.T) {
	sid := "8c76706f-1c00-4aed-9c6d-7509f3033943"
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"claude", "-p", "--input-format", "stream-json", "--resume", sid}, true},
		{[]string{"claude", "-p", "--input-format", "stream-json", "--session-id", sid}, true},
		{[]string{"claude", "--resume", sid}, false},
		{[]string{"claude", "-p", "--input-format", "stream-json", "--resume", "other"}, false},
		{[]string{"vim", sid}, false},
	} {
		if got := isHeadlessClaude(c.args, sid); got != c.want {
			t.Errorf("%v: %v", c.args, got)
		}
	}
}
