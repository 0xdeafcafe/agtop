package ui

import "github.com/0xdeafcafe/rush/internal/convo"

// rowOf is the row of the first of body's rows with ref, or -1, where body
// is rows from base on. In a conversation it looks from where the turn
// holding ref begins (rows put in after its steps only push it further
// down), so it reads that turn's rows, not the whole window's.
func rowOf(body []convo.Line, base int, ref string, s *convo.Session, conv bool) int {
	from := 0
	if conv && s != nil {
		if t := s.TurnOf(ref); t >= 0 {
			from = min(max(0, s.TurnRow(t)-base), len(body))
		}
	}
	for i := from; i < len(body); i++ {
		if body[i].Ref == ref {
			return base + i
		}
	}
	for i := range from { // drawn since another way: look above it too
		if body[i].Ref == ref {
			return base + i
		}
	}
	return -1
}
