package claude

import (
	"runtime"
	"testing"
)

// Claude Code files a session begun in /tmp under its real path.
func TestProjectSlugRealDir(t *testing.T) {
	want := "-Users-me-x"
	if runtime.GOOS == "darwin" {
		if got := ProjectSlug("/tmp/spawnprobe"); got != "-private-tmp-spawnprobe" {
			t.Errorf("got %q", got)
		}
	}
	if got := ProjectSlug("/Users/me/x"); got != want {
		t.Errorf("got %q", got)
	}
}
