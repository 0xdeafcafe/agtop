package ui

import (
	"runtime"
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
// alone, and splitting it keeps the split, next time too.
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
	if got := m.viewNow(); got != "split" || m.store.Config.ListOnly || m.store.Config.View != "split" {
		t.Fatalf("shift+→ from it splits, and keeps the split: %s %+v", got, m.store.Config)
	}
	closeIt()
	if got := m.viewNow(); got != "split" {
		t.Fatalf("closing it leaves the split, got %s", got)
	}
}

// The layout picked is kept for next time: #view agent opens agtop on the
// Session alone, #view list on Agents alone.
func TestViewKept(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	m.store.Config.MenuBarAsked = true
	for _, want := range []string{"agent", "list", "split"} {
		m.command(a, "#view "+want)
		saved := state.Load()
		next := &Model{store: saved, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
		next.startView()
		if next.sheet != nil || saved.Config.View != want || next.viewNow() != want {
			t.Fatalf("#view %s: next time opened on %s (kept %q)", want, next.viewNow(), saved.Config.View)
		}
	}
}

// The first time, agtop asks which layout; a layout kept before there was
// a choice counts as the answer.
func TestViewAsked(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	m.store.Config.MenuBarAsked = true
	m.startView()
	if _, ok := m.sheet.(*viewSheet); !ok {
		t.Fatalf("the first time should ask which layout")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyDown})
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sheet != nil || m.store.Config.View != "agent" || m.viewNow() != "agent" {
		t.Fatalf("picking the Session alone should keep and show it: %q %s", m.store.Config.View, m.viewNow())
	}

	old := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	old.store.Config.ListOnly, old.store.Config.MenuBarAsked = true, true
	old.startView()
	if old.sheet != nil || old.viewNow() != "list" {
		t.Fatalf("Agents alone kept from before shouldn't ask: %s", old.viewNow())
	}
}

// Typing #view's choices lays the screen out without asking the layout
// which layout is on (that went round forever).
func TestHashViewPicker(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a", inKind: inPrompt}
	m.input = []rune("#view ")
	if cmds, _ := m.promptPicker(); len(cmds) != 3 {
		t.Fatalf("#view should offer its three layouts: %v", cmds)
	}
	m.layout()
}

// With the Session alone kept, leaving it is a peek at Agents: enter opens
// the one picked, alone again, and esc goes back to the one you left.
func TestPeekFromSession(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a, b := &fleet.Agent{Key: "a", DisplayName: "alpha"}, &fleet.Agent{Key: "b", DisplayName: "beta"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{Agents: []*fleet.Agent{a, b}}, w: 240, h: 50, order: []*fleet.Agent{a, b}, sel: "a", inKind: inPrompt, previews: map[string]previewEntry{}}
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	m.command(a, "#view agent")
	m.key(esc)
	if !m.peeking() || m.viewNow() != "list" {
		t.Fatalf("esc from the Session alone should peek at Agents: %s", m.viewNow())
	}
	m.sel = "b"
	m.key(enter)
	if m.viewNow() != "agent" || m.sel != "b" || m.inKind == inRename || m.sheet != nil {
		t.Fatalf("enter while peeking should open beta alone: %s %s", m.viewNow(), m.sel)
	}
	m.key(esc)
	m.sel = "a"
	m.key(esc)
	if m.viewNow() != "agent" || m.sel != "b" {
		t.Fatalf("esc while peeking should go back to beta: %s %s", m.viewNow(), m.sel)
	}

	m.command(b, "#view split")
	m.key(esc)
	if m.peeking() {
		t.Fatalf("closing a split Session isn't a peek")
	}
}

// On a Mac, once the layout's picked, agtop offers the menu bar icon, once.
func TestMenuBarAsked(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the menu bar is macOS only")
	}
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := &fleet.Agent{Key: "a"}
	m := &Model{store: &state.Store{}, snap: &fleet.Snapshot{}, w: 240, h: 50, order: []*fleet.Agent{a}, sel: "a"}
	m.startView()
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := m.sheet.(*menuBarSheet); !ok {
		t.Fatalf("after the layout it should offer the menu bar")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sheet != nil || m.store.Config.MenuBar || !state.Load().Config.MenuBarAsked {
		t.Fatalf("not now should keep it off and not ask again: %+v", m.store.Config)
	}
	m.startView()
	if m.sheet != nil {
		t.Fatalf("it asks only once")
	}
}
