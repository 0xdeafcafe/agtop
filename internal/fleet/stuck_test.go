package fleet

import (
	"testing"
	"time"
)

func TestStuck(t *testing.T) {
	now := time.Now()
	a := &Agent{}
	a.State, a.UpdatedAt = "working", now.Add(-31*time.Minute)
	if !a.Stuck(now) {
		t.Fatal("quiet 31m at work should be stuck")
	}
	a.Subagents = []SubagentTile{{ID: "s", Mod: now.Add(-6 * time.Minute)}}
	if a.Stuck(now) {
		t.Fatal("a subagent writing 6m ago means it is not stuck")
	}
	if got := a.StuckSubs(now); len(got) != 1 {
		t.Fatalf("subagent quiet 6m should be stuck, got %d", len(got))
	}
	a.State = "done"
	a.Subagents = nil
	if a.Stuck(now) {
		t.Fatal("a finished agent is never stuck")
	}
}
