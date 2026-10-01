package convo

import (
	"strconv"
	"strings"
)

// Activity draws only the current turn's status, independently of turn folds
// and scroll position. Standalone transcript renderers keep their inline status.
func (s *Session) Activity(o Options) []Line {
	t := s.Live()
	o.Width = max(20, o.Width)
	d := &drawer{s: s, t: t, o: o, cw: min(o.Width, o.rowCap()), docked: true}
	switch {
	case t != nil:
		d.ref = "t" + strconv.Itoa(t.N)
		d.liveLine()
	case !s.compacting.IsZero():
		d.compactingLine() // rush compacting it with no turn running
	default:
		return nil
	}
	rows := d.lines
	for len(rows) > 0 && strings.TrimSpace(stripANSI(rows[0].Text)) == strings.TrimSpace(stripANSI(d.spine())) {
		rows = rows[1:]
	}
	return rows
}
