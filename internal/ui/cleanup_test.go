package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Done work goes once it's been done and untouched for the wait; never while
// it runs, never when it isn't done, never when the tidy-up is off.
func TestCleanupDue(t *testing.T) {
	now := time.Now()
	st := &state.Store{}
	st.Overlay.Done = map[string]time.Time{}
	m := &Model{store: st, snap: &fleet.Snapshot{At: now}}
	add := func(key string, done time.Duration, active time.Duration, pid int) {
		a := &fleet.Agent{Key: key, PID: pid}
		a.UpdatedAt = now.Add(-active)
		if done >= 0 {
			a.Done = true
			st.Overlay.Done[key] = now.Add(-done)
		}
		m.snap.Agents = append(m.snap.Agents, a)
	}
	add("old", 5*time.Hour, 6*time.Hour, 0)
	add("recent", time.Hour, 2*time.Hour, 0)
	add("running", 5*time.Hour, 6*time.Hour, 42)
	add("notdone", -1, 9*time.Hour, 0)
	add("touched", 5*time.Hour, 30*time.Minute, 0) // done long ago, but worked on since

	if d := m.dueIn([]string{"old"}, now); d != 0 {
		t.Errorf("old: %v", d)
	}
	if d := m.dueIn([]string{"recent"}, now); d < 110*time.Minute || d > 2*time.Hour {
		t.Errorf("recent: %v, want about 2h", d)
	}
	if d := m.dueIn([]string{"touched"}, now); d < 2*time.Hour {
		t.Errorf("touched: %v, want 2h30m", d)
	}
	for _, k := range []string{"running", "notdone"} {
		if d := m.dueIn([]string{k}, now); d >= 0 {
			t.Errorf("%s: %v, should never be due", k, d)
		}
	}
	if d := m.dueIn([]string{"old", "recent"}, now); d <= 0 {
		t.Errorf("a worktree shared with a recent one waits for it: %v", d)
	}
	st.Config.CleanupHours = -1
	if d := m.dueIn([]string{"old"}, now); d >= 0 {
		t.Errorf("off: %v", d)
	}
}
