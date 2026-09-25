package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// NewSolo is the view of one agtop-mode session alone, for embedding in
// another app: agtop's header, then its Session at the whole width, with no
// Agents list and no keys that lead to another agent. Efficiency, Machine
// and Settings open with ctrl+\, and Agents is this session. id is the
// session's agtop id. esc at the Session's top level and ctrl+q quit; the session's host
// keeps running.
func NewSolo(store *state.Store, version, id string) *Model {
	m := New(store, version)
	m.solo = id
	m.onboard = false
	m.snap = m.loadSnap()
	m.rebuild()
	m.pinSolo()
	return m
}

// loadSnap is a fresh snapshot of every agent; in solo, unless its list is
// shown, only the one session is in it, so nothing else is listed, counted
// or acted on.
func (m *Model) loadSnap() *fleet.Snapshot {
	snap := m.loader.Load(true)
	if m.solo == "" {
		return snap
	}
	m.fleetAgents = snap.Agents
	var mine *fleet.Agent
	for _, a := range snap.Agents {
		if a.Agtop && a.ID == m.solo {
			mine = a
			m.soloKey = a.Key
			break
		}
	}
	if m.soloList {
		return snap
	}
	s := *snap
	s.Agents = nil
	if mine != nil {
		s.Agents = append(s.Agents, mine)
	}
	return &s
}

// soloAlone is solo showing its one session, the list hidden.
func (m *Model) soloAlone() bool { return m.solo != "" && !m.soloList }

// listKey is the key that shows and hides the list beside a Session:
// ctrl+6, which terminals send as ctrl+^ unless they speak the kitty
// keyboard protocol.
func listKey(s string) bool { return s == "ctrl+6" || s == "ctrl+^" || s == "ctrl+shift+6" }

// toggleList shows or hides Agents beside the open Session. In solo the
// list comes with every agent, to pick and answer; hidden again, the view
// is back on solo's own session with solo's limits. Elsewhere it moves
// between the split and the Session alone, for now only: #view keeps the
// layout you chose.
func (m *Model) toggleList() tea.Cmd {
	if m.solo != "" {
		m.soloList = !m.soloList
		if m.soloList {
			m.full, m.preview, m.paneFocus = false, true, false
			m.flash("Agents beside the session · ctrl+6 or esc hides them", false)
		} else {
			m.bar, m.picker, m.inKind = nil, nil, inPrompt
		}
		m.refresh()
		m.pinSolo()
		return m.loadPreview()
	}
	if m.selected() == nil {
		return nil
	}
	if m.listW > 0 && m.chatOpen() {
		m.full, m.paneFocus = true, true
		return m.loadPreview()
	}
	m.full, m.peekFrom = false, ""
	if !m.chatOpen() {
		m.preview = true
	}
	if l, _ := m.widths(); l == 0 {
		// No room for both, or the Session alone is your layout: the list
		// takes the screen.
		m.leaveChat()
	}
	m.paneFocus = false
	return m.loadPreview()
}

// pinSolo keeps the solo view on its session, whatever a key or message
// did to the selection. Efficiency, Machine and Settings open as usual;
// Agents is the one session.
func (m *Model) pinSolo() {
	if m.solo == "" {
		return
	}
	m.zen, m.peekFrom = false, ""
	if m.view != placeAgents || m.soloList {
		return
	}
	if m.soloKey != "" {
		m.sel, m.shown = m.soloKey, m.soloKey
	}
	m.preview, m.full, m.paneFocus = true, true, true
}

// soloKeyGuard drops the keys that would leave the one session in solo:
// zen, the command bar, tab between list and Session, ctrl+n to the next
// agent, and , . < > between places, which stay text. ctrl+\ still goes
// to the next place. It says whether it took the key.
func (m *Model) soloKeyGuard(s string) (tea.Cmd, bool) {
	if m.solo == "" {
		return nil, false
	}
	if m.soloList {
		switch {
		case s == "ctrl+z":
			return nil, true
		case s == "esc" && !m.paneFocus && m.mode == modeList && m.dialog == nil && m.picker == nil:
			return m.toggleList(), true // esc from the list hides it
		}
		return nil, false
	}
	switch s {
	case "ctrl+q", "ctrl+\\":
		return nil, false
	case "ctrl+z", "ctrl+n":
		return nil, true
	}
	if m.placeStep(s) != 0 {
		return nil, true
	}
	if m.view != placeAgents {
		return nil, false // the place's own keys, tab through its pages included
	}
	if m.host == nil {
		// Still opening: nothing to type into yet but the way out.
		if s == "esc" {
			m.scanner.Flush()
			return tea.Quit, true
		}
		return nil, s != "ctrl+c"
	}
	if s == "tab" {
		if c := m.host; c != nil {
			if cmd, used := m.slashKey(c, s); used {
				return cmd, true
			}
		}
		return nil, true
	}
	return nil, false
}
