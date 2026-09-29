package state

import (
	"os"
	"path/filepath"
	"testing"
)

// agtop's folder stays in use until rush's has a config in it.
func TestPickDir(t *testing.T) {
	dir := t.TempDir()
	rush, agtop := filepath.Join(dir, "rush"), filepath.Join(dir, "agtop")
	if got := pick(rush, agtop, "config.json"); got != rush {
		t.Fatalf("neither: got %s", got)
	}
	os.MkdirAll(agtop, 0o755)
	os.WriteFile(filepath.Join(agtop, "config.json"), []byte("{}"), 0o600)
	os.MkdirAll(filepath.Join(rush, "sessions"), 0o755) // made by something else
	if got := pick(rush, agtop, "config.json"); got != agtop {
		t.Fatalf("agtop's: got %s", got)
	}
	os.WriteFile(filepath.Join(rush, "config.json"), []byte("{}"), 0o600)
	if got := pick(rush, agtop, "config.json"); got != rush {
		t.Fatalf("copied: got %s", got)
	}
	if _, err := os.Lstat(agtop); err != nil {
		t.Fatal("agtop's was touched")
	}
}
