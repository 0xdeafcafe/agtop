package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// What agents did lately fills the list's empty foot, newest first, and a
// click on a line picks its agent.
func TestFeedFillsListFoot(t *testing.T) {
	m, _ := benchModel(200, 60)
	m.snap.Agents = m.snap.Agents[:3]
	m.rebuild()
	m.noteEvent(m.snap.Agents[1], cGreen, "✓", "finished")
	m.noteEvent(m.snap.Agents[2], cOrange, "?", "which way?")
	out := ansi.Strip(m.listView())
	i := strings.Index(out, "from agents")
	feed := out[max(0, i):]
	if j := strings.Index(feed, "which way?"); i < 0 || j < 0 || strings.Index(feed, "finished") < j {
		t.Fatalf("feed missing or out of order:\n%s", out)
	}
	y := 0
	for i, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "which way?") {
			y = i
		}
	}
	if k := m.rowAt(0, y); k != m.snap.Agents[2].Key {
		t.Fatalf("newest line's row picks %q; keys %q", k, m.rowKeys)
	}
}
