package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ⌥m, as a Mac sends it without Option as Meta, picks what the next
// session starts as with the list or the Session focused.
func TestAltMPicksNextStart(t *testing.T) {
	for _, focus := range []bool{false, true} {
		m, _ := benchModel(120, 40)
		m.paneFocus = focus
		m.key(macOption(tea.KeyPressMsg{Code: 'µ', Text: "µ"}))
		if m.sheet == nil {
			t.Errorf("focus on the Session %v: ⌥m should open the start sheet", focus)
		}
	}
}
