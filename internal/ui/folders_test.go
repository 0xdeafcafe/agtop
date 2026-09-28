package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Grouped by folder, each repository is a section, those with an agent
// running first; a worktree's agents sit under its repository, after the
// main checkout's, headed by the worktree; the header says what's working
// and what git says. Agents older than a day go under Earlier.
func TestFolderSections(t *testing.T) {
	now := time.Now()
	st := &state.Store{}
	st.Config.GroupBy = "folder"
	m := &Model{store: st, snap: &fleet.Snapshot{At: now}}
	add := func(name, repo, root, cwd, state string, age time.Duration) {
		a := &fleet.Agent{Key: name, DisplayName: name, Repo: repo, Root: root}
		a.Cwd, a.State, a.UpdatedAt = cwd, state, now.Add(-age)
		if state == "working" || state == "blocked" {
			a.PID = 1
		}
		m.snap.Agents = append(m.snap.Agents, a)
	}
	const app, wt = "/src/app", "/src/app/.claude/worktrees/fix"
	add("zed", "/src/zed", "/src/zed", "/src/zed", "stopped", time.Hour)
	add("in-tree", wt, app, wt, "working", time.Minute)
	add("on-main", app, app, app+"/web", "blocked", time.Minute)
	add("scratchy", "", "", "/tmp/x", "stopped", time.Hour)
	add("old", app, app, app, "stopped", 48*time.Hour)
	m.rebuild()

	var got []string
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			got = append(got, "§"+l.title)
		case lineTree:
			got = append(got, "⎇"+l.root)
		case lineAgent:
			got = append(got, l.agent.Key)
		}
	}
	want := "§app on-main ⎇" + wt + " in-tree §scratch scratchy §zed zed §Earlier"
	if s := strings.Join(got, " "); s != want {
		t.Fatalf("lines:\n got %s\nwant %s", s, want)
	}

	m.folders.byRoot = map[string]fleet.Folder{app: {
		Root: app, Worktrees: 3,
		Git:   fleet.GitState{Branch: "main", Upstream: true, Ahead: 2, Changed: 4},
		Trees: map[string]fleet.GitState{wt: {Branch: "fix", Changed: 1}},
	}}
	out := ansi.Strip(strings.Join(m.listLines(140, 40), "\n"))
	for _, s := range []string{"app  1 working · 1 need you · 2", "main ↑2 · 4 changed · 3 worktrees", "⎇ fix  fix · 1 changed"} {
		if !strings.Contains(out, s) {
			t.Errorf("list lacks %q:\n%s", s, out)
		}
	}
}
