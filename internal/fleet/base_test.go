package fleet

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// A worktree's branch knows the branch it was made from, from its reflog,
// and how far it's gone from it.
func TestWorktreeBase(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run(root, "init", "-q", "-b", "main")
	run(root, "commit", "-q", "--allow-empty", "-m", "one")
	run(root, "branch", "dev")
	tree := filepath.Join(root, "wt")
	run(root, "worktree", "add", "-q", "-b", "feat", tree, "dev")
	run(tree, "commit", "-q", "--allow-empty", "-m", "two")
	run(tree, "commit", "-q", "--allow-empty", "-m", "three")
	run(root, "commit", "-q", "--allow-empty", "-m", "on main")

	f := CheckFolder(root, []string{tree}, false)
	if s := f.Trees[tree]; s.Base != "dev" || s.BaseAhead != 2 || s.BaseBehind != 0 {
		t.Errorf("base %q ↑%d ↓%d, want dev ↑2 ↓0", s.Base, s.BaseAhead, s.BaseBehind)
	}
}
