package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A folder is its repository's main checkout: git says its branch, how far
// it is from its upstream and what's uncommitted, and the same of each
// worktree an agent is in.
func TestCheckFolder(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	remote, repo := filepath.Join(root, "remote.git"), filepath.Join(root, "repo")
	_ = os.MkdirAll(repo, 0o755)
	run(t, root, "init", "-q", "--bare", remote)
	run(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-qm", "first")
	run(t, repo, "remote", "add", "origin", remote)
	run(t, repo, "push", "-q", "-u", "origin", "main")
	_ = os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-qm", "second")
	_ = os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte(".claude/\n"), 0o644)
	wt := filepath.Join(repo, ".claude", "worktrees", "feature")
	run(t, repo, "worktree", "add", "-q", "-b", "feature", wt)
	other := filepath.Join(root, "other")
	run(t, repo, "worktree", "add", "-q", "-b", "other", other)
	_ = os.WriteFile(filepath.Join(wt, "d.txt"), []byte("d\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wt, "e.txt"), []byte("e\n"), 0o644)

	if got := mainCheckout(filepath.Join(wt, "sub"), map[string]string{}); got != repo {
		t.Fatalf("worktree's main checkout: %q, want %q", got, repo)
	}
	agents := []*Agent{{Repo: repo, Root: repo}, {Repo: wt, Root: repo}, {Repo: wt, Root: repo}, {}}
	wants := FolderWants(agents)
	if len(wants) != 1 || len(wants[repo]) != 1 || wants[repo][0] != wt {
		t.Fatalf("wants %v", wants)
	}

	f := CheckFolder(repo, wants[repo], false)
	if g := f.Git; g.Branch != "main" || !g.Upstream || g.Ahead != 1 || g.Behind != 0 || g.Changed != 1 || g.Err != "" {
		t.Fatalf("main checkout: %+v", g)
	}
	if f.Worktrees != 2 {
		t.Fatalf("worktrees: %d, want 2", f.Worktrees)
	}
	if g := f.Trees[wt]; g.Branch != "feature" || g.Upstream || g.Changed != 2 {
		t.Fatalf("worktree: %+v", g)
	}
	if _, ok := f.Trees[other]; ok {
		t.Fatal("checked a worktree no agent is in")
	}

	// Whole, every worktree, the remote and the last commits.
	f = CheckFolder(repo, nil, true)
	if len(f.Linked) != 2 || len(f.Trees) != 2 || f.Trees[other].Branch != "other" {
		t.Fatalf("whole: linked %v trees %+v", f.Linked, f.Trees)
	}
	if len(f.Recent) != 2 || f.Recent[0].Subject != "second" || f.Recent[0].At.IsZero() {
		t.Fatalf("recent: %+v", f.Recent)
	}
	if f.Remote != strings.TrimSuffix(remote, ".git") {
		t.Fatalf("remote: %q", f.Remote)
	}
}

func TestShortRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:0xdeafcafe/rush.git":          "github.com/0xdeafcafe/rush",
		"https://github.com/langwatch/langwatch.git":   "github.com/langwatch/langwatch",
		"ssh://git@gitlab.example.com:22/team/app.git": "gitlab.example.com/team/app",
	} {
		if got := shortRemote(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
