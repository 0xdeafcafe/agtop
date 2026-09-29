package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Only what nothing has touched, anywhere inside, for a day goes.
func TestScratch(t *testing.T) {
	scratchDir = t.TempDir()
	old := time.Now().Add(-2 * ScratchIdle)
	mk := func(p string, at time.Time) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		_ = os.Chtimes(p, at, at)
	}
	mk(filepath.Join(scratchDir, "old.log"), old)
	mk(filepath.Join(scratchDir, "cache", "a", "b"), old)
	_ = os.Chtimes(filepath.Join(scratchDir, "cache", "a"), old, old)
	_ = os.Chtimes(filepath.Join(scratchDir, "cache"), old, old)
	mk(filepath.Join(scratchDir, "busy", "old"), old)
	mk(filepath.Join(scratchDir, "busy", "deep", "new"), time.Now())
	_ = os.Chtimes(filepath.Join(scratchDir, "busy"), old, old)
	mk(filepath.Join(scratchDir, "new.log"), time.Now())

	if s := FindScratch(); s.Items != 4 || s.StaleItems != 2 || s.Stale == 0 {
		t.Fatalf("found %+v", s)
	}
	if _, n, err := ClearScratch(); err != nil || n != 2 {
		t.Fatalf("cleared %d: %v", n, err)
	}
	for name, want := range map[string]bool{"old.log": false, "cache": false, "busy": true, "new.log": true} {
		if _, err := os.Stat(filepath.Join(scratchDir, name)); (err == nil) != want {
			t.Errorf("%s there: %v, want %v", name, err == nil, want)
		}
	}
}
