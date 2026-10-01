package ui

import (
	"time"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// jobActivity reports observed output activity, not a guessed process state.
// A silent task can be healthy (a watcher or a long compile, for example).
// This only reads cached file metadata; rendering never waits for a stat.
func (c *hostConn) jobActivity(j *convo.Job, from string, now time.Time) string {
	if !j.Running() {
		return ""
	}
	if from == "" {
		from = c.jobOutput(j)
	}
	if e := c.tails[from]; e != nil && !e.mod.IsZero() && e.size > 0 {
		quiet := max(time.Duration(0), now.Sub(e.mod))
		if quiet >= time.Minute {
			return paint(cYellow, "quiet for "+age(quiet)) + paint(cSub, " · may be waiting; silence does not mean stuck")
		}
		return paint(cGreen, "output active") + paint(cSub, " · updated "+age(quiet)+" ago")
	}
	if !j.ProgressAt.IsZero() {
		return paint(cSub, "progress updated "+age(max(time.Duration(0), now.Sub(j.ProgressAt)))+" ago · no captured output")
	}
	if !j.Start.IsZero() {
		return paint(cSub, "no captured output · running for "+age(max(time.Duration(0), now.Sub(j.Start))))
	}
	return paint(cSub, "no captured output · activity unknown")
}
