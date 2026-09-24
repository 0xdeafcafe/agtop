package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// tab goes between the list and the Session; < and > between places, and
// tab through a place's pages; ctrl+z turns Zen on and off.
func TestPlacesAndFocus(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.host.input = nil
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	places := tea.KeyPressMsg{Code: '>', Text: ">"}
	back := tea.KeyPressMsg{Code: '<', Text: "<"}
	zen := tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
	if places.String() != ">" || back.String() != "<" || zen.String() != "ctrl+z" {
		t.Fatalf("keys print as %q, %q and %q", places.String(), back.String(), zen.String())
	}

	m.key(tab)
	if m.paneFocus || m.view != 0 {
		t.Fatalf("tab in the Session should give the list the keys, not change place (focus %v, view %d)", m.paneFocus, m.view)
	}
	m.key(tab)
	if !m.paneFocus || m.view != 0 {
		t.Fatal("tab in the list should go into the agent's Session")
	}

	m.key(places)
	if m.view != 1 || m.mode != modeProcs {
		t.Fatalf("> should go to Machine's Processes, got view %d mode %d", m.view, m.mode)
	}
	m.key(tab)
	if m.mode != modeCleanup {
		t.Fatal("tab in Machine should go to Cleanup")
	}
	m.key(tab)
	if m.mode != modeProcs {
		t.Fatal("tab past Cleanup should come back to Processes")
	}
	m.key(back)
	if m.view != 0 || m.mode != modeList {
		t.Fatal("< should come back to Agents")
	}
	m.key(back)
	if m.view != 2 || m.dialog == nil {
		t.Fatal("< from Agents should go round to Settings")
	}
	m.key(places)
	m.key(places)
	if m.view != 1 {
		t.Fatal("> from Settings should go round to Agents, then Machine")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.view != 0 || m.mode != modeList {
		t.Fatal("esc should come back to Agents")
	}
	m.input = nil
	m.key(tea.KeyPressMsg{Code: '.', Text: "."})
	if m.view != 1 {
		t.Fatal(". should go to Machine, as > does, without shift")
	}
	m.key(tea.KeyPressMsg{Code: ',', Text: ","})
	if m.view != 0 {
		t.Fatal(", should come back to Agents, as < does")
	}
	m.key(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.key(places)
	if m.view != 0 || string(m.host.input) != "a>" {
		t.Fatalf("> with something typed should be typed, got view %d box %q", m.view, string(m.host.input))
	}
	m.host.input = m.host.input[:0]

	m.key(zen)
	if !m.zen || !m.paneFocus || m.selected() == nil || !m.selected().NeedsYou() && !m.selected().Waiting() {
		t.Fatal("ctrl+z should show the agent that needs you, with the keys")
	}
	m.key(zen)
	if m.zen {
		t.Fatal("ctrl+z again should leave zen")
	}
	m.setZen(true)
	m.key(tab)
	if !m.zen || m.paneFocus || m.noPrompt() {
		t.Fatal("tab in zen should give the list the keys, and its Prompt back")
	}
	for _, a := range m.order {
		if a != nil && !a.NeedsYou() && !a.Waiting() {
			t.Fatalf("zen's list should hold only agents waiting on you, has %s", a.DisplayName)
		}
	}
	if len(m.order) == 0 {
		t.Fatal("zen's list is empty")
	}
	m.key(tab)
	if !m.zenFull() {
		t.Fatal("tab from zen's list goes back into the agent")
	}
	m.key(zen)
	if len(m.order) < 30 {
		t.Fatalf("leaving zen should bring every agent back, have %d rows", len(m.order))
	}
	m.setZen(true)
	m.key(places)
	if m.zen || m.view != 1 {
		t.Fatal("going to another place leaves zen")
	}
}
