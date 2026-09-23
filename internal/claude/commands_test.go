package claude

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSettingsSave(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "settings.json")
	_ = os.MkdirAll(filepath.Dir(real), 0o755)
	orig := "{\n  \"zeta\": 1,\n  \"hooks\": {\"Stop\": \"a && b > x\"},\n  \"alpha\": {\"b\": 1, \"a\": 2}\n}\n"
	if err := os.WriteFile(real, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "cfg")
	_ = os.MkdirAll(cfg, 0o755)
	link := filepath.Join(cfg, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(Account{ConfigDir: cfg})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetEnv("FOO", "<1>")
	// Claude Code changes the file after agtop read it.
	b, _ := os.ReadFile(real)
	_ = os.WriteFile(real, []byte(strings.Replace(string(b), `"zeta": 1`, `"zeta": 1, "model": "opus"`, 1)), 0o644)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a file")
	}
	got, _ := os.ReadFile(real)
	want := "{\n  \"zeta\": 1,\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"Stop\": \"a && b > x\"\n  },\n  \"alpha\": {\n    \"b\": 1,\n    \"a\": 2\n  },\n  \"env\": {\n    \"FOO\": \"<1>\"\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("saved:\n%s\nwant:\n%s", got, want)
	}
}
