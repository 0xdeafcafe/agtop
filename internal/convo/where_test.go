package convo

import "testing"

// A command or call into a worktree names it, and a leading cd there
// comes off, since the row names it instead.
func TestWorktreeIn(t *testing.T) {
	cmd := "cd /Users/lw/src/langwatch/.worktrees/go-ports-graph && rm -rf .claude/tmp"
	if got := WorktreeIn(cmd); got != "go-ports-graph" {
		t.Errorf("WorktreeIn = %q", got)
	}
	if got := DropCd(cmd); got != "rm -rf .claude/tmp" {
		t.Errorf("DropCd = %q", got)
	}
	if got := WorktreeIn(`{"file_path":"/r/.claude/worktrees/lane-a/x.go"}`); got != "lane-a" {
		t.Errorf("from input = %q", got)
	}
	for _, keep := range []string{"cd /tmp && make", "cd a/.worktrees/b; make && x", "make"} {
		if got := DropCd(keep); got != keep {
			t.Errorf("DropCd(%q) = %q", keep, got)
		}
	}
}
