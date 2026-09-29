package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
)

const pasted = "one\ntwo\nthree\nfour"

// Space with the pointer on a paste chip in the Prompt opens it in place;
// undo is by hand, the chip's text stays kept.
func TestSpaceOpensTheChipUnderThePointer(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, mode: modeList}
	chip := m.pastes.add(pasted)
	m.input = []rune("see " + chip + " please")
	m.promptBox, m.promptBoxY = box{w: 80, text: m.input, cursor: len(m.input), anchor: -1}, 10
	// "│ " then "see " comes before the chip.
	m.update(tea.MouseMotionMsg{X: 2 + 5, Y: 11})
	if m.chipHot.box != 2 {
		t.Fatalf("no chip lit under the pointer: %+v", m.chipHot)
	}
	b := m.promptBoxAt(80)
	if b.hot != m.chipHot.at {
		t.Fatal("the Prompt doesn't draw the chip lit")
	}
	m.update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := string(m.input); got != "see "+pasted+" please" || m.cursorPos() != len([]rune("see "+pasted)) {
		t.Fatalf("input %q, cursor %d", got, m.cursorPos())
	}
	if m.chipHot.box != 0 {
		t.Fatal("still lit once opened")
	}

	// Off the chip, space is a space; and a key clears what's lit.
	m.input = []rune("see " + chip)
	m.setCursor(len(m.input))
	m.promptBox.text = m.input
	m.update(tea.MouseMotionMsg{X: 2 + 1, Y: 11})
	if m.chipHot.box != 0 {
		t.Fatal("lit off the chip")
	}
	m.update(tea.MouseMotionMsg{X: 2 + 5, Y: 11})
	m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.chipHot.box != 0 || !strings.Contains(string(m.input), chip) {
		t.Fatalf("typing left it lit or opened it: %q", string(m.input))
	}
}

// A click on a chip in the Session's box opens it, and undo folds it again.
func TestClickOpensTheChip(t *testing.T) {
	c := &hostConn{kind: "claude", key: "k", client: &host.Client{}, sess: convo.New(), open: map[string]bool{}}
	m := &Model{snap: &fleet.Snapshot{}, host: c, listW: 40, mode: modeList}
	chip := c.pastes.add(pasted)
	c.input = []rune(chip)
	c.box, c.boxY = box{w: 60, text: c.input, cursor: len(c.input), anchor: -1, lead: "❯ "}, 20
	// x0 is listW+3; then "│ " and the lead.
	if !m.clickBox(43+2+2+3, 21) {
		t.Fatal("the click missed the box")
	}
	if string(c.input) != pasted || c.back != 0 || m.boxDrag != 0 || !m.paneFocus {
		t.Fatalf("input %q back %d drag %d", string(c.input), c.back, m.boxDrag)
	}
	if !m.undoKey(c, "ctrl+/") || string(c.input) != chip {
		t.Fatalf("undo: %q", string(c.input))
	}

	// A click past the chip places the cursor, as ever.
	c.box.text = c.input
	c.box.w = 60
	m.clickBox(43+2+2+len(chip)+3, 21)
	if string(c.input) != chip || m.boxDrag != 1 {
		t.Fatalf("a click off the chip opened it: %q", string(c.input))
	}
}

// A space typed into something over the boxes (a menu, here) is that
// thing's, even with the pointer on a chip.
func TestSpaceOverAMenuLeavesTheChip(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, mode: modeList}
	chip := m.pastes.add(pasted)
	m.input = []rune("see " + chip)
	m.promptBox, m.promptBoxY = box{w: 80, text: m.input, cursor: len(m.input), anchor: -1}, 10
	m.picker = &picker{}
	m.update(tea.MouseMotionMsg{X: 2 + 5, Y: 11})
	if m.chipHot.box != 0 {
		t.Fatal("a chip lit under an open menu")
	}
	m.picker = nil
	m.update(tea.MouseMotionMsg{X: 2 + 5, Y: 11})
	m.picker = &picker{}
	m.update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !strings.Contains(string(m.input), chip) {
		t.Fatal("space opened a chip while a menu had the keys")
	}
}
