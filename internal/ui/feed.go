package ui

import (
	"time"

	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// agentEvent is a line in the feed: an agent finished, asked, or failed.
type agentEvent struct {
	at                       time.Time
	key, name, colour, glyph string
	what                     string
}

// noteEvent adds a line to the feed, keeping the last 30.
func (m *Model) noteEvent(a *fleet.Agent, colour, glyph, what string) {
	if what == "" {
		what = oneLine(a.Detail)
	}
	m.events = append(m.events, agentEvent{at: time.Now(), key: a.Key, name: oneLine(a.DisplayName), colour: colour, glyph: glyph, what: what})
	if n := len(m.events); n > 30 {
		m.events = m.events[n-30:]
	}
}

// feedLines is the feed for the list's empty foot, newest first, in at
// most room rows (none when there's no room for a heading and a line),
// with each row's agent for a click.
func (m *Model) feedLines(w, room int) (lines, keys []string) {
	if room < 3 || len(m.events) == 0 {
		return nil, nil
	}
	lines, keys = []string{"", faint("  from agents")}, []string{"", ""}
	now := time.Now()
	for i := len(m.events) - 1; i >= 0 && len(lines) < room; i-- {
		e := m.events[i]
		age := dim(age(now.Sub(e.at)))
		left := "  " + paint(e.colour, e.glyph) + " " + paint(cText, cellw.Truncate(e.name, max(8, w/3), "…")) + " " + dim(e.what)
		lines = append(lines, spread(fit(left, w-cellw.String(age)-1), age, w))
		keys = append(keys, e.key)
	}
	return lines, keys
}
