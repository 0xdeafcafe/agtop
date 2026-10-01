package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// A long session laid out again for a new width (the list's edge dragged,
// the terminal resized) takes longer than a frame. So each frame draws the
// Session for at most relayBudget, its end first, and shows the rest as it
// was; relayoutMsg brings the next frame, until it's all drawn afresh.
const (
	relayBudget = 6 * time.Millisecond
	relayEvery  = 16 * time.Millisecond
)

type relayoutMsg struct{}

// relayout asks for another frame soon, when the Session is to be drawn at
// a new width or the last frame left some of it as it was.
func (m *Model) relayout() tea.Cmd {
	c := m.host
	if c == nil || m.relayPending {
		return nil
	}
	_, paneW, _ := m.layout()
	// The pane draws the Session 3 columns in from its edge (listView).
	width := paneW - 3
	if m.minimapEnabled(c, width) {
		width = m.conversationWidth(width)
	}
	if !c.stale && (!c.drewConvo || c.paneW == width) {
		return nil
	}
	m.relayPending = true
	return tea.Tick(relayEvery, func(time.Time) tea.Msg { return relayoutMsg{} })
}
