package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// NewHosted is the view of one agtop-mode session alone, for embedding in
// another app: agtop's header, then its Session at the whole width, with no
// Agents list and no keys that lead to another agent. Efficiency, Machine
// and Settings open with ctrl+\, and Agents is this session. id is the
// session's agtop id. esc at the Session's top level and ctrl+q quit; the session's host
// keeps running.
func NewHosted(store *state.Store, version, id string) *Model {
	m := New(store, version)
	m.hosted = id
	m.onboard = false
	m.snap = m.hostedSnap(m.loader.Load(true))
	m.rebuild()
	m.pinHosted()
	return m
}

// hostedSnap is a snapshot of every agent as the view takes it; in hosted,
// unless its list is shown (ctrl+6), only the one session is in it, so
// nothing else is listed, counted or acted on.
func (m *Model) hostedSnap(snap *fleet.Snapshot) *fleet.Snapshot {
	if m.hosted == "" {
		return snap
	}
	m.fleetAgents = snap.Agents
	var mine *fleet.Agent
	for _, a := range snap.Agents {
		if a.Agtop && a.ID == m.hosted {
			mine = a
			m.hostedKey = a.Key
			break
		}
	}
	if m.hostedList {
		return snap
	}
	s := *snap
	s.Agents = nil
	if mine != nil {
		s.Agents = append(s.Agents, mine)
	}
	return &s
}

// hostedAlone is hosted showing its one session, the list hidden.
func (m *Model) hostedAlone() bool { return m.hosted != "" && !m.hostedList }

// listToggleKey is the key that shows and hides Agents beside a Session:
// ctrl+6, which terminals send as ctrl+^ unless they speak the kitty
// keyboard protocol.
func listToggleKey(s string) bool { return s == "ctrl+6" || s == "ctrl+^" || s == "ctrl+shift+6" }

// allAgents is every agent, hosted's hidden ones included: what the
// command bar searches and goes to.
func (m *Model) allAgents() []*fleet.Agent {
	if m.hosted != "" && m.fleetAgents != nil {
		return m.fleetAgents
	}
	return m.snap.Agents
}

// anyAgent is an agent by key among allAgents.
func (m *Model) anyAgent(key string) *fleet.Agent {
	for _, a := range m.allAgents() {
		if a.Key == key {
			return a
		}
	}
	return nil
}

// hostedShow is hosted going to another agent than its own, from the
// command bar: the list comes beside it, as ctrl+6 shows it, and ctrl+6
// or esc from the list is the way back.
func (m *Model) hostedShow(key string) {
	if m.hosted == "" || m.hostedList || key == m.hostedKey {
		return
	}
	m.hostedList = true
	m.full, m.preview = false, true
	s := *m.snap
	s.Agents = m.fleetAgents
	m.snap = m.hostedSnap(&s)
	m.rebuild()
}

// toggleList shows or hides Agents beside the open Session. In hosted the
// list comes with every agent, to pick and answer, and the plugin
// group-by modes; hidden again, the view is back on hosted's own session
// with hosted's limits. Elsewhere it moves between the split and the
// Session alone, without changing the layout #view keeps.
func (m *Model) toggleList() tea.Cmd {
	if m.hosted != "" {
		m.hostedList = !m.hostedList
		if m.hostedList {
			m.full, m.preview, m.paneFocus = false, true, false
			m.flash("Agents beside the session · ctrl+6 or esc hides them", false)
		} else {
			m.bar, m.picker, m.inKind = nil, nil, inPrompt
		}
		// The full fleet is already cached (m.fleetAgents): narrowing or
		// widening the list is a filter of it, not a fresh disk read.
		s := *m.snap
		s.Agents = m.fleetAgents
		m.snap = m.hostedSnap(&s)
		m.pinHosted()
		m.rebuild()
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

// pinHosted keeps the hosted view on its session, whatever a key or message
// did to the selection. Efficiency, Machine and Settings open as usual;
// Agents is the one session, unless its list is shown.
func (m *Model) pinHosted() {
	if m.hosted == "" {
		return
	}
	m.zen, m.peekFrom = false, ""
	if m.view != placeAgents || m.hostedList {
		return
	}
	if m.hostedKey != "" {
		m.sel, m.shown = m.hostedKey, m.hostedKey
	}
	m.preview, m.full, m.paneFocus = true, true, true
}

// hostedKeyGuard drops the keys that would leave the one session in hosted:
// zen, the command bar, tab between list and Session, ctrl+n to the next
// agent, and , . < > between places, which stay text. ctrl+\ still goes
// to the next place, and ctrl+6 shows or hides Agents beside the session.
// It says whether it took the key.
func (m *Model) hostedKeyGuard(s string) (tea.Cmd, bool) {
	if m.hosted == "" || m.bar != nil {
		return nil, false // the command bar has the keys it needs
	}
	if m.hostedList {
		switch {
		case s == "ctrl+z":
			return nil, true
		case s == "esc" && !m.paneFocus && m.mode == modeList && m.dialog == nil && m.picker == nil:
			return m.toggleList(), true // esc from the list hides it
		}
		return nil, false
	}
	switch s {
	case "ctrl+q", "ctrl+\\", "ctrl+k", "super+k":
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
