package ui

import (
	"github.com/0xdeafcafe/rush/internal/agent"
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

// cycleSendMode is ctrl+t: the next way enter sends to c.
func (m *Model) cycleSendMode(c *hostConn) {
	ms := c.sendModes()
	cur := m.sendModeOf(c)
	next := ms[0]
	for i, k := range ms {
		if k == cur {
			next = ms[(i+1)%len(ms)]
		}
	}
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

// sendModeTop is the box's border while the session works: what enter does,
// and the key that changes it.
func (m *Model) sendModeTop(c *hostConn) string {
	md := m.sendModeOf(c)
	top := dim(" · working, so ") + paint(cOrange, "enter "+sendModeWords[md])
	if len(c.sendModes()) > 1 {
		top += dim(" · ") + paint(cSub, "ctrl+t") + dim(" "+sendModeNames[md])
	}
	if md != sendStop && len(c.input) > 0 {
		top += dim(" · " + m.sendNowKey() + " stops it and sends")
	}
	return top
}
