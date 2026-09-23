package convo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
	run("init", "-q")
	write("web/a.go", "a\n")
	write("old name.txt", "x\n")
	write("img.bin", "\x00\x01")
	run("add", ".")
	run("commit", "-qm", "init")
	run("mv", "old name.txt", "new name.txt")
	write("img.bin", "\x00\x02\x03")
	write("api/new.go", "n\n")
	tr := readTree(filepath.Join(root, "web"))
	if tr.Err != "" {
		t.Fatal(tr.Err)
	}
	got := map[string]TreeFile{}
	for _, f := range tr.Files {
		rel, _ := filepath.Rel(tr.Root, f.Path)
		got[rel] = f
	}
	if f, ok := got["api/new.go"]; !ok || !f.Untracked {
		t.Errorf("untracked outside the cwd's folder missing: %v", got)
	}
	if _, ok := got["new name.txt"]; !ok {
		t.Errorf("renamed file missing: %v", got)
	}
	if f := got["img.bin"]; !f.Binary {
		t.Errorf("binary not marked: %+v", f)
	}
	if nr := readTree(t.TempDir()); nr.Err == "" {
		t.Error("a folder outside git should say so")
	}
}
