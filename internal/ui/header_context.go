package ui

import "fmt"

func headerContext(c *hostConn) string {
	s := c.sess
	if s.Context <= 0 || s.ContextWindow() <= 0 {
		return ""
	}
	pct := 100 * float64(s.Context) / float64(s.ContextWindow())
	colour := cText
	if pct >= 90 {
		colour = cRed
	} else if pct >= 80 {
		colour = cYellow
	}
	value := paint(cSub, "context ") + paint(colour, fmt.Sprintf("%.0f%%", pct))
	if c.pointerHover.context {
		return hoverLine(value, cellWidthContext(pct))
	}
	return value
}
func cellWidthContext(pct float64) int { return len(fmt.Sprintf("context %.0f%%", pct)) }
func (m *Model) contextAt(c *hostConn, x, y int) bool {
	left := m.paneX() + c.contextLabel[0]
	return !m.zenFull() && y == m.paneTop+paneTitleRow && c.contextLabel[1] > 0 && x >= left && x < left+c.contextLabel[1]
}
