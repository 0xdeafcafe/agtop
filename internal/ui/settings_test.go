package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// [ and ] go through Settings' pages, round the ends; tab doesn't.
func TestSettingsPagesBrackets(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.setView(placeSettings)
	m.setSettingsPage(pageOverview)
	n := len(m.settingsPages())
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.dialog.page != pageProviders {
		t.Fatalf("] went to page %d, not Providers", m.dialog.page)
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.dialog.page != n-1 {
		t.Fatalf("[ from Overview went to page %d, not the last (%d)", m.dialog.page, n-1)
	}
	m.setSettingsPage(pageSessions)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.dialog == nil || m.dialog.page != pageSessions {
		t.Fatal("tab changed Settings' page")
	}
	if !strings.Contains(ansi.Strip(m.pages()), "[ ]") {
		t.Fatalf("the page strip doesn't say [ ]: %q", ansi.Strip(m.pages()))
	}
}

// Every page draws, and every form row explains itself.
func TestSettingsPagesDraw(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.setView(placeSettings)
	for i, p := range m.settingsPages() {
		m.setSettingsPage(i)
		if body := m.dialogBody(130); len(body) < 3 {
			t.Fatalf("%s drew %d lines", p.name, len(body))
		}
		if p.form == nil {
			continue
		}
		for _, st := range flat(p.form(m)) {
			if st.line == nil && st.what == "" && st.about == nil {
				t.Errorf("%s › %s doesn't say what it does", p.name, st.label)
			}
		}
	}
}

// An agent other than the built-in one keeps what its sessions start with
// apart from Claude Code's.
func TestStartForKind(t *testing.T) {
	var d state.Dispatch
	d.SetStartFor("claude", state.Start{Model: "opus", Mode: "plan"})
	d.SetStartFor("codex", state.Start{Effort: "high", Mode: "auto"})
	if d.Model != "opus" || d.Permission != "plan" {
		t.Fatalf("Claude's start isn't in Model and Permission: %+v", d)
	}
	if got := d.StartFor("codex"); got.Effort != "high" || got.Mode != "auto" || got.Model != "" {
		t.Fatalf("codex start = %+v", got)
	}
	if got := d.StartFor(""); got.Model != "opus" {
		t.Fatalf("an empty kind is Claude Code's: %+v", got)
	}
	d.SetStartFor("codex", state.Start{})
	if _, ok := d.Starts["codex"]; ok {
		t.Fatal("an emptied start is kept")
	}
}

// Agents is one page whatever is installed; 1-9 pick the agent it shows.
func TestSettingsAgentsOnePage(t *testing.T) {
	m, _ := accountsModel(t)
	m.setView(placeSettings)
	if n := len(m.settingsPages()); n != pageInterface+1 {
		t.Fatalf("%d pages: an agent has a page of its own again", n)
	}
	order := m.agentOrder()
	if len(order) < 2 {
		t.Skip("needs two agents")
	}
	m.setSettingsPage(pageAgents)
	if m.settingsAgent() != order[0].Kind() {
		t.Fatalf("Agents opens on %s, not the first", m.settingsAgent())
	}
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.settingsAgent() != order[1].Kind() {
		t.Fatalf("2 shows %s, not %s", m.settingsAgent(), order[1].Kind())
	}
	m.openAgentSettings(order[0].Kind())
	if m.dialog.page != pageAgents || m.settingsAgent() != order[0].Kind() {
		t.Fatal("openAgentSettings didn't land on the agent")
	}
}
