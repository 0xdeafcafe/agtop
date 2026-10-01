package ui

import "github.com/0xdeafcafe/rush/internal/convo"

// activitySession supplies the running indicator only on the chat page.
func (m *Model) activitySession(c *hostConn) *convo.Session {
	if m.viewName(c) != "conversation" {
		return nil
	}
	return c.sess
}

// paneActivity is kept outside the transcript's scroll storage, so a scroll
// frame can reuse the text while its current timer and animation stay fresh.
func (m *Model) paneActivity(c *hostConn, w int) []convo.Line {
	active := m.activitySession(c)
	if active == nil {
		return nil
	}
	if l := m.live; active == c.sess && c.client == nil && l != nil && l.key == c.key && l.ready.Load() {
		if working := readScreen(l.lines()).working; working != "" {
			return []convo.Line{{Text: fit("    "+paint(cOrange, working), w)}}
		}
	}
	return active.Activity(convo.Options{Width: w, Now: paneNow(), Tick: m.tick, Wide: m.hostedAlone()})
}
