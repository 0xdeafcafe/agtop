package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Working is ordered by how long each has been at this run: one already at
// it when rush opened counts its whole session, one that starts later
// counts from then, and leaving Working starts it again from zero.
func TestWorkingLongestRunFirst(t *testing.T) {
	now := time.Now()
	agent := func(key string, born time.Duration) *fleet.Agent {
		a := &fleet.Agent{Key: key, DisplayName: key, PID: 1}
		a.State, a.CreatedAt, a.UpdatedAt = "working", now.Add(-born), now
		return a
	}
	old, young := agent("old", time.Hour), agent("young", time.Minute)
	m := &Model{store: &state.Store{}, previews: map[string]previewEntry{}, w: 120, h: 40, lastState: map[string]string{}}
	m.snap = &fleet.Snapshot{At: now, Agents: []*fleet.Agent{young, old}}
	m.rebuild()
	if m.order[0] != old {
		t.Fatalf("the longest at it goes first, got %s", m.order[0].Key)
	}
	late := agent("late", 2*time.Hour) // an old session starting a new run
	m.snap = &fleet.Snapshot{At: now.Add(time.Minute), Agents: []*fleet.Agent{young, old, late}}
	m.rebuild()
	if got := m.runFor(late, m.snap.At); got != 0 {
		t.Errorf("a run rush saw start counts from then, got %v", got)
	}
	if m.order[len(m.order)-1] != late {
		t.Errorf("the newest run goes last, got %s", m.order[len(m.order)-1].Key)
	}
}
