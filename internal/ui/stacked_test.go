package ui

import "testing"

// Rows stack once the list has StackAt percent of the screen or less, and
// only when too narrow once that's turned off.
func TestStackedAtShare(t *testing.T) {
	m, _ := infoModel(t)
	m.w = 300
	if !m.stacked(126, 20) { // 42% of 300
		t.Error("list at the default 42% should stack")
	}
	if m.stacked(130, 20) {
		t.Error("list over 42% with room should not stack")
	}
	m.store.Config.StackAt = -1
	if m.stacked(126, 20) {
		t.Error("turned off, a roomy list should not stack")
	}
	if !m.stacked(40, 20) {
		t.Error("too narrow always stacks")
	}
}
