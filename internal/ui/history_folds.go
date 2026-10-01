package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/convo"
)

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
		m.flash("all turns open · tool details keep their own folds", false)
	} else {
		m.flash("older turns previewed · latest turn stays open", false)
	}
}

// historyChips is the header's one control for the conversation: every turn
// shows whole by default, and these two clicks (or the chords) change it all.
// It joins the chips already there when there is room (key hint first to
// go), recording its click targets (from the chips' start), and leaves them alone when there is not.
func (m *Model) historyChips(c *hostConn, chips string, room int) string {
	// One toggle: the mode in use lit, the other quiet, a dot between.
	open, closed, sep := "open all", "collapse older", faint(" · ")
	if c.historyMode == convo.HistoryCompact {
		open, closed = dim(open), paint(cOrange, closed)
	} else {
		open, closed = paint(cOrange, open), dim(closed)
	}
	w1, w2, ws := cellw.String(open), cellw.String(closed), cellw.String(sep)
	hint := dim("  " + m.boundKey("session.history.open") + " / " + m.boundKey("session.history.close"))
	for _, tail := range []string{hint + "  ", "  "} {
		mine := open + sep + closed + tail
		if cellw.String(chips+mine) > room {
			continue
		}
		at := cellw.String(chips)
		c.histTabs = append(c.histTabs, paneTab{at, at + w1, 1}, paneTab{at + w1 + ws, at + w1 + ws + w2, 0})
		return chips + mine
	}
	return chips
}

// clickHistory runs a header chip on the first click.
func (m *Model) clickHistory(c *hostConn, x, y int) bool {
	if m.zenFull() || y != m.paneTop+paneTabsRow {
		return false
	}
	for _, t := range c.histTabs {
		if x-m.paneX() >= t.start && x-m.paneX() < t.end {
			m.setHistoryFold(t.view == 1)
			return true
		}
	}
	return false
}
