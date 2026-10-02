package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// What agents did lately fills the list's empty foot, newest first, and a
// click on a line picks its agent.
func TestFeedFillsListFoot(t *testing.T) {
	showAgentFeed(t)
	m, _ := benchModel(200, 60)
	m.snap.Agents = m.snap.Agents[:3]
	m.rebuild()
	m.noteEvent(m.snap.Agents[1], cGreen, "✓", "finished")
	m.noteEvent(m.snap.Agents[2], cOrange, "?", "which way?")
	out := ansi.Strip(m.listView())
	i := strings.Index(out, "from your agents")
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

// A finish is routine, dimmed, unless its last words are news: then they
// show instead of "finished".
func TestFeedNews(t *testing.T) {
	showAgentFeed(t)
	routine := agentEvent{glyph: "✓", what: "finished", said: "Tests pass; committed the fix."}
	news := agentEvent{glyph: "✓", what: "finished", said: "Turns out the cache was never written: root cause found."}
	if !routine.routine() || news.routine() || (agentEvent{glyph: "✗"}).routine() {
		t.Fatal("routine finishes dim; news and failures don't")
	}
	m, _ := benchModel(200, 60)
	m.snap.Agents = m.snap.Agents[:3]
	m.rebuild()
	a := m.snap.Agents[1]
	a.Detail = news.said
	m.noteEvent(a, cGreen, "✓", "finished")
	out := ansi.Strip(m.listView())
	if !strings.Contains(out, "@agent-number-1-doing-things") && !strings.Contains(out, "@agent-number-1") {
		t.Errorf("the feed should name the agent by its @handle:\n%s", out)
	}
	if !strings.Contains(out, "Turns out the cache") {
		t.Errorf("news should show its words:\n%s", out)
	}
}

func showAgentFeed(t *testing.T) {
	agentFeedShown = true
	t.Cleanup(func() { agentFeedShown = false })
}

func TestAgentFeedHiddenByDefault(t *testing.T) {
	m, _ := benchModel(100, 35)
	m.events = []agentEvent{{at: time.Now(), key: "a", name: "A", glyph: "▶", what: "started"}}
	if lines, _ := m.feedLines(80, 20); lines != nil {
		t.Fatal("the from-your-agents box drew while hidden")
	}
}
