package ui

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// ↑↓ in Agents pass over an open section's heading and stop on a folded
// one; there the agent picked before stays the one shown beside the list.
func TestListMoveSkipsOpenHeadings(t *testing.T) {
	a, b, c := &fleet.Agent{Key: "a"}, &fleet.Agent{Key: "b"}, &fleet.Agent{Key: "c"}
	m := &Model{store: &state.Store{}, order: []*fleet.Agent{a, b, c}}
	m.lines = []listLine{
		{kind: lineSection, title: "Working"}, {kind: lineAgent, agent: a},
		{kind: lineSection, title: "Idle"}, {kind: lineAgent, agent: b},
		{kind: lineSection, title: "Earlier"},
	}
	m.sel = "a"
	m.move(1)
	if m.sel != "b" {
		t.Fatalf("↓ from a: on %q, want b", m.sel)
	}
	m.move(1)
	if m.sel != "§Earlier" || m.focused() != b {
		t.Fatalf("↓ from b: on %q, showing %v", m.sel, m.focused())
	}
	m.move(-1)
	m.move(-1)
	if m.sel != "a" {
		t.Fatalf("↑↑: on %q, want a", m.sel)
	}
}
