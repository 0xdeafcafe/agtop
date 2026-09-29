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

// hostedSnap is a snapshot of every agent as the view takes it; in embedded only
// the one session is in it, so nothing else is listed, counted or acted on.
func (m *Model) hostedSnap(snap *fleet.Snapshot) *fleet.Snapshot {
	if m.hosted == "" {
		return snap
	}
	m.fleetAgents = snap.Agents
	s := *snap
	s.Agents = nil
	for _, a := range snap.Agents {
		if a.Agtop && a.ID == m.hosted {
			s.Agents = append(s.Agents, a)
			m.hostedKey = a.Key
			break
		}
	}
	return &s
}

// pinHosted keeps the embedded view on its session, whatever a key or message
// did to the selection. Efficiency, Machine and Settings open as usual;
// Agents is the one session.
func (m *Model) pinHosted() {
	if m.hosted == "" {
		return
	}
	m.zen, m.peekFrom = false, ""
	if m.view != placeAgents {
		return
	}
	if m.hostedKey != "" {
		m.sel, m.shown = m.hostedKey, m.hostedKey
	}
	m.preview, m.full, m.paneFocus = true, true, true
}

// hostedKeyGuard drops the keys that would leave the one session in embedded:
// zen, the command bar, tab between list and Session, ctrl+n to the next
// agent, and , . < > between places, which stay text. ctrl+\ still goes
// to the next place. It says whether it took the key.
func (m *Model) hostedKeyGuard(s string) (tea.Cmd, bool) {
	if m.hosted == "" {
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
