package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A worktree goes only when every change in it is committed and pushed.
func TestWorktreeSafety(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	remote, repo := filepath.Join(root, "remote.git"), filepath.Join(root, "repo")
	_ = os.MkdirAll(repo, 0o755)
	run(t, root, "init", "-q", "--bare", remote)
	run(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-qm", "first")
	run(t, repo, "remote", "add", "origin", remote)
	run(t, repo, "push", "-q", "origin", "main")
	wt := filepath.Join(repo, ".claude", "worktrees", "feature")
	run(t, repo, "worktree", "add", "-q", "-b", "feature", wt)

	agent := &Agent{Key: "k"}
	agent.Spend.Dirs = []string{filepath.Join(wt, "sub")}
	agent.Cwd = repo
	find := func() Worktree {
		t.Helper()
		ws := FindWorktrees([]*Agent{agent})
		if len(ws) != 1 || ws[0].Path != wt || len(ws[0].Agents) != 1 || !ws[0].Claude || ws[0].Branch != "feature" {
			t.Fatalf("found %+v", ws)
		}
		ws[0].Check()
		return ws[0]
	}

	// A new file: uncommitted.
	_ = os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	if w := find(); w.Safe() || w.Changed != 1 {
		t.Fatalf("uncommitted: %+v", w)
	}
	// Committed, not pushed.
	run(t, wt, "add", ".")
	run(t, wt, "commit", "-qm", "b")
	w := find()
	if w.Safe() || w.Unpushed != 1 || w.Losses() != "1 commit not pushed" {
		t.Fatalf("unpushed: %+v %q", w, w.Losses())
	}
	if err := RemoveWorktree(w, false); err == nil {
		t.Fatal("removed a worktree with an unpushed commit")
	}
	// Pushed, and an ignored build folder: safe, and it goes.
	run(t, wt, "push", "-q", "origin", "feature")
	_ = os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("dist/\n.gitignore\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(wt, "dist"), 0o755)
	_ = os.WriteFile(filepath.Join(wt, "dist", "x.js"), []byte("x"), 0o644)
	if w = find(); !w.Safe() || w.Size == 0 {
		t.Fatalf("pushed: %+v", w)
	}
	if err := RemoveWorktree(w, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("the worktree is still there")
	}
	if ws := FindWorktrees([]*Agent{agent}); len(ws) != 0 {
		t.Fatalf("git still lists it: %+v", ws)
	}
	run(t, repo, "rev-parse", "--verify", "-q", "feature") // the branch stays
}

// Forcing removes one that would lose work, after the confirmation.
func TestWorktreeForce(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "repo")
	_ = os.MkdirAll(repo, 0o755)
	run(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-qm", "first")
	wt := filepath.Join(root, "wt")
	run(t, repo, "worktree", "add", "-q", "-b", "x", wt)
	_ = os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("d\n"), 0o644)
	ws := FindWorktrees([]*Agent{{Key: "k", Job: jobIn(wt)}})
	if len(ws) != 1 {
		t.Fatalf("found %+v", ws)
	}
	ws[0].Check()
	if ws[0].Safe() || !ws[0].NoRemote {
		t.Fatalf("no remote should never be safe: %+v", ws[0])
	}
	if err := RemoveWorktree(ws[0], true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
}

func jobIn(dir string) claude.Job { return claude.Job{Cwd: dir} }
