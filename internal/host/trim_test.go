package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrimTmpKeepsWhatsInUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tmp")
	old := time.Now().Add(-4 * time.Hour)
	for _, n := range []string{"old-build", "held-build", "claude-502", "fresh-build"} {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Join(p, "deep"), 0o700); err != nil {
			t.Fatal(err)
		}
		if n != "fresh-build" {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := trimTmp(dir, time.Now(), map[string]bool{"held-build": true}); got != 1 {
		t.Errorf("removed %d, want 1", got)
	}
	for n, want := range map[string]bool{"old-build": false, "held-build": true, "claude-502": true, "fresh-build": true} {
		if _, err := os.Stat(filepath.Join(dir, n)); (err == nil) != want {
			t.Errorf("%s there = %v, want %v", n, err == nil, want)
		}
	}
	if trimTmp(filepath.Dir(dir), time.Now(), nil) != 0 {
		t.Error("trimmed a folder that isn't a session's tmp")
	}
}

func TestHeldUnderSeesOpenFiles(t *testing.T) {
	dir := t.TempDir() // on macOS, under /var, a link to /private/var
	p := filepath.Join(dir, "busy", "deep", "f")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	held, ok := heldUnder(dir)
	if !ok {
		t.Skip("lsof isn't here")
	}
	if !held["busy"] {
		t.Errorf("an open file's folder wasn't held: %v", held)
	}
}
