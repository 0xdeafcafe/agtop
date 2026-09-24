package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestGettingStarted(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.onboard = true
	has := func() bool { return strings.Contains(ansi.Strip(m.render()), "Try zen") }
	if !has() {
		t.Fatal("Getting started isn't under the list")
	}
	for _, s := range steps {
		m.didStep(s.id)
		m.didStep(s.id) // twice counts once
	}
	if got := len(m.store.Config.Onboarding.Steps); got != len(steps) {
		t.Fatalf("%d steps recorded, want %d", got, len(steps))
	}
	if has() {
		t.Fatal("Getting started stays once every step is done")
	}
	if !strings.Contains(m.status, "all set") {
		t.Fatalf("finishing says %q", m.status)
	}
}

func TestTipsShowOnce(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.onboard = true
	m.noteProgress(true)
	first := m.status
	if first == "" {
		t.Fatal("no tip with agents needing you")
	}
	m.status = ""
	m.noteProgress(true)
	if m.status == first {
		t.Fatalf("the same tip showed twice: %q", first)
	}
}

func TestTour(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.onboard = true
	m.startTour()
	for i := range tourSteps {
		if !strings.Contains(ansi.Strip(m.render()), tourSteps[i].title) {
			t.Fatalf("stop %d doesn't say %q", i+1, tourSteps[i].title)
		}
		if i == 1 {
			m.tourKey("left")
			if m.tour != 1 {
				t.Fatal("← doesn't go back")
			}
			m.tourKey("enter")
		}
		m.tourKey("enter")
	}
	if m.tour != 0 || !m.store.Config.Onboarding.Toured {
		t.Fatal("the tour doesn't end, or doesn't remember it has shown")
	}
	m.startTour()
	m.tourKey("esc")
	if m.tour != 0 {
		t.Fatal("esc doesn't skip the tour")
	}
}
