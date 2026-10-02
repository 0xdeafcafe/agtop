package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// A pinned agent shows as a tile beside the Session, or under it, and a
// click on the tile swaps it in.
func TestGridPinsAndSwaps(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(240, 60)
	open := m.focused()
	other := m.snap.Agents[1]
	m.grid.keys = []string{other.Key}
	for _, below := range []bool{false, true} {
		m.grid.under = map[string]bool{other.Key: below}
		out := ansi.Strip(m.listView())
		if !strings.Contains(out, "agent number 1 doing things") || len(m.grid.at) != 1 {
			t.Fatalf("below=%v: tile missing (%d drawn)", below, len(m.grid.at))
		}
		at := m.grid.at[0]
		if below && at.y <= m.paneTop || !below && at.x <= m.paneX() {
			t.Fatalf("below=%v: tile at %+v", below, at)
		}
	}
	at := m.grid.at[0]
	if _, ok := m.gridClick(at.x+2, at.y+1); !ok || m.sel != other.Key || m.grid.keys[0] != open.Key {
		t.Fatalf("click didn't swap: sel %s keys %v", m.sel, m.grid.keys)
	}
}

// A pinned agent's session stays open when another is opened: its tile is
// its Session, its output reaches it, and a click takes it back in at once.
func TestGridKeepsTileSessions(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(240, 60)
	first := m.host
	m.gridPin(false)
	m.sel = m.snap.Agents[1].Key
	m.syncHost()
	if m.grid.conns[first.key] != first || m.host == first {
		t.Fatalf("not kept: conns %v host %v", m.grid.conns, m.host)
	}
	m.listView()
	if len(m.grid.at) != 1 || !strings.Contains(ansi.Strip(strings.Join(m.gridSession(m.snap.Agents[0], 80, 30), "\n")), "now make streaming quicker") {
		t.Fatal("the tile isn't drawn as its Session")
	}
	m.sel = first.key
	m.syncHost()
	if m.host != first || m.grid.conns[first.key] != nil {
		t.Fatal("the kept session wasn't taken back in")
	}
}

// Held, the edge between the Session and the tiles resizes them.
func TestGridEdgeResizes(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(240, 60)
	m.grid.keys = []string{m.snap.Agents[1].Key}
	m.listView()
	at := m.grid.at[0]
	if _, ok := m.gridClick(at.x, at.y+3); !ok || !m.grid.drag {
		t.Fatal("the edge isn't a handle")
	}
	m.Update(tea.MouseMotionMsg{X: at.x - 40, Y: at.y + 3, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: at.x - 40, Y: at.y + 3, Button: tea.MouseLeft})
	m.listView()
	if m.grid.drag || m.grid.at[0].x >= at.x-10 || m.store.Config.GridShare == 0 {
		t.Fatalf("not resized: %+v → %+v", at, m.grid.at[0])
	}
}

// Pinned beside and under at once: the column beside the Session as tall
// as it, the row under both as wide as the pane; and that's kept.
func TestGridMixedLayout(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(240, 60)
	a1, a2 := m.snap.Agents[1].Key, m.snap.Agents[2].Key
	m.grid.keys, m.grid.under = []string{a1, a2}, map[string]bool{a2: true}
	m.listView()
	if len(m.grid.at) != 2 {
		t.Fatalf("%d tiles drawn", len(m.grid.at))
	}
	col, row := m.grid.at[0], m.grid.at[1]
	if col.key != a1 || row.key != a2 || col.x <= m.paneX() || row.x != m.paneX() || row.y != col.y+col.h || row.w <= col.x-m.paneX() {
		t.Fatalf("laid wrong: column %+v row %+v", col, row)
	}
	m.gridSave()
	if c := m.store.Config; len(c.GridBelowKeys) != 1 || c.GridBelowKeys[0] != a2 || c.GridBelow {
		t.Fatalf("not kept: %v %v", c.GridBelowKeys, c.GridBelow)
	}
	// The column's edge still resizes.
	if _, ok := m.gridClick(col.x, col.y+2); !ok || !m.grid.drag {
		t.Fatal("the column's edge isn't a handle")
	}
}

// A click on a tile's box gives it the keys without swapping it in: what's
// typed lands in its box, the open Session stays; esc hands them back.
func TestGridTileFocusTakesKeys(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(240, 60)
	first := m.host
	m.gridPin(false)
	m.sel = m.snap.Agents[1].Key
	m.syncHost()
	m.listView()
	at := m.grid.at[0]
	open := m.sel
	if _, ok := m.gridClick(at.x+3, at.y+(first.dockY-m.paneTop)+1); !ok || m.grid.focus != first.key || m.sel != open {
		t.Fatalf("box click: focus %q sel %s", m.grid.focus, m.sel)
	}
	if !strings.Contains(m.listView(), "┃") {
		t.Fatal("the focused tile isn't marked")
	}
	first.input = nil
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if string(first.input) != "x" || m.sel != open || m.host == first {
		t.Fatalf("key went astray: tile %q", string(first.input))
	}
	first.input = nil
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.grid.focus != "" {
		t.Fatal("esc on an empty box didn't hand the keys back")
	}
	// Elsewhere on the tile still swaps it in.
	m.listView()
	at = m.grid.at[0]
	if _, ok := m.gridClick(at.x+3, at.y+1); !ok || m.sel != first.key {
		t.Fatalf("click didn't swap: sel %s", m.sel)
	}
}

// One tile beside the Session halves the pane, X|X; two make thirds, side
// by side; one under it halves the height.
func TestGridEqualSplit(t *testing.T) {
	m, _ := benchModel(240, 60)
	m.grid = gridState{keys: []string{m.order[1].Key}, under: map[string]bool{}}
	if l := m.gridSplit(180, 50); len(l.col) != 1 || l.sw != l.cw || l.ch != l.sh {
		t.Fatalf("X|X: session %d, tile %dx%d of %d", l.sw, l.cw, l.ch, l.sh)
	}
	m.grid.keys = append(m.grid.keys, m.order[2].Key)
	if l := m.gridSplit(180, 50); len(l.col) != 2 || l.sw != 60 || l.cw != 60 {
		t.Fatalf("X|X|X: session %d, tiles %d", l.sw, l.cw)
	}
	m.grid = gridState{keys: []string{m.order[1].Key}, under: map[string]bool{m.order[1].Key: true}}
	if l := m.gridSplit(180, 50); len(l.row) != 1 || l.sh != 25 || l.rh != 25 {
		t.Fatalf("X over X: session %d, row %d", l.sh, l.rh)
	}
}
