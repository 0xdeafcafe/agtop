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
	got := ansi.Strip(m.worktreeRow("/r/.claude/worktrees/devclean", st, fleet.Worktree{Size: 3 << 20}, 1, 120))
	for _, want := range []string{"devclean", "3 commits of its own", "off main, 89 behind", "● 1 running", "3M"} {
		if !strings.Contains(got, want) {
			t.Errorf("row %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "worktree-devclean") {
		t.Errorf("row %q repeats the name as its branch", got)
	}
	got = ansi.Strip(foldLine(16, 0, 2<<30))
	if !strings.Contains(got, "16 more with nothing of their own") || !strings.Contains(got, "cleans them up") {
		t.Errorf("fold %q", got)
	}
}
