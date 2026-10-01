package ui

import "strings"

const (
	paneTitleRow = 1
	paneMetaRow  = 2
	paneTabsRow  = 3
)

// paneFrameRow extends only the header into the existing pane gutters.
// Text and transcript widths stay unchanged, including minimap placement.
func (m *Model) paneFrameRow(b *strings.Builder, p string, w int, fade string, header bool, row int) {
	left, right := "  ", " "
	if header {
		switch {
		case row >= 0 && row < 4:
			left, right = onBg(bgChrome, "", 2), onBg(bgChrome, "", 1)
			if m.host != nil && m.host.sess != nil && w > 0 {
				colours := headerColours(w, row, sessionAgent(m.host))
				right = onBg(colours[len(colours)-1], "", 1)
			}
		case row == 4:
			if m.host != nil && m.host.sess != nil {
				border := harnessBorder(sessionAgent(m.host))
				left, right = paint(border, "──"), paint(border, "─")
			} else {
				left, right = faint("──"), faint("─")
			}
		}
	}
	b.WriteString(left)
	m.paneRow(b, p, w, fade)
	b.WriteString(right)
}
