package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestHeaderStartsAtTheTop(t *testing.T) {
	m, _ := benchModel(200, 50)
	icon := clanker(m.clkState(m.mood(m.tally()), m.tally()))
	header := m.header()
	if len(header) != clkH-1 || !strings.Contains(ansi.Strip(header[0]), "rush") || !strings.Contains(ansi.Strip(header[2]), "Agents") {
		t.Fatal("the header starts on the top row, the tabs on its third")
	}
	for i := 0; i < clkH-1; i++ {
		got := ansi.Strip(ansi.Cut(header[i], 2, 2+clkW))
		if got != ansi.Strip(icon[i]) {
			t.Fatalf("icon row %d moved or changed: %q", i, got)
		}
	}
	if got := ansi.Strip(ansi.Cut(m.underHead()[0], 2, 2+clkW)); got != ansi.Strip(icon[clkH-1]) {
		t.Fatal("icon foot was clipped instead of retained")
	}
	if _, ok := m.clickTab(0, 0); ok {
		t.Fatal("the counts row is not navigation")
	}
}
