package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Up and down go from project head to project head, and stay inside a
// project once stepped into, so the one open never folds away above the
// cursor.
func TestProjectsMoveKeepsLevel(t *testing.T) {
	now := time.Now()
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{At: now}, w: 120, h: 40}
	for _, a := range []*fleet.Agent{
		{Key: "a1", PID: 1, Root: "/src/alpha", Repo: "/src/alpha"},
		{Key: "a2", PID: 2, Root: "/src/alpha", Repo: "/src/alpha"},
		{Key: "b1", PID: 3, Root: "/src/beta", Repo: "/src/beta"},
	} {
		a.UpdatedAt = now
		m.snap.Agents = append(m.snap.Agents, a)
	}
	m.work.projSel = "p/src/alpha"
	step := func(k, want string) {
		t.Helper()
		m.projectsKey(k)
		if m.work.projSel != want {
			t.Fatalf("%s: on %q, want %q", k, m.work.projSel, want)
		}
	}
	step("down", "p/src/beta")
	step("up", "p/src/alpha")
	step("enter", "aa1")
	step("down", "aa2")
	step("down", "aa2") // the last in alpha: stays, doesn't fold alpha away
	step("up", "aa1")
	step("up", "aa1")
	step("left", "p/src/alpha")
}

// Each kind has its own section: repositories under Projects, other
// folders after them, and temp work and /tmp under Temporary.
func TestProjectsSections(t *testing.T) {
	now := time.Now()
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{At: now}, w: 140, h: 40}
	for _, a := range []*fleet.Agent{
		{Key: "a1", PID: 1, Root: "/src/alpha", Repo: "/src/alpha"},
		{Key: "b1", Cwd: "/Users/me/Downloads", DisplayName: "Sort downloads", Temp: 2 << 20},
	} {
		a.UpdatedAt = now
		m.snap.Agents = append(m.snap.Agents, a)
	}
	m.clean.tmp = fleet.Scratch{Items: 3, Size: 4 << 20, StaleItems: 1, Stale: 1 << 20, Checked: now}
	rows := m.projectRows()
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = ansi.Strip(r.line)
	}
	page := strings.Join(lines, "\n")
	last := -1
	for _, s := range []string{"Projects", "◆ alpha", "Other folders", "◇ Downloads", "Temporary", "◌ /tmp", "◌ Sort downloads"} {
		i := strings.Index(page, s)
		if i <= last {
			t.Fatalf("%q missing or out of order:\n%s", s, page)
		}
		last = i
	}
}
