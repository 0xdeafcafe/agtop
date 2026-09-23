package ui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// railNow heads the rail with what the agent is on right now: its task and
// the next ones, what's queued, and subagents still running. Empty when
// there's none of that.
func (m *Model) railNow(c *hostConn, w int) []string {
	var out []string
	add := func(s string) { out = append(out, fit(s, w)) }
	now, done, total := c.sess.Current()
	if total > 0 && done < total {
		add(" " + paint(cSub+bold, "Tasks") + "  " + dim(fmt.Sprintf("%d of %d done", done, total)))
		if now != nil {
			add(" " + paint(cOrange, "■ ") + paint(cText, ansi.Truncate(firstNonEmpty(now.Active, now.Subject), w-4, "…")))
		}
		shown := 0
		for _, t := range c.sess.Tasks {
			if t.Status == "pending" && shown < 3 {
				add(" " + dim("☐ "+ansi.Truncate(t.Subject, w-4, "…")))
				shown++
			}
		}
		add("")
	}
	if q := m.queueOf(c).items; len(q) > 0 {
		add(" " + paint(cSub+bold, "Queued") + "  " + dim(fmt.Sprint(len(q))))
		for i, item := range q {
			if i == 2 {
				add(" " + dim(fmt.Sprintf("… %d more", len(q)-i)))
				break
			}
			add(" " + dim("· "+ansi.Truncate(oneLine(item), w-4, "…")))
		}
		add("")
	}
	if run := c.runningSubs(); len(run) > 0 {
		add(" " + paint(cSub+bold, "Subagents") + "  " + paint(cOrange, fmt.Sprintf("%d running", len(run))))
		for i, sa := range run {
			if i == 4 {
				add(" " + dim(fmt.Sprintf("… %d more", len(run)-i)))
				break
			}
			add(" " + paint(cOrange, spinner[(m.tick+i)%len(spinner)]) + " " + paint(cText, sa.Type) + " " +
				dim(ansi.Truncate(oneLine(sa.Description), max(4, w-len(sa.Type)-5), "…")))
		}
		add("")
	}
	return out
}
