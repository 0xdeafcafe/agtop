package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// wallModel is a fleet of n agents in every state, each with something
// said and run, on a w×h screen showing the Wall.
func wallModel(n, w, h int) *Model {
	now := time.Now()
	m := &Model{store: &state.Store{}, previews: map[string]previewEntry{}, w: w, h: h, lastState: map[string]string{}}
	states := []string{"working", "blocked", "working", "done", "working", "idle"}
	var agents []*fleet.Agent
	for i := 0; i < n; i++ {
		a := &fleet.Agent{Key: fmt.Sprintf("k%d", i), DisplayName: fmt.Sprintf("agent number %d fixing the auth flow", i), PID: 100 + i, Repo: "/src/langwatch", Branch: "feat/wall"}
		a.ID, a.State, a.CreatedAt, a.UpdatedAt = fmt.Sprintf("id%d", i), states[i%len(states)], now.Add(-time.Duration(i)*time.Minute), now.Add(-time.Minute)
		a.Todos, a.TodosDone, a.Spend.Cost = 5, i%6, 1.5*float64(i)
		if a.State == "blocked" {
			a.Needs = "Which database should the migration target?"
		}
		agents = append(agents, a)
		m.previews[a.Key] = previewEntry{p: claude.Preview{
			Model: "claude-opus-4-5", Context: int64(90_000 * (i + 1)), At: now.Add(-time.Second), Doing: "running pnpm test",
			Recent: []agent.Line{
				{Role: "user", Text: "make the login page stop flickering when the token refreshes"},
				{Role: "assistant", Text: "Looking at how the **token** refresh is wired into the session provider first."},
				{Role: "tool", Text: "Read\x00src/auth/session.tsx"},
				{Role: "tool", Text: "Grep\x00refreshToken"},
				{Role: "assistant", Text: "The provider re-mounts on every refresh because the key changes. I'll memoise the context value and keep the key stable, then run the tests."},
				{Role: "tool", Text: "Edit\x00src/auth/session.tsx"},
				{Role: "tool", Text: "Bash\x00pnpm test --filter auth"},
			},
		}}
	}
	m.snap = &fleet.Snapshot{At: now, Agents: agents}
	m.setView(placeAgents)
	m.setAgentsPage(agentsWall)
	return m
}

func TestWallFillsTheScreen(t *testing.T) {
	for _, tc := range []struct{ n, w, h int }{{1, 120, 40}, {3, 200, 50}, {7, 160, 45}, {14, 180, 40}, {30, 100, 30}, {2, 50, 20}} {
		m := wallModel(tc.n, tc.w, tc.h)
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != tc.h {
			t.Errorf("%v: %d lines, want %d", tc, len(lines), tc.h)
		}
		for i, l := range lines {
			if cw := cellw.String(l); cw > tc.w {
				t.Errorf("%v: line %d is %d wide: %q", tc, i, cw, ansi.Strip(l))
			}
		}
		if len(m.wall.tiles) == 0 {
			t.Errorf("%v: no tiles drawn", tc)
		}
		if os.Getenv("WALL_SHOW") != "" {
			fmt.Println(m.View().Content)
		}
	}
}

func TestWallOrderAndKeys(t *testing.T) {
	m := wallModel(6, 160, 45)
	agents := m.wallAgents()
	if agents[0].State != "blocked" {
		t.Fatalf("first tile %s, want the one that needs you", agents[0].State)
	}
	cols, _, _ := wallGrid(len(agents), m.w-4, m.wallH())
	m.wallKey("down")
	if m.wall.sel != agents[min(cols, len(agents)-1)].Key {
		t.Fatalf("down went to %s", m.wall.sel)
	}
	m.wallKey("left")
	if m.wall.sel != agents[cols-1].Key {
		t.Fatalf("left went to %s", m.wall.sel)
	}
	m.wallKey("esc")
	if m.view != placeAgents {
		t.Fatal("esc didn't go back to Agents")
	}
}

func TestWallGrid(t *testing.T) {
	for _, tc := range []struct{ n, w, h, cols int }{{1, 200, 40, 1}, {2, 200, 40, 2}, {4, 200, 40, 2}, {9, 200, 40, 3}} {
		c, r, th := wallGrid(tc.n, tc.w, tc.h)
		if c != tc.cols || th < wallMinH || c*r < tc.n {
			t.Errorf("%v: %d cols × %d rows, %d tall", tc, c, r, th)
		}
	}
	// Too many to fit pages them rather than squashing.
	if _, r, th := wallGrid(40, 120, 30); th < wallMinH || r*wallMinH > 30 {
		t.Errorf("40 tiles: %d rows %d tall", r, th)
	}
}
