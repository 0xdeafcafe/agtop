package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestParseSide(t *testing.T) {
	d, g, q := parseSide("Here are the notes:\nD: hosts own sessions\n- G: a second cache beside the store\nQ:\nq: who owns retries?\nD: a\nD: b\nD: c\nD: d\nD: e\nX: noise")
	if len(d) != 5 || d[0] != "hosts own sessions" || d[4] != "d" {
		t.Fatalf("decided %q", d)
	}
	if len(g) != 1 || g[0] != "a second cache beside the store" {
		t.Fatalf("grain %q", g)
	}
	if len(q) != 1 || q[0] != "who owns retries?" {
		t.Fatalf("open %q", q)
	}
	if _, g, _ := parseSide("G!: no auth on the new route"); len(g) != 1 || g[0] != "!no auth on the new route" {
		t.Fatalf("important %q", g)
	}
}

func sideModel(t *testing.T, w int) *Model {
	t.Helper()
	old := sideAsker
	t.Cleanup(func() { sideAsker = old })
	sideAsker = func(string) (string, string, error) {
		return "stub-sonnet", "D: keep the host as owner\nG!: polls instead of events\nQ: who retries?", nil
	}
	m, _ := benchModel(w, 50)
	return m
}

// Recall is off to start; ctrl+] z puts it at the foot of the list.
func TestRecallToggle(t *testing.T) {
	m := sideModel(t, 200)
	if strings.Contains(ansi.Strip(m.listView()), "◇ recall") {
		t.Fatal("on to start")
	}
	pressKeys(m, "ctrl+]", "z")
	if !m.side.on || !strings.Contains(ansi.Strip(m.listView()), "◇ recall") {
		t.Fatal("the toggle didn't show it")
	}
	if m.side.x >= m.paneX() {
		t.Fatalf("drawn at x %d, not in the list (pane at %d)", m.side.x, m.paneX())
	}
	pressKeys(m, "ctrl+]", "z")
	if m.side.on || strings.Contains(ansi.Strip(m.listView()), "◇ recall") {
		t.Fatal("the toggle didn't hide it")
	}
}

func TestRecallReadAndClick(t *testing.T) {
	m := sideModel(t, 200)
	m.side.on = true
	m.listView()
	cmd := m.sideTick()
	if cmd == nil {
		t.Fatal("no read")
	}
	runCmd(m, cmd)
	out := ansi.Strip(m.listView())
	for _, s := range []string{"stub-sonnet", "IMPORTANT", "RECAP", "polls instead of events", "who retries?"} {
		if !strings.Contains(out, s) {
			t.Fatalf("no %q in\n%s", s, out)
		}
	}
	if m.sideTick() != nil {
		t.Fatal("read again within the minute")
	}
	m.side.notes[m.host.key].at = time.Now().Add(-2 * time.Minute)
	if m.sideTick() != nil {
		t.Fatal("read again with nothing new")
	}
	row := -1
	for i, it := range m.side.items {
		if it == "polls instead of events" {
			row = i
		}
	}
	m.host.input = nil
	m.Update(tea.MouseClickMsg{X: m.side.x + 3, Y: m.side.y + row, Button: tea.MouseLeft})
	if got := string(m.host.input); got != "About: polls instead of events " {
		t.Fatalf("box %q", got)
	}
}

// runCmd runs cmd and what it leads to through m, batches included.
func runCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		_ = next
	}
}
