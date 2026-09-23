package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommands(t *testing.T) {
	cfg, proj := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cfg, "skills", "pdf", "SKILL.md"), "---\nname: pdf\ndescription: \"read pdfs\"\n---\nbody")
	write(filepath.Join(proj, ".claude", "commands", "git", "push.md"), "# Push the branch\n")
	plug := filepath.Join(cfg, "plugins", "synced", "x", "design")
	write(filepath.Join(plug, "skills", "critique", "SKILL.md"), "---\nname: design-critique\ndescription: feedback\nargument-hint: \"<url>\"\n---\n")
	got := map[string]Command{}
	for _, c := range Commands(cfg, proj) {
		got[c.Name] = c
	}
	if c := got["pdf"]; !c.Skill || c.Description != "read pdfs" {
		t.Errorf("pdf = %+v", c)
	}
	if c := got["git:push"]; c.Description != "Push the branch" {
		t.Errorf("git:push = %+v", c)
	}
	if c := got["design:design-critique"]; c.ArgumentHint != "<url>" {
		t.Errorf("plugin skill = %+v (all: %v)", c, got)
	}
}
