package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Zen is only the agent that needs you: no header, no Session strip, no
// key hints, and the screen still exactly filled.
func TestZenHidesTheUI(t *testing.T) {
	m, _ := benchModel(160, 40)
	for _, a := range m.snap.Agents {
		a.State, a.Needs = "done", ""
	}
	m.snap.Agents[0].State, m.snap.Agents[0].Needs = "blocked", "which way?"
	m.setZen(true)
	frame := ansi.Strip(m.listView())
	if rows := strings.Count(frame, "\n") + 1; rows != m.h {
		t.Fatalf("zen should fill the screen exactly, got %d rows of %d", rows, m.h)
	}
	for _, ui := range []string{"Machine", "Settings", "ctrl+n", "ctrl+z", "find in chat", "back to the list", "views"} {
		if strings.Contains(frame, ui) {
			t.Errorf("zen should hide %q:\n%s", ui, frame)
		}
	}
	if !strings.Contains(frame, "needs you") || !strings.Contains(frame, "halfway through typing") {
		t.Fatalf("zen should still show the agent and its box:\n%s", frame)
	}

	for _, a := range m.snap.Agents {
		a.State, a.Needs = "done", ""
	}
	frame = ansi.Strip(m.listView())
	if !strings.Contains(frame, "nothing needs you") || strings.Contains(frame, "ctrl+") || strings.Contains(frame, "Machine") {
		t.Fatalf("zen with nothing waiting should only say so:\n%s", frame)
	}
}
