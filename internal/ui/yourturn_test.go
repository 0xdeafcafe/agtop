package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

// An agent that stopped on an error needs you; one that finished without
// asking is your turn, sharing Active with the idle.
func TestFinishedTurnsWaitForYou(t *testing.T) {
	now := time.Now()
	agent := func(key, st string) *fleet.Agent {
		a := &fleet.Agent{Key: key, DisplayName: key, PID: 7}
		a.State, a.UpdatedAt = st, now.Add(-5*time.Minute)
		return a
	}
	quiet, broke, idle := agent("quiet", "done"), agent("broke", "done"), agent("idle", "done")
	broke.Spend.Halt = &claude.Halt{Kind: "server_error", Text: "API Error: Response stalled mid-stream."}
	idle.Seen = true
	m := &Model{store: &state.Store{}, previews: map[string]previewEntry{}, w: 120, h: 40, lastState: map[string]string{}}
	m.snap = &fleet.Snapshot{At: now, Agents: []*fleet.Agent{quiet, broke, idle}}
	m.rebuild()
	want := map[string]string{"quiet": activeSection, "broke": "Needs you", "idle": activeSection}
	for k, g := range want {
		if m.groupOf[k] != g {
			t.Errorf("%s in %q, want %q", k, m.groupOf[k], g)
		}
	}
	var titles []string
	for _, l := range m.lines {
		if l.kind == lineSection {
			titles = append(titles, l.title)
		}
	}
	if len(titles) != 2 || titles[0] != "Needs you" || titles[1] != activeSection {
		t.Fatalf("sections %v", titles)
	}
	if s, _, _ := m.rowSummary(broke); s != "stopped · Response stalled mid-stream." {
		t.Fatalf("summary %q", s)
	}
}
