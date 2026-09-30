package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// { } go between the list and the Session; < and > between places, [ ]
// through a place's own pages; ctrl+z turns Zen on and off.
func TestPlacesAndFocus(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.host.input = nil
	places := tea.KeyPressMsg{Code: '>', Text: ">"}
	back := tea.KeyPressMsg{Code: '<', Text: "<"}
	zen := tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
	next := tea.KeyPressMsg{Code: ']', Text: "]"}
	brace := tea.KeyPressMsg{Code: '}', Text: "}"}
	if places.String() != ">" || back.String() != "<" || zen.String() != "ctrl+z" {
		t.Fatalf("keys print as %q, %q and %q", places.String(), back.String(), zen.String())
	}

	m.key(brace)
	if m.paneFocus || m.view != 0 {
		t.Fatalf("} in the Session should give the list the keys, not change place (focus %v, view %d)", m.paneFocus, m.view)
	}
	m.key(brace)
	if !m.paneFocus || m.view != 0 {
		t.Fatal("} in the list should go into the agent's Session")
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
	if m.view != placeProjects {
		t.Fatalf("ctrl+\\ from the Session's box: view %d", m.view)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.view != placeAgents || m.mode != modeList {
		t.Fatalf("esc from Projects should come back to Agents: view %d mode %d", m.view, m.mode)
	}
	m.key(brace) // the list has the keys
	if m.paneFocus {
		t.Fatal("} should give the list the keys")
	}

	m.key(next)
	if m.mode != modeWall || m.work.page != agentsWall {
		t.Fatalf("] in Agents should go to the Wall: mode %d page %d", m.mode, m.work.page)
	}
	m.key(next)
	if m.mode != modeList || m.work.page != agentsList {
		t.Fatal("] past the Wall should come back to the plain Agents list")
	}

	// Projects is a place of its own, one page with no pages row.
	m.key(places)
	if m.view != placeProjects || m.mode != modeProjects {
		t.Fatalf("> from Agents should go to Projects: view %d mode %d", m.view, m.mode)
	}
	if p := ansi.Strip(m.pages()); strings.Contains(p, "Worktrees") || strings.Contains(p, "Wall") {
		t.Fatalf("Projects has pages: %q", p)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.view != placeAgents || m.mode != modeList {
		t.Fatal("esc from Projects should come back to the plain Agents list")
	}

	m.key(places)
	m.key(places)
	if m.view != placeEff || m.mode != modeEff || m.eff.page != effOverview {
		t.Fatalf("> should go to Efficiency's Overview, got view %d mode %d", m.view, m.mode)
	}
	m.key(next)
	if m.eff.page != effTimeline {
		t.Fatal("] in Efficiency should go to its Timeline")
	}
	m.key(places)
	if m.view != placeSettings || m.dialog == nil {
		t.Fatalf("> from Efficiency should go to Settings, got view %d", m.view)
	}
	m.key(back)
	m.key(back)
	m.key(back)
	if m.view != placeAgents || m.mode != modeList {
		t.Fatal("< < < from Settings should come back to Agents")
	}
	m.key(back)
	if m.view != placeSettings || m.dialog == nil {
		t.Fatal("< from Agents should go round to Settings")
	}
	m.key(places)
	m.key(places)
	m.key(places)
	if m.view != placeEff || m.eff.page != effTimeline {
		t.Fatal("> > > from Settings should go round through Agents and Projects to Efficiency, on the page it was on")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.view != placeAgents || m.mode != modeList {
		t.Fatal("esc should come back to Agents")
	}
	m.input = nil
	m.key(tea.KeyPressMsg{Code: '.', Text: "."})
	if m.view != placeProjects {
		t.Fatal(". should go to Projects, as > does, without shift")
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
	if m.zen || m.view != placeProjects {
		t.Fatal("going to another place leaves zen")
	}
}
