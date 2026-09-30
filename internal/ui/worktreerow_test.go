package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

func TestWorktreeRowSaysWhatItHas(t *testing.T) {
	m := &Model{}
	st := fleet.GitState{Branch: "worktree-devclean", Base: "main", BaseAhead: 3, BaseBehind: 89}
	got := ansi.Strip(m.worktreeLine("/r/.claude/worktrees/devclean", st, fleet.Worktree{Size: 3 << 20}, 1, 120))
	for _, want := range []string{"devclean", "3 commits", "main · 89 behind", "● 1 running", "3M"} {
		if !strings.Contains(got, want) {
			t.Errorf("row %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "worktree-devclean") {
		t.Errorf("row %q repeats the name as its branch", got)
	}
}

// A worktree folder named for its branch says the name once in the list.
func TestTreeLineNamesOnce(t *testing.T) {
	m := &Model{}
	root := "/r/.worktrees/feat-slack-migration"
	m.folders.byRoot = map[string]fleet.Folder{"/r": {Trees: map[string]fleet.GitState{root: {Branch: "feat/slack-migration", Changed: 1, Base: "origin/main"}}}}
	got := ansi.Strip(m.treeLine(listLine{title: "/r", root: root}, 120))
	if strings.Contains(got, "feat/slack-migration") || !strings.Contains(got, "feat-slack-migration  ±1 from origin/main") {
		t.Fatalf("row %q", got)
	}
	m.folders.byRoot["/r"].Trees[root] = fleet.GitState{Branch: "other", Changed: 1}
	if got := ansi.Strip(m.treeLine(listLine{title: "/r", root: root}, 120)); !strings.Contains(got, "feat-slack-migration  other ±1") {
		t.Fatalf("another branch is still named: %q", got)
	}
}
