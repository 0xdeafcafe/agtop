package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// A home links to everything of ~/.claude's but the sign-in and state
// file, makes the folders sessions write in ~/.claude first so they land
// there, and leaves alone what the home has of its own.
func TestLinkHome(t *testing.T) {
	root := Account{ConfigDir: filepath.Join(t.TempDir(), ".claude")}
	home := Account{ConfigDir: filepath.Join(t.TempDir(), "home")}
	for _, f := range []string{"settings.json", ".credentials.json", ".claude.json"} {
		if err := os.MkdirAll(root.ConfigDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root.ConfigDir, f), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(home.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home.ConfigDir, "CLAUDE.md"), []byte("own"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.ConfigDir, "CLAUDE.md"), []byte("root's"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LinkHome(home, root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"settings.json", "projects", "todos"} {
		to, err := os.Readlink(filepath.Join(home.ConfigDir, name))
		if err != nil || to != filepath.Join(root.ConfigDir, name) {
			t.Errorf("%s should link to ~/.claude's: %q, %v", name, to, err)
		}
	}
	for _, name := range []string{".credentials.json", ".claude.json"} {
		if _, err := os.Lstat(filepath.Join(home.ConfigDir, name)); err == nil {
			t.Errorf("%s is the home's own, not a link", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home.ConfigDir, "CLAUDE.md")); string(b) != "own" {
		t.Errorf("the home's own CLAUDE.md was replaced: %q", b)
	}
	// A session in the home writes its transcript into ~/.claude.
	if err := os.WriteFile(filepath.Join(home.ConfigDir, "projects", "t.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root.ConfigDir, "projects", "t.jsonl")); err != nil {
		t.Error("a transcript written in the home isn't in ~/.claude")
	}
	// Linking again, once ~/.claude has something new, adds it.
	if err := os.WriteFile(filepath.Join(root.ConfigDir, "keybindings.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LinkHome(home, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(home.ConfigDir, "keybindings.json")); err != nil {
		t.Error("what ~/.claude gains isn't linked the next time")
	}
}
