package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Stepping the list's edge past either end leaves one side alone, and
// stepping back brings the split back.
func TestStepSplit(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	side := func() (int, int) { l, p, _ := m.layout(); m.listW = l; return l, p }

	if l, p := side(); l == 0 || p == 0 {
		t.Fatalf("a wide screen should start split: %d|%d", l, p)
	}
	for range 200 {
		m.stepSplit(true)
		side()
	}
	if l, p := side(); l != m.w || p != 0 || !m.store.Config.ListOnly {
		t.Fatalf("past the right end should be the list alone: %d|%d", l, p)
	}
	m.stepSplit(false)
	if l, p := side(); l == 0 || p == 0 || m.store.Config.ListOnly {
		t.Fatalf("alt+← should bring the split back: %d|%d", l, p)
	}
	for range 200 {
		m.stepSplit(false)
		side()
	}
	if l, p := side(); l != 0 || p != m.w {
		t.Fatalf("past the left end should be the Session alone: %d|%d", l, p)
	}
	m.stepSplit(true)
	if l, p := side(); l == 0 || p == 0 {
		t.Fatalf("alt+→ should bring the split back: %d|%d", l, p)
	}

	m.dragSplit(m.w - 1)
	if l, p := side(); p != 0 {
		t.Fatalf("dropping the edge on the right should leave the list: %d|%d", l, p)
	}
	m.dragSplit(80)
	if l, p := side(); l == 0 || p == 0 {
		t.Fatalf("dragging back should split again: %d|%d", l, p)
	}
	m.dragSplit(0)
	if l, _ := side(); l != 0 {
		t.Fatalf("dropping the edge on the left should leave the Session: %d", l)
	}
}

// shift+← → do it too (Terminal.app's option isn't alt), and so does #view.
func TestSplitKeys(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	m.listW, _, _ = m.layout()
	for range 200 {
		m.key(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
		m.listW, _, _ = m.layout()
	}
	if got := m.viewNow(); got != "list" {
		t.Fatalf("shift+→ past the end should be the list alone, got %s", got)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	if got := m.viewNow(); got != "split" {
		t.Fatalf("shift+← should split again, got %s", got)
	}
	for _, c := range []struct{ cmd, want string }{
		{"#view agent", "agent"}, {"#view list", "list"}, {"#view split", "split"},
		{"#view:orchestrator", "list"}, {"#view:agent", "agent"}, {"#view:split", "split"},
	} {
		m.command(a, c.cmd)
		if got := m.viewNow(); got != c.want {
			t.Fatalf("%s gave %s", c.cmd, got)
		}
	}
}

// From Agents alone, a Session opens as you last had one: pushed to the
// whole screen, the next one opens there too; closing goes back to Agents
// alone, and splitting it makes Sessions open beside the list again.
func TestChatOpensAsLast(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	m.command(a, "#view list")
	open := func() string { m.preview = true; return m.viewNow() }
	closeIt := func() { m.preview, m.full = false, false }

	if got := open(); got != "split" {
		t.Fatalf("a Session first opens beside the list, got %s", got)
	}
	m.command(a, "#view agent")
	closeIt()
	if got := m.viewNow(); got != "list" {
		t.Fatalf("closing a Session goes back to Agents alone, got %s", got)
	}
	if got := open(); got != "agent" {
		t.Fatalf("the next Session should open alone, got %s", got)
	}
	m.listW, _, _ = m.layout()
	m.stepSplit(true)
	if got := m.viewNow(); got != "split" || !m.store.Config.ListOnly {
		t.Fatalf("shift+→ from it splits, Agents alone still kept: %s", got)
	}
	closeIt()
	if got := open(); got != "split" {
		t.Fatalf("and Sessions open beside the list again, got %s", got)
	}
}
