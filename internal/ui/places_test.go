package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// tab goes between the list and the Session; < and > between places, and
// [ ] through a place's pages; ctrl+z turns Zen on and off.
func TestPlacesAndFocus(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.host.input = nil
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	places := tea.KeyPressMsg{Code: '>', Text: ">"}
	back := tea.KeyPressMsg{Code: '<', Text: "<"}
	zen := tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
	next := tea.KeyPressMsg{Code: ']', Text: "]"}
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
	// The Session's box types them, empty or not: a message may start
	// with a > quote. ctrl+\ still moves.
	for _, k := range []tea.KeyPressMsg{places, back, {Code: ',', Text: ","}, {Code: '.', Text: "."}} {
		m.key(k)
	}
	if m.view != 0 || string(m.host.input) != "><,." {
		t.Fatalf("in the Session's box: view %d, box %q", m.view, string(m.host.input))
	}
	m.host.input = m.host.input[:0]
	m.key(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl})
	if m.view != placeWork {
		t.Fatalf("ctrl+\\ from the Session's box: view %d", m.view)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.key(tab) // the list has the keys
	if m.paneFocus {
		t.Fatal("tab should give the list the keys")
	}

	m.key(places)
	if m.view != placeWork || m.mode != modeWork || m.work.page != workNowPage {
		t.Fatalf("> should go to the Overview's Now, got view %d mode %d", m.view, m.mode)
	}
	m.key(next)
	if m.mode != modeWork || m.work.page != workProjects {
		t.Fatal("] in the Overview should go to Projects")
	}
	m.key(next)
	if m.mode != modeWall || m.work.page != workWall {
		t.Fatal("] past Projects should go to the Wall")
	}
	m.key(next)
	if m.mode != modeWork || m.work.page != workNowPage {
		t.Fatal("] past the Wall should come back to Now")
	}
	m.key(places)
	if m.view != placeEff || m.mode != modeEff || m.eff.page != effOverview {
		t.Fatalf("> should go to Efficiency's Overview, got view %d mode %d", m.view, m.mode)
	}
	m.key(next)
	if m.eff.page != effTimeline {
		t.Fatal("] in Efficiency should go to its Timeline")
	}
	m.key(places)
	if m.view != placeMachine || m.mode != modeProcs {
		t.Fatalf("> from Efficiency should go to Machine's Processes, got view %d mode %d", m.view, m.mode)
	}
	m.key(next)
	if m.mode != modeCleanup {
		t.Fatal("] in Machine should go to Cleanup")
	}
	m.key(next)
	if m.mode != modeProcs {
		t.Fatal("] past Cleanup should come back to Processes")
	}
	m.key(back)
	m.key(back)
	m.key(back)
	if m.view != placeAgents || m.mode != modeList {
		t.Fatal("< < < should come back to Agents")
	}
	m.key(back)
	if m.view != placeSettings || m.dialog == nil {
		t.Fatal("< from Agents should go round to Settings")
	}
	m.key(places)
	m.key(places)
	m.key(places)
	if m.view != placeEff || m.eff.page != effTimeline {
		t.Fatal("> from Settings should go round to Agents, the Overview, then Efficiency on the page it was on")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.view != placeAgents || m.mode != modeList {
		t.Fatal("esc should come back to Agents")
	}
	m.input = nil
	m.key(tea.KeyPressMsg{Code: '.', Text: "."})
	if m.view != placeWork {
		t.Fatal(". should go to the Overview, as > does, without shift")
	}
	m.key(tea.KeyPressMsg{Code: ',', Text: ","})
	if m.view != placeAgents {
		t.Fatal(", should come back to Agents, as < does")
	}
	m.key(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.key(places)
	if m.view != 0 || string(m.input) != "a>" {
		t.Fatalf("> with something typed should be typed, got view %d box %q", m.view, string(m.input))
	}
	m.input = m.input[:0]

	m.key(zen)
	if !m.zen || !m.paneFocus || m.selected() == nil || !m.selected().NeedsYou() && !m.selected().Waiting() {
		t.Fatal("ctrl+z should show the agent that needs you, with the keys")
	}
	m.key(zen)
	if m.zen {
		t.Fatal("ctrl+z again should leave zen")
	}
	m.setZen(true)
	m.key(zen)
	if len(m.order) < 30 {
		t.Fatalf("leaving zen should bring every agent back, have %d rows", len(m.order))
	}
	m.setZen(true)
	m.key(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}) // zen's box has the keys: > is text there
	if m.zen || m.view != placeWork {
		t.Fatal("going to another place leaves zen")
	}
}
