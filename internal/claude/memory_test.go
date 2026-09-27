package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	cfg, cwd := filepath.Join(root, "cfg"), filepath.Join(root, "src", "app")
	proj := filepath.Join(cfg, "projects", ProjectSlug(cwd))
	mem := filepath.Join(proj, "memory")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	note := func(name, desc, body string) {
		fm := "---\nname: " + name + "\n"
		if desc != "" {
			fm += "description: " + desc + "\n"
		}
		write(filepath.Join(mem, name+".md"), fm+"---\n\n"+body+"\n")
	}

	// An index past 200 lines, one line too long, one link to nothing.
	var index []string
	index = append(index, "- [Work on main](work-on-main.md) — no branches")
	index = append(index, "- [Style](style.md) — "+strings.Repeat("long ", 40))
	index = append(index, "- [Gone](gone.md) — was deleted")
	for i := len(index); i < 205; i++ {
		index = append(index, fmt.Sprintf("- filler %d", i))
	}
	index = append(index, "- [Late](late.md) — past the cut")
	write(filepath.Join(mem, "MEMORY.md"), strings.Join(index, "\n")+"\n")
	note("work-on-main", "no worktrees or branches, work on main directly", "See `internal/ui/memory.go` and `internal/ui/gone.go`, and `other/x.go`.")
	note("style", "commit style for this repository with conventional commits", "")
	note("late", "a late one", "")
	note("stray", "", "never indexed")
	note("style-2", "conventional commits style for this repository commit", "")
	write(filepath.Join(cwd, "internal", "ui", "memory.go"), "package ui\n")

	// The user's CLAUDE.md imports one file that's there and one that isn't;
	// a package name and code aren't imports. A rule with paths: loads later.
	write(filepath.Join(cfg, "CLAUDE.md"), "@RTK.md\nUse @scope/pkg, not `@code.md`.\n```\n@fenced.md\n```\nSee @./nope.md.\n")
	write(filepath.Join(cfg, "RTK.md"), "@deeper.md\n")
	write(filepath.Join(cfg, "deeper.md"), strings.Repeat("x", 45_000))
	write(filepath.Join(cwd, "CLAUDE.md"), "# app\n")
	write(filepath.Join(cwd, ".claude", "rules", "ts.md"), "---\ndescription: |\n  TypeScript,\n  strictly\npaths:\n  - \"**/*.ts\"\n---\nstrict\n")

	r := CheckMemory(cfg, cwd, proj)

	ix := r.Index
	if ix.Lines != 206 || ix.LoadedLines != IndexLines || !ix.Cut() || ix.Long != 1 {
		t.Fatalf("index: %d lines, %d loaded, %d long", ix.Lines, ix.LoadedLines, ix.Long)
	}
	var got []string
	for _, in := range r.Instr {
		rel, _ := filepath.Rel(root, in.Path)
		got = append(got, fmt.Sprintf("%s %s missing=%v later=%v", in.Scope, rel, in.Missing, in.OnDemand))
	}
	want := []string{
		"user cfg/CLAUDE.md missing=false later=false",
		"import cfg/RTK.md missing=false later=false",
		"import cfg/deeper.md missing=false later=false",
		"import cfg/nope.md missing=true later=false",
		"project src/app/CLAUDE.md missing=false later=false",
		"rule src/app/.claude/rules/ts.md missing=false later=true",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("instructions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if d := FrontMatter(r.Instr[5].Path)["description"]; d != "TypeScript, strictly" {
		t.Errorf("block description: %q", d)
	}
	notes := map[string]MemNote{}
	for _, n := range r.Notes {
		notes[n.Name] = n
	}
	if n := notes["work-on-main"]; !n.Indexed || len(n.Gone) != 1 || n.Gone[0] != "internal/ui/gone.go" {
		t.Errorf("work-on-main: %+v", n)
	}
	if n := notes["late"]; n.Indexed || !n.PastCut {
		t.Errorf("late: %+v", n)
	}
	if a, b := notes["style-2"], notes["style"]; !strings.HasSuffix(a.Same+b.Same, "style.md") && !strings.HasSuffix(a.Same+b.Same, "style-2.md") {
		t.Errorf("style and style-2 should repeat each other: %+v %+v", a, b)
	}
	if n := notes["stray"]; n.Indexed || n.PastCut {
		t.Errorf("stray: %+v", n)
	}

	var kinds []string
	for _, p := range r.Problems {
		kinds = append(kinds, p.Kind)
		if p.Title == "" || p.Fix == "" || p.Path == "" {
			t.Errorf("%s: missing words or a file: %+v", p.Kind, p)
		}
	}
	if got, want := strings.Join(kinds, " "), "index-cut broken-link broken-import big-instructions orphan duplicate stale long-lines no-description"; got != want {
		t.Fatalf("problems:\n%s\nwant:\n%s", got, want)
	}
	// Up front: the instructions without the later rule, and the loaded
	// part of the index.
	var instr int64
	for _, in := range r.Instr {
		if !in.OnDemand {
			instr += in.Size
		}
	}
	if r.Upfront != instr+int64(ix.LoadedBytes) || ix.LoadedBytes >= ix.Bytes {
		t.Errorf("up front %d, want %d + %d", r.Upfront, instr, ix.LoadedBytes)
	}
}

func TestMemIndexBytes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "MEMORY.md")
	line := strings.Repeat("y", 999)
	var lines []string
	for range 30 {
		lines = append(lines, line)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := ReadMemIndex(p)
	if ix.LoadedLines != 25 || ix.LoadedBytes > IndexBytes || !ix.Cut() {
		t.Fatalf("%d lines, %d bytes loaded", ix.LoadedLines, ix.LoadedBytes)
	}
}
