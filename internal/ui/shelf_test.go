package ui

import (
	"testing"
)

func TestShelfPicksAndReturns(t *testing.T) {
	m := footModel(t, 3)
	m.snap.Agents[1].Kind, m.snap.Agents[1].Spend.Model = "claude", "claude-haiku-4-5"
	m.host = nil
	tiles := m.shelf(m.startDir())
	if len(tiles) < 2 {
		t.Fatalf("shelf = %+v, want the profile's setup and a recent one", tiles)
	}
	m.shelfKey("alt+2")
	if m.startOver == nil || m.setupKey(*m.startOver) != m.setupKey(tiles[1]) {
		t.Fatalf("⌥2 didn't take the second setup: %+v", m.startOver)
	}
	if got := m.shelf(m.startDir()); m.setupKey(got[1]) != m.setupKey(tiles[1]) {
		t.Fatal("picking a setup moved it on the shelf")
	}
	m.shelfKey("alt+1")
	if m.startOver != nil {
		t.Fatalf("⌥1 kept a one-off instead of the profile's own: %+v", m.startOver)
	}
	if m.shelfKey("alt+x") || !m.shelfKey("alt+9") {
		t.Fatal("only ⌥1…9 are the shelf's")
	}
}

func TestShelfClickOnBorder(t *testing.T) {
	m, _ := benchModel(160, 50)
	m.snap.Agents[1].Kind, m.snap.Agents[1].Spend.Model = "claude", "claude-haiku-4-5"
	m.host, m.preview = nil, false // the Prompt full width
	m.rebuild()
	m.View()
	if len(m.shelfHits) < 2 {
		t.Fatalf("shelf hits = %+v, box %d wide", m.shelfHits, m.promptBox.w)
	}
	h := m.shelfHits[1]
	if !m.shelfClick(h.x0, m.promptBoxY) || m.startOver == nil {
		t.Fatal("a click on a setup didn't take it")
	}
	if m.shelfClick(h.x0, m.promptBoxY+1) {
		t.Fatal("a click off the border took a setup")
	}
}
