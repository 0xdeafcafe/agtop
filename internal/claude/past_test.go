package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadConvo(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	prompt := `{"type":"user","cwd":"/src/app","timestamp":"2026-09-01T10:00:00Z","message":{"role":"user","content":"fix the build"}}` + "\n"

	c, ok := ReadConvo(write("a1b2c3d4-first", prompt))
	if !ok || c.Title != "fix the build" || c.Cwd != "/src/app" || c.SessionID != "a1b2c3d4-first" || c.Started.IsZero() {
		t.Fatalf("a first prompt should title it: %+v %v", c, ok)
	}
	c, _ = ReadConvo(write("ai", prompt+`{"type":"ai-title","aiTitle":"Build fix"}`+"\n"))
	if c.Title != "Build fix" {
		t.Fatalf("Claude Code's title should beat the prompt: %q", c.Title)
	}
	c, _ = ReadConvo(write("custom", prompt+`{"type":"custom-title","customTitle":"mine"}`+"\n"+`{"type":"ai-title","aiTitle":"Build fix"}`+"\n"))
	if c.Title != "mine" {
		t.Fatalf("a /rename should beat Claude Code's title: %q", c.Title)
	}
	if _, ok := ReadConvo(write("hooks", `{"type":"queue-operation","timestamp":"2026-09-01T10:00:00Z"}`+"\n")); ok {
		t.Fatal("a transcript with nothing asked in it is no conversation")
	}
}
