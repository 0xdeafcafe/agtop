package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// alt+f opens the Agents view's own filter: typing narrows the list to
// agents whose name matches, esc first clears what's typed then closes it,
// and backspace on an empty query closes it too.
func TestListFilterByName(t *testing.T) {
	m, _ := benchModel(160, 40)
	m.paneFocus, m.preview = false, false

	m.key(tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt})
	if m.listFilter == nil {
		t.Fatal("alt+f should open the filter")
	}
	for _, r := range "number 22" {
		m.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := string(m.listFilter.query); got != "number 22" {
		t.Fatalf("query = %q", got)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if got := string(m.listFilter.query); got != "number " {
		t.Fatalf("alt+backspace should drop the last word, query = %q", got)
	}
	for _, r := range "22" {
		m.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if len(m.order) != 1 || m.order[0].DisplayName != "agent number 22 doing things" {
		t.Fatalf("only the matching agent should be listed, got %v", m.order)
	}
	frame := ansi.Strip(m.listView())
	// Long names may shorten to preserve the harness badge.
	if !strings.Contains(frame, "agent number 22") || !strings.Contains(frame, "Claude Code") {
		t.Fatalf("the matching agent should still show:\n%s", frame)
	}
	if strings.Contains(frame, "agent number 3 doing things") {
		t.Fatalf("a non-matching agent shouldn't show:\n%s", frame)
	}

	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.listFilter == nil || len(m.listFilter.query) != 0 {
		t.Fatal("esc with something typed should clear it, not close the filter")
	}
	if len(m.order) != 30 {
		t.Fatalf("clearing the query should show every agent again, got %d", len(m.order))
	}

	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.listFilter != nil {
		t.Fatal("esc again, with nothing typed, should close the filter")
	}

	m.key(tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt})
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.listFilter == nil || len(m.listFilter.query) != 0 {
		t.Fatal("backspace should drop the last letter typed")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.listFilter != nil {
		t.Fatal("backspace on an empty query should close the filter")
	}
}
