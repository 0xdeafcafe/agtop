package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Split by project, each status section keeps its place and its rows sit
// together by project, a worktree's agents under the repository it came
// from, after the main checkout's, headed by the worktree; each project's
// line says what git says. Off, the sections are as they were.
func TestProjectSplit(t *testing.T) {
	now := time.Now()
	st := &state.Store{}
	st.Config.GroupBy = "status"
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
	add("on-main", app, app, app+"/web", "working", time.Minute)
	add("scratchy", "", "", "/tmp/x", "stopped", time.Hour)
	add("old", app, app, app, "stopped", 48*time.Hour)
	add("done-here", app, app, app, "stopped", 2*time.Hour)
	lines := func() string {
		m.rebuild()
		var got []string
		for _, l := range m.lines {
			switch l.kind {
			case lineSection:
				got = append(got, "§"+l.title)
			case lineProject:
				got = append(got, "▪"+l.title)
			case lineTree:
				got = append(got, "⎇"+l.root)
			case lineAgent:
				got = append(got, l.agent.Key)
			}
		}
		return strings.Join(got, " ")
	}
	want := "§Working ▪app on-main ⎇" + wt + " in-tree §Today ▪app done-here ▪scratch scratchy ▪zed zed §Earlier"
	if s := lines(); s != want {
		t.Fatalf("split:\n got %s\nwant %s", s, want)
	}

	m.folders.byRoot = map[string]fleet.Folder{app: {
		Root: app, Worktrees: 3,
		Git:   fleet.GitState{Branch: "main", Upstream: true, Ahead: 2, Changed: 4},
		Trees: map[string]fleet.GitState{wt: {Branch: "fix", Changed: 1}},
	}}
	out := ansi.Strip(strings.Join(m.listLines(140, 40), "\n"))
	for _, s := range []string{"app  main ↑2 ±4  ⎇3 ┄", "⎇ fix  fix ±1"} {
		if !strings.Contains(out, s) {
			t.Errorf("list lacks %q:\n%s", s, out)
		}
	}
	if strings.Count(out, "main ↑2") != 2 {
		t.Errorf("git should head the project in both its sections:\n%s", out)
	}

	st.Config.SplitBy = "none"
	if s, want := lines(), "§Working in-tree on-main §Today done-here scratchy zed §Earlier"; s != want {
		t.Fatalf("unsplit:\n got %s\nwant %s", s, want)
	}
}

// Two folders of one name are told apart by the folders they're in.
func TestFolderTitles(t *testing.T) {
	got := folderTitles(map[string]bool{"/src/lw/langwatch": true, "/tmp/w/langwatch": true, "/src/agtop": true, scratchSection: true})
	want := map[string]string{"/src/lw/langwatch": "lw/langwatch", "/tmp/w/langwatch": "w/langwatch", "/src/agtop": "agtop", scratchSection: scratchSection}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}
