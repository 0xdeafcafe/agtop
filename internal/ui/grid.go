package ui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

// The grid: agents pinned beside the open Session, each a live tile drawn
// as the Wall draws one. The Session has the keys; a click on a tile
// swaps that agent in, and the one it replaces takes the tile.
type gridState struct {
	keys  []string             // the pinned agents, in order
	conns map[string]*hostConn // tiles' sessions kept open, by agent key
	under map[string]bool      // tiles in the row under the Session; the rest are in the column beside it
	focus string               // the tile whose box has the keys, typed into without swapping it in
	at    []wallTile           // where each tile was drawn, on screen, for clicks
	share float64              // how much of the pane the column takes (the row, with no column); 0 is a third
	box   [4]int               // the pane the grid was laid in: x, y, w, h on screen
	colX  int                  // the column's edge on screen, 0 with no column
	rowY  int                  // the row's edge on screen, 0 with no row
	drag  bool                 // the edge between the Session and the tiles is held
}

// gridLayout is how the pane divides: the Session top-left, the column
// beside it as tall as it is, the row under both as wide as the pane.
type gridLayout struct {
	sw, sh, cw, ch, rw, rh int
	col, row               []*fleet.Agent
}

// Tiles narrower or shorter than these aren't drawn: the Session has it all.
const (
	gridMinW = 36
	gridMinH = 7
)

// gridAgents are the pinned agents still about, the open one left out.
func (m *Model) gridAgents() []*fleet.Agent {
	var out []*fleet.Agent
	open := ""
	if f := m.focused(); f != nil {
		open = f.Key
	}
	for _, k := range m.grid.keys {
		if a := m.agentByKey(k); a != nil && k != open {
			out = append(out, a)
		}
	}
	return out
}

// gridPin pins the open agent as a tile, beside the Session or under it,
// and gives the keys to the list to pick the agent the Session shows next.
func (m *Model) gridPin(below bool) tea.Cmd {
	a := m.focused()
	if a == nil {
		m.flash("open an agent first", false)
		return nil
	}
	if m.grid.under == nil {
		m.grid.under = map[string]bool{}
	}
	m.grid.under[a.Key] = below
	if !slices.Contains(m.grid.keys, a.Key) {
		m.grid.keys = append(m.grid.keys, a.Key)
	}
	m.paneFocus = false
	where := "beside"
	if below {
		where = "under"
	}
	m.gridSave()
	m.flash(oneLine(a.DisplayName)+" pinned "+where+" the Session · pick the agent to open beside it", false)
	return nil
}

// gridUnpin takes the newest tile off, or every tile with all.
func (m *Model) gridUnpin(all bool) {
	switch {
	case len(m.grid.keys) == 0:
		m.flash("no tiles pinned", false)
	case all:
		m.grid.keys = nil
	default:
		m.grid.keys = m.grid.keys[:len(m.grid.keys)-1]
	}
	m.gridClose()
	m.gridSave()
}

// gridSplit is how the pane's w×h divides. No tiles, or no room for
// them, leaves the Session all of it.
func (m *Model) gridSplit(w, h int) gridLayout {
	l := gridLayout{sw: w, sh: h}
	if m.zen {
		return l
	}
	var col, row []*fleet.Agent
	for _, a := range m.gridAgents() {
		if m.grid.under[a.Key] {
			row = append(row, a)
		} else {
			col = append(col, a)
		}
	}
	if len(row) > 0 {
		// Alone, the row and the Session halve the height (or as dragged).
		// ponytail: with a column too the row is a fixed third; give it its own share if it wants dragging.
		rs := 1.0 / 3
		if len(col) == 0 {
			rs = cmp.Or(m.grid.share, 0.5)
		}
		rh, rw := max(gridMinH, int(float64(h)*rs)), w/len(row)
		if rw >= gridMinW && h-rh >= 12 {
			l.sh, l.rw, l.rh, l.row = h-rh, rw, rh, row
		}
	}
	if len(col) > 0 {
		// Side by side, X|X: the Session and each tile an equal width, or
		// the tiles share what the edge was dragged to.
		share := cmp.Or(m.grid.share, float64(len(col))/float64(len(col)+1))
		if cw := int(float64(w)*share) / len(col); cw >= gridMinW && w-cw*len(col) >= gridMinW {
			l.sw, l.cw, l.ch, l.col = w-cw*len(col), cw, l.sh, col
		}
	}
	return l
}

// gridJoin lays the tiles round the Session's rows, the pane at (x, y) on
// screen, and notes where each tile went for clicks.
func (m *Model) gridJoin(pane []string, x, y int, l gridLayout) []string {
	m.grid.at = m.grid.at[:0]
	m.grid.box = [4]int{x, y, l.sw + l.cw, l.sh + l.rh}
	m.grid.colX, m.grid.rowY = 0, 0
	draw := func(tiles []*fleet.Agent, tw, th int) [][]string {
		drawn := make([][]string, len(tiles))
		for i, a := range tiles {
			if drawn[i] = m.gridSession(a, tw-1, th); drawn[i] != nil {
				rule := faint("│") // a rule between it and what's beside it
				if a.Key == m.grid.focus {
					rule = paint(cOrange, "┃") // its box has the keys
				}
				for r := range drawn[i] {
					drawn[i][r] = rule + drawn[i][r]
				}
				continue
			}
			drawn[i] = m.wallAgentTile(a, tw, th, false)
		}
		return drawn
	}
	col := draw(l.col, l.cw, l.ch)
	out := make([]string, 0, l.sh+l.rh)
	for r := range l.sh {
		s := ""
		if r < len(pane) {
			s = pane[r]
		}
		if len(col) > 0 {
			s = fit(s, l.sw)
			for _, d := range col {
				t := ""
				if r < len(d) {
					t = d[r]
				}
				s += reset + fit(t, l.cw)
			}
		}
		out = append(out, s)
	}
	for i, a := range l.col {
		m.grid.at = append(m.grid.at, wallTile{key: a.Key, x: x + l.sw + i*l.cw, y: y, w: l.cw, h: l.ch})
		m.grid.colX = x + l.sw
	}
	row := draw(l.row, l.rw, l.rh)
	for r := range l.rh {
		var parts []string
		for _, d := range row {
			t := ""
			if r < len(d) {
				t = d[r]
			}
			parts = append(parts, fit(t, l.rw))
		}
		out = append(out, strings.Join(parts, reset))
	}
	for i, a := range l.row {
		m.grid.at = append(m.grid.at, wallTile{key: a.Key, x: x + i*l.rw, y: y + l.sh, w: l.rw, h: l.rh})
		m.grid.rowY = y + l.sh
	}
	return out
}

// gridClick swaps the agent of the tile clicked in, the one it replaces
// pinned in its place.
func (m *Model) gridClick(x, y int) (tea.Cmd, bool) {
	// The edge the tiles start at is a handle: held, it resizes them.
	// The column's edge, or the row's with no column.
	b := m.grid.box
	if g := m.grid; g.colX > 0 && x == g.colX && y >= b[1] && y < cmp.Or(g.rowY, b[1]+b[3]) ||
		g.colX == 0 && g.rowY > 0 && y == g.rowY && x >= b[0] && x < b[0]+b[2] {
		m.grid.drag = true
		return nil, true
	}
	m.grid.focus = ""
	for _, t := range m.grid.at {
		if x < t.x || x >= t.x+t.w || y < t.y || y >= t.y+t.h {
			continue
		}
		a := m.agentByKey(t.key)
		if a == nil {
			return nil, true
		}
		// A click on its box (its foot) gives the tile the keys, in place.
		if c := m.grid.conns[t.key]; c != nil && c.dockY > 0 && y-t.y >= c.dockY-m.paneTop {
			m.grid.focus = t.key
			return nil, true
		}
		if f := m.focused(); f != nil {
			for i, k := range m.grid.keys {
				if k == t.key {
					m.grid.keys[i] = f.Key
				}
			}
			if m.grid.under[t.key] != m.grid.under[f.Key] { // it takes the tile's place
				if m.grid.under == nil {
					m.grid.under = map[string]bool{}
				}
				m.grid.under[f.Key] = m.grid.under[t.key]
			}
		}
		m.sel = a.Key
		m.rebuild()
		m.gridSave()
		return m.focusPane(a), true
	}
	return nil, false
}

// gridItems are the grid's tiles as the Wall's reads want them.
func (m *Model) gridItems() []wallItem {
	var out []wallItem
	for _, a := range m.gridAgents() {
		out = append(out, wallItem{key: a.Key, a: a})
	}
	return out
}

// gridAdopt makes the tile's kept session for key the open one, the open
// one kept for its own tile when it's pinned; false when key has none.
func (m *Model) gridAdopt(key string) bool {
	t := m.grid.conns[key]
	if t == nil {
		return false
	}
	delete(m.grid.conns, key)
	if m.grid.focus == key {
		m.grid.focus = ""
	}
	if c := m.host; c != nil && slices.Contains(m.grid.keys, c.key) {
		m.grid.conns[c.key] = c
		m.host = nil
	} else {
		m.dropHost()
	}
	m.host, m.hostOpening = t, ""
	return true
}

// gridPark keeps the open session for its tile instead of closing it,
// when its agent is pinned and another is about to be opened.
func (m *Model) gridPark() {
	c := m.host
	if c == nil || !slices.Contains(m.grid.keys, c.key) {
		return
	}
	if m.grid.conns == nil {
		m.grid.conns = map[string]*hostConn{}
	}
	m.grid.conns[c.key], m.host = c, nil
}

// gridClose closes the kept sessions of tiles no longer pinned.
func (m *Model) gridClose() {
	for k, c := range m.grid.conns {
		if slices.Contains(m.grid.keys, k) {
			continue
		}
		c.unwatch()
		c.closed.Store(true)
		if c.client != nil {
			go c.client.Close() //nolint:errcheck // a socket let go: nothing waits on its close
		}
		delete(m.grid.conns, k)
	}
}

// withHost runs f as though c were the open session, then puts back the
// one that is.
func (m *Model) withHost(c *hostConn, f func()) {
	host, opening, sel, shown, focus := m.host, m.hostOpening, m.sel, m.shown, m.paneFocus
	m.host = c
	f()
	if m.host == nil || m.host == c {
		m.host = host
	}
	m.hostOpening, m.sel, m.shown, m.paneFocus = opening, sel, shown, focus
}

// gridSession draws a's tile as its Session, when its session is kept.
func (m *Model) gridSession(a *fleet.Agent, w, h int) []string {
	c := m.grid.conns[a.Key]
	if c == nil {
		return nil
	}
	var out []string
	m.withHost(c, func() {
		m.sel, m.shown, m.paneFocus = a.Key, a.Key, a.Key == m.grid.focus
		out = m.rushPane(w, h)
	})
	if len(out) > h {
		out = out[:h]
	}
	return out
}

// gridSave keeps the grid for the next start.
func (m *Model) gridSave() {
	var under []string
	for _, k := range m.grid.keys {
		if m.grid.under[k] {
			under = append(under, k)
		}
	}
	m.store.Config.Grid, m.store.Config.GridBelowKeys, m.store.Config.GridShare = slices.Clone(m.grid.keys), under, m.grid.share
	m.store.Config.GridBelow = false // GridBelowKeys says it now
	_ = m.store.SaveConfig()
}

// gridDrag moves the held edge to the pointer at (x, y): the tiles take
// what's past it, between a sixth of the pane and most of it.
func (m *Model) gridDrag(x, y int) {
	b := m.grid.box
	row := m.grid.colX == 0
	share := float64(b[0]+b[2]-x) / float64(max(1, b[2]))
	if row {
		share = float64(b[1]+b[3]-y) / float64(max(1, b[3]))
	}
	// The Session keeps the room it needs, and so do the tiles.
	lo, hi := float64(gridMinW)/float64(max(1, b[2])), float64(b[2]-60)/float64(max(1, b[2]))
	if row {
		lo, hi = float64(gridMinH)/float64(max(1, b[3])), float64(b[3]-12)/float64(max(1, b[3]))
	}
	m.grid.share = min(hi, max(lo, share))
}

// gridKey gives the focused tile the keys the Session would have, as
// though it were open; esc in its empty box hands them back.
func (m *Model) gridKey(k tea.KeyPressMsg, s string) (tea.Cmd, bool) {
	key := m.grid.focus
	c := m.grid.conns[key]
	if c == nil || !slices.Contains(m.grid.keys, key) {
		m.grid.focus = ""
		return nil, false
	}
	if s == "esc" && len(c.input) == 0 && c.sel == "" {
		m.grid.focus = ""
		return nil, true
	}
	var cmd tea.Cmd
	m.withHost(c, func() {
		m.sel, m.shown, m.paneFocus = key, key, true
		cmd = m.paneKey(k, s)
	})
	return cmd, true
}
