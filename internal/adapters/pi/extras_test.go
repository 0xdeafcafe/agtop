package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// Pi's prompt templates and skills are its commands, and its AGENTS.md
// files (a CLAUDE.md where there's none) its memory.
func TestCommandsAndMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, cwd := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "prompts", "review.md"), "---\ndescription: Review it\n---\nGo.")
	write(filepath.Join(cwd, ".pi", "skills", "ship", "SKILL.md"), "---\nname: ship\ndescription: Ship it\n---\n")
	write(filepath.Join(cwd, "CLAUDE.md"), "be kind")
	p := agent.Profile{Kind: Kind, Dir: dir}
	cmds := Adapter{}.Commands(p, cwd)
	if len(cmds) != 2 || cmds[0].Name != "review" || cmds[0].Description != "Review it" || cmds[1].Name != "skill:ship" || !cmds[1].Skill || cmds[1].Source != "project" {
		t.Fatalf("commands: %+v", cmds)
	}
	var got []string
	for _, f := range (Adapter{}).Memory(p, cwd, "").Files {
		if f.Missing {
			got = append(got, "-"+filepath.Base(f.Path))
		} else {
			got = append(got, filepath.Base(f.Path))
		}
	}
	// The project's AGENTS.md shows as missing, to be written; its
	// CLAUDE.md is read, as Pi does.
	if len(got) < 2 || got[0] != "-AGENTS.md" || got[len(got)-1] != "CLAUDE.md" {
		t.Fatalf("memory files: %v", got)
	}
}
