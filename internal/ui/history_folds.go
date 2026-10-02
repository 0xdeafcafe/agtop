package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// historyOf is how chat c draws its turns: a room's whole, since each turn
// is a member speaking; the rest as set.
func (m *Model) historyOf(c *hostConn) convo.HistoryMode {
	if isRoomKey(c.key) {
		return convo.HistoryOpen
	}
	return c.historyMode
}

// setHistoryFold applies to turns still loading too. Tool-level overrides
// survive, and a later individual choice takes precedence over the mode.
func (m *Model) setHistoryFold(open bool) {
	c := m.host
	if c == nil || c.sess == nil {
		m.flash("open a session to change its history", false)
		return
	}
	c.historyMode = convo.HistoryCompact
	if open {
		c.historyMode = convo.HistoryOpen
	}
	for ref := range c.open {
		if strings.HasPrefix(ref, "t") && !strings.Contains(ref, ":") {
			delete(c.open, ref)
		}
	}
	// Collapsed, a selected step may disappear; keep focus on its turn. The
	// view isn't moved for it otherwise: the row at its top stays put.
	if turn, _, step := strings.Cut(c.sel, ":"); !open && step && strings.HasPrefix(c.sel, "t") {
		c.sel, c.selMoved = turn, true
	}
	c.view = 0
	c.scrollOnly = false
	c.drawn.History = c.historyMode
	c.paneKick = true
	if open {
		m.flash("every turn shown whole", false)
	} else {
		m.flash("turns gone past show what matters: what was said, what changed, what was made", false)
	}
}
