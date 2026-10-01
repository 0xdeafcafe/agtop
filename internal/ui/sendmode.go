package ui

import (
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// sendMode is how enter sends to a session that's working: into its queue,
// into the turn under way without stopping it, or stopping it first. Each
// session keeps its own; ctrl+t goes round what its agent can do.
type sendMode int

const (
	sendQueue sendMode = iota
	sendGuide
	sendStop
)

// sendModes are the ways c's agent takes a message mid-turn.
func (c *hostConn) sendModes() []sendMode {
	out := []sendMode{sendQueue}
	if agent.Supports(c.kindOf(), agent.FeatureGuide) {
		out = append(out, sendGuide)
	}
	if agent.Supports(c.kindOf(), agent.FeatureInterrupt) {
		out = append(out, sendStop)
	}
	return out
}

// sendModeOf is how enter sends to c now: its pick, while its agent can.
func (m *Model) sendModeOf(c *hostConn) sendMode {
	md := m.sendModes[c.key]
	for _, k := range c.sendModes() {
		if k == md {
			return md
		}
	}
	return sendQueue
}

// nextSendMode is the way enter sends to c after the current one.
func (m *Model) nextSendMode(c *hostConn) sendMode {
	ms := c.sendModes()
	cur := m.sendModeOf(c)
	next := ms[0]
	for i, k := range ms {
		if k == cur {
			next = ms[(i+1)%len(ms)]
		}
	}
	return next
}

// cycleSendMode is ctrl+t: the next way enter sends to c.
func (m *Model) cycleSendMode(c *hostConn) {
	next := m.nextSendMode(c)
	if m.sendModes == nil {
		m.sendModes = map[string]sendMode{}
	}
	m.sendModes[c.key] = next
	m.flash("enter "+sendModeWords[next]+" while it works", false)
}

// sendModeWords say what enter does in each mode, and sendModeNames name it.
var (
	sendModeWords = map[sendMode]string{sendQueue: "queues", sendGuide: "guides: it reads it at its next step", sendStop: "stops it and sends"}
	sendModeNames = map[sendMode]string{sendQueue: "queue", sendGuide: "guide", sendStop: "stop & send"}
)

// sendModeStates and sendModeSwitches say how enter sends now, and name the
// mode a switch goes to: guide is what the box calls coalescing.
var (
	sendModeStates   = map[sendMode]string{sendQueue: "queuing", sendGuide: "coalescing", sendStop: "stop & send"}
	sendModeSwitches = map[sendMode]string{sendQueue: "queue", sendGuide: "coalesce", sendStop: "stop & send"}
)

// sendModeTop is the box's border while the session works: how enter sends,
// and the key that goes to the next way. Dim, and short.
func (m *Model) sendModeTop(c *hostConn) string {
	top := dim(" · ") + paint(cDim, sendModeStates[m.sendModeOf(c)])
	if next := m.nextSendMode(c); next != m.sendModeOf(c) {
		top += dim(" · " + m.boundKey("session.sendmode") + " " + sendModeSwitches[next])
	}
	return top
}

// boxKeys are the Session box's own keys, as they apply now: what enter
// does, sending now, how it sends while it works, stopping it, and where
// what's kept or cleared goes.
func (m *Model) boxKeys(c *hostConn, a *fleet.Agent, s *convo.Session) []string {
	live := c.client != nil && s.Live() != nil
	working := live || c.client == nil && busy(a)
	typed := len(c.input) > 0
	var pairs []string
	switch {
	case typed && live:
		pairs = append(pairs, "enter", sendModeKeys[m.sendModeOf(c)], m.sendNowKey(), "send now")
	case typed && working:
		pairs = append(pairs, "enter", "queue it", m.sendNowKey(), "send now")
	case typed:
		pairs = append(pairs, "enter", "send")
	case len(m.queueOf(c).items) > 0:
		pairs = append(pairs, m.sendNowKey(), "send the queue now")
	}
	if live && len(c.sendModes()) > 1 {
		pairs = append(pairs, m.boundKey("session.sendmode"), "queue · guide · stop")
	}
	if working {
		pairs = append(pairs, m.boundKey("session.stop"), "stop it")
	}
	w, on := m.stash()
	switch {
	case typed && on:
		pairs = append(pairs, m.boundKey("session.stash"), w.Verb)
	case time.Since(c.clearedAt) < 2*time.Minute:
		pairs = append(pairs, undoHint, "bring back what you cleared")
	case m.hasStash(c.key):
		pairs = append(pairs, m.boundKey("session.stash"), "your "+w.Noun+" back")
	}
	if !on {
		return pairs
	}
	return append(pairs, m.boundKey("session.history"), strings.ToLower(w.Tab)+" · sent · cleared")
}

// historyHint is what an empty Session box says of the history, when the
// stash is running.
func (m *Model) historyHint() string {
	w, on := m.stash()
	if !on {
		return ""
	}
	return " · " + m.boundKey("session.history") + ": " + strings.ToLower(w.Tab) + ", sent, cleared"
}

// sendModeKeys say what enter does in each mode, beside it in the hints.
var sendModeKeys = map[sendMode]string{sendQueue: "queue it", sendGuide: "guide it", sendStop: "stop & send"}

// boundKey is the first key bound to action id, as the hints name it.
func (m *Model) boundKey(id string) string {
	if ks := m.keyMap().Keys(id); len(ks) > 0 {
		return ks[0].String()
	}
	return ""
}
