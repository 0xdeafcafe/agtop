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

func TestHashPickerOverGettingStarted(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.onboard, m.paneFocus = true, false
	m.render()
	_, _, bodyH := m.layout()
	m.input = []rune("#")
	m.render() // the card was shown, so the picker goes over it
	frame := ansi.Strip(m.render())
	if _, _, got := m.layout(); got != bodyH {
		t.Fatalf("the list went from %d to %d rows with the picker over the card", bodyH, got)
	}
	if !strings.Contains(frame, "#done") || strings.Contains(frame, "Try zen") {
		t.Fatal("the # picker isn't over Getting started\n" + frame)
	}
	m.input = m.input[:0]
	if !strings.Contains(ansi.Strip(m.render()), "#tips off") {
		t.Fatal("Getting started doesn't say how # works\n" + ansi.Strip(m.render()))
	}
}

func TestRoundMove(t *testing.T) {
	for _, c := range []struct{ cur, d, n, want int }{
		{0, -1, 5, 4}, {4, 1, 5, 0}, {2, 1, 5, 3}, {0, -1, 0, 0}, {0, 1, 1, 0},
	} {
		if got := roundMove(c.cur, c.d, c.n); got != c.want {
			t.Errorf("roundMove(%d, %d, %d) = %d, want %d", c.cur, c.d, c.n, got, c.want)
		}
	}
}
