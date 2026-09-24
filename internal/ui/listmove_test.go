package ui

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// ↑↓ in Agents stop on every section heading, open or folded; there the
// agent picked before stays the one shown beside the list.
func TestListMoveStopsOnHeadings(t *testing.T) {
	a, b := &fleet.Agent{Key: "a"}, &fleet.Agent{Key: "b"}
	m := &Model{store: &state.Store{}, order: []*fleet.Agent{a, b}}
	m.lines = []listLine{
		{kind: lineSection, title: "Working"}, {kind: lineAgent, agent: a},
		{kind: lineSection, title: "Idle"}, {kind: lineAgent, agent: b},
		{kind: lineSection, title: "Earlier"},
	}
	m.sel = "a"
	m.move(1)
	if m.sel != "§Idle" || m.focused() != a {
		t.Fatalf("↓ from a: on %q, showing %v", m.sel, m.focused())
	}
	m.move(1)
	if m.sel != "b" {
		t.Fatalf("↓↓ from a: on %q, want b", m.sel)
	}
	m.move(1)
	if m.sel != "§Earlier" || m.focused() != b {
		t.Fatalf("↓ from b: on %q, showing %v", m.sel, m.focused())
	}
	m.move(-4)
	if m.sel != "§Working" || m.focused() != b {
		t.Fatalf("to the top: on %q, showing %v", m.sel, m.focused())
	}
}
