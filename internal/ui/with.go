package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// withAgent is #with: what the next session starts as, once, as #new
// without a task (newCommand); alone, it says what that is. It never
// changes a default: Settings › Providers does.
func (m *Model) withAgent(arg string) tea.Cmd {
	if strings.TrimSpace(arg) != "" {
		return m.newCommand(arg)
	}
	m.flash("the next session starts as "+m.startWith(m.startDir(), true)+" · #with harness@provider picks another, once", false)
	return nil
}

// modelWord is model id as the agent of kind names it: "Opus 5.5".
func modelWord(kind, id string) string {
	k := agent.Kind(kind)
	if k == "" {
		k = agent.LegacyKind
	}
	return agent.ModelName(k, id)
}

// agentName is what runs sessions of kind, as rush names it:
// Provider (Harness), OpenAI (Codex).
func agentName(kind string) string { return agent.Label(agent.Kind(kind)) }

// harnessName is the harness sessions of kind run in: Claude Code, Codex.
// What a program does itself (reads memory, draws screens) says it.
func harnessName(kind string) string { return agent.HarnessLabel(agent.Kind(kind)) }
