package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// A rewound copy gets the file checkpoints of the conversation it was cut
// from, under its own id; the originals stay where they were.
func TestCopyCheckpoints(t *testing.T) {
	a := Account{ConfigDir: t.TempDir()}
	src := filepath.Join(a.ConfigDir, "file-history", "old")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "abc@v1"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.CopyCheckpoints("old", "new"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(a.ConfigDir, "file-history", "new", "abc@v1")); err != nil || string(b) != "hello\n" {
		t.Fatalf("copied %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(src, "abc@v1")); err != nil {
		t.Fatal("the original's checkpoint is kept")
	}
	// Twice is fine, and a conversation with none has nothing to copy.
	if err := a.CopyCheckpoints("old", "new"); err != nil {
		t.Fatal(err)
	}
	if err := a.CopyCheckpoints("none", "other"); err != nil {
		t.Fatal(err)
	}
}
