package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Up and down go from project to project in the list; enter goes into the
// one picked, on the right, and left comes back.
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
	m.projectsBody()
	step := func(k, want string) {
		t.Helper()
		m.projectsKey(k)
		if m.work.projSel != want {
			t.Fatalf("%s: on %q, want %q", k, m.work.projSel, want)
		}
	}
	step("down", "p/src/beta")
	step("up", "p/src/alpha")
	// enter goes into the project on the right; the list keeps its place.
	m.projectsKey("enter")
	if !m.work.projIn || m.work.inSel != "aa1" {
		t.Fatalf("enter: in %v on %q, want in on aa1", m.work.projIn, m.work.inSel)
	}
	m.projectsKey("down")
	m.projectsKey("down")
	if m.work.inSel != "aa2" {
		t.Fatalf("down in alpha went to %q, want aa2", m.work.inSel)
	}
	step("left", "p/src/alpha")
	if m.work.projIn {
		t.Fatal("left didn't come back to the list")
	}
}

// Each kind has its own place: repositories, then other folders on the
// Projects tab, and temp work and /tmp on Temporary.
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
	page := ansi.Strip(strings.Join(m.projectsBody(), "\n"))
	m.projectsKey("3")
	page += ansi.Strip(strings.Join(m.projectsBody(), "\n"))
	last := -1
	for _, s := range []string{"Projects", "◆ alpha", "OTHER FOLDERS", "◇ Downloads", "Temporary", "◌ /tmp", "◌ Sort downloads"} {
		i := strings.Index(page[last+1:], s)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", s, page)
		}
		last += 1 + i
	}
}
