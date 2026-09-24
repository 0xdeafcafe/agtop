package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- what enter does ---

// enterSheet asks, the first time you press enter on an agent, whether
// enter should rename agents or open them, and does it.
type enterSheet struct {
	agent string
	cur   int // 0 rename, 1 open
}

var enterChoices = []struct{ key, what, about string }{
	{"rename", "Rename it", "as in the Finder · ⌘↓ or tab opens it"},
	{"open", "Open it", "into its Session · ctrl+r renames it"},
}

func (m *Model) askEnter(a *fleet.Agent) {
	m.sheet = &enterSheet{agent: a.Key}
}

func (e *enterSheet) width(*Model) int { return 64 }

func (e *enterSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Enter on an agent", "what should it do?", w), ""}
	for i, c := range enterChoices {
		out = append(out, sheetRow(paint(cText+bold, c.what)+"  "+dim(c.about), i == e.cur, w))
	}
	return append(out, "", dim("Settings › General changes it"), "",
		keysFit(w, "↑↓", "choose", "enter", "pick", "esc", "cancel"))
}

func (e *enterSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "shift+tab":
		e.cur = 0
	case "down", "tab":
		e.cur = 1
	case "enter":
		m.sheet = nil
		m.store.Config.EnterOn = enterChoices[e.cur].key
		_ = m.store.SaveConfig()
		a := m.agentByKey(e.agent)
		if a == nil {
			return nil
		}
		if e.cur == 1 {
			return m.focusPane(a)
		}
		m.startRename(a)
	}
	return nil
}
