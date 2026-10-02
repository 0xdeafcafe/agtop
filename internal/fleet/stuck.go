package fleet

import "time"

// StuckAfter is how long an agent at work may go without writing anything
// before it shows as stuck; StuckSubAfter is the same for a subagent run,
// whose prompt cache lasts five minutes, so a longer wait buys nothing.
const (
	StuckAfter    = 30 * time.Minute
	StuckSubAfter = 5 * time.Minute
)

// Stuck is an agent at work that has written nothing, nor have its
// subagents, for StuckAfter: a hung command, or a wait that never ends.
func (a *Agent) Stuck(now time.Time) bool {
	if a.Past || a.Done || a.Checking || !(a.State == "working" || a.Busy()) {
		return false
	}
	return a.Quiet(now) >= StuckAfter
}

// Quiet is how long since it, or any subagent of it, last wrote.
func (a *Agent) Quiet(now time.Time) time.Duration {
	last := a.UpdatedAt
	if a.ModTime.After(last) {
		last = a.ModTime
	}
	for _, s := range a.Subagents {
		if s.Mod.After(last) {
			last = s.Mod
		}
	}
	return now.Sub(last)
}

// StuckSubs are its subagent runs quiet for StuckSubAfter.
func (a *Agent) StuckSubs(now time.Time) []SubagentTile {
	var out []SubagentTile
	for _, s := range a.Subagents {
		if !s.Mod.IsZero() && now.Sub(s.Mod) >= StuckSubAfter {
			out = append(out, s)
		}
	}
	return out
}
