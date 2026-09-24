package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// The first enter on an agent asks whether enter renames or opens; y
// renames it, with its name selected, as in the Finder, and enter does from
// then on.
func TestEnterRenames(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a", Name: "one", DisplayName: "one"}
	b := &fleet.Agent{Key: "b", Name: "two", DisplayName: "two"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{Agents: []*fleet.Agent{a, b}}, w: 240, h: 50, sel: "a"}
	m.store.Overlay.Names = map[string]string{}
	m.lines = []listLine{{kind: lineAgent, agent: a}, {kind: lineAgent, agent: b}}
	m.order = []*fleet.Agent{a, b}

	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := m.sheet.(*enterSheet); !ok || m.inKind == inRename {
		t.Fatalf("the first enter should ask what enter does")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sheet != nil || m.store.Config.EnterOn != "rename" || m.inKind != inRename {
		t.Fatalf("enter should choose rename and rename a: %q, kind %v", m.store.Config.EnterOn, m.inKind)
	}
	m.inKind, m.input = inPrompt, nil
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sheet != nil || m.inKind != inRename || m.promptFor != "a" || string(m.input) != "one" {
		t.Fatalf("enter should rename a: kind %v for %q input %q", m.inKind, m.promptFor, string(m.input))
	}
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if string(m.input) != "x" {
		t.Fatalf("typing should replace the selected name: %q", string(m.input))
	}
}

// The row under the mouse is selected at once, not after it rests there,
// but not while a name is being typed.
func TestHoverSelects(t *testing.T) {
	a := &fleet.Agent{Key: "a", DisplayName: "one"}
	b := &fleet.Agent{Key: "b", DisplayName: "two"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, sel: "a", listW: 80, listTop: 2}
	m.order = []*fleet.Agent{a, b}
	m.rowKeys = []string{"a", "b"}

	m.mouseMove(5, 3)
	if m.sel != "b" {
		t.Fatalf("hovering b should select it: %q", m.sel)
	}
	m.inKind, m.promptFor = inRename, "b"
	m.mouseMove(5, 2)
	if m.sel != "b" {
		t.Fatalf("hovering while renaming shouldn't move the selection: %q", m.sel)
	}
}

// ctrl+r renames, and #rename alone puts the name in the box to edit.
func TestCtrlRAndHashRename(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a", Name: "one", DisplayName: "one"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{Agents: []*fleet.Agent{a}}, w: 240, h: 50, sel: "a"}
	m.store.Overlay.Names = map[string]string{}
	m.lines = []listLine{{kind: lineAgent, agent: a}}
	m.order = []*fleet.Agent{a}

	m.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.inKind != inRename || string(m.input) != "one" {
		t.Fatalf("ctrl+r should rename a: kind %v input %q", m.inKind, string(m.input))
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.input, m.inKind = []rune("#rename"), inPrompt
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.inKind != inRename || string(m.input) != "one" {
		t.Fatalf("#rename should rename a: kind %v input %q", m.inKind, string(m.input))
	}
}
