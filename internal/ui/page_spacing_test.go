package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestPageContentMovesUpWithoutMovingIcon(t *testing.T) {
	m, _ := benchModel(200, 50)
	icon := clanker(m.clkState(m.mood(m.tally()), m.tally()))
	header := m.header()
	if len(header) != clkH || strings.TrimSpace(ansi.Strip(header[0])) != "" || !strings.Contains(ansi.Strip(header[1]), "rush") || !strings.Contains(ansi.Strip(header[3]), "Agents") {
		t.Fatal("content must move one row up while keeping icon top gap")
	}
	for i := 0; i < clkH-1; i++ {
		got := ansi.Strip(ansi.Cut(header[i+1], 2, 2+clkW))
		if got != ansi.Strip(icon[i]) {
			t.Fatalf("icon row %d moved or changed: %q", i, got)
		}
	}
	if got := ansi.Strip(ansi.Cut(m.underHead()[0], 2, 2+clkW)); got != ansi.Strip(icon[clkH-1]) {
		t.Fatal("icon foot was clipped instead of retained")
	}
	if _, ok := m.clickTab(0, 0); ok {
		t.Fatal("padding is not navigation")
	}
}
