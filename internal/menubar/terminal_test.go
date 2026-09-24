package menubar

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// An agtop open in Warp is the one the menu bar comes back to, on the
// agent asked for; once it's closed, Warp is remembered.
func TestHere(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the menu bar is macOS only")
	}
	t.Setenv("AGTOP_HOME", t.TempDir())
	t.Setenv("__CFBundleIdentifier", "dev.warp.Warp-Stable")
	gone := filepath.Join(openDir(), "999999") // a pid that isn't running
	release := Here()
	_ = os.WriteFile(gone, []byte("com.apple.Terminal"), 0o600)
	if pid, id := open(); pid != os.Getpid() || id != "dev.warp.Warp-Stable" {
		t.Fatalf("open: %d %q", pid, id)
	}
	if _, err := os.Stat(gone); err == nil {
		t.Fatal("an agtop that's gone should be forgotten")
	}
	_ = os.WriteFile(filepath.Join(openDir(), strconv.Itoa(os.Getpid())+".goto"), []byte("acct/abc"), 0o600)
	if k := Goto(); k != "acct/abc" || Goto() != "" {
		t.Fatalf("goto: %q, and only once", k)
	}
	release()
	if pid, _ := open(); pid != 0 {
		t.Fatal("closed, it isn't open")
	}
	if b, _ := os.ReadFile(lastTerminal()); string(b) != "dev.warp.Warp-Stable" || !runsFiles(string(b)) {
		t.Fatalf("last terminal: %q", b)
	}
}
