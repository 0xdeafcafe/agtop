package ui

import (
	tea "charm.land/bubbletea/v2"
)

// --- which layout ---

// viewSheet asks, the first time agtop opens, which layout to keep:
// Agents and the Session side by side, the Session alone, or Agents alone.
type viewSheet struct{ cur int }

var viewChoices = []struct{ key, what, about string }{
	{"split", "Side by side", "Agents, and the picked agent's Session beside them"},
	{"agent", "The Session alone", "one agent with the whole screen · esc for Agents"},
	{"list", "Agents alone", "every agent at a glance · tab opens one"},
}

func (m *Model) askView() {
	m.sheet = &viewSheet{}
}

func (v *viewSheet) width(*Model) int { return 76 }

func (v *viewSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Layout", "how should agtop open?", w), ""}
	for i, c := range viewChoices {
		out = append(out, sheetRow(paint(cText+bold, c.what)+"  "+dim(c.about), i == v.cur, w))
	}
	return append(out, "", dim("#view or Settings › General changes it · shift+← → too"), "",
		keysFit(w, "↑↓", "choose", "enter", "pick", "esc", "side by side"))
}

func (v *viewSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "up", "shift+tab":
		v.cur = pickerMove(v.cur, len(viewChoices), "up")
	case "down", "tab":
		v.cur = pickerMove(v.cur, len(viewChoices), "down")
	case "1", "2", "3":
		v.cur = int(s[0] - '1')
		return v.pick(m)
	case "enter":
		return v.pick(m)
	case "esc", "ctrl+c":
		v.cur = 0
		return v.pick(m)
	}
	return nil
}

// pick keeps the layout chosen and puts it on screen.
func (v *viewSheet) pick(m *Model) tea.Cmd {
	m.sheet = nil
	m.store.Config.SetView(viewChoices[v.cur].key)
	_ = m.store.SaveConfig()
	cmd := m.openView()
	m.askMenuBar()
	return cmd
}

// openView puts the kept layout on screen as agtop opens: the Session
// alone opens on the picked agent; the others lay out on their own.
func (m *Model) openView() tea.Cmd {
	m.full = false
	if m.store.Config.View == "agent" && m.selected() != nil {
		m.preview, m.full = true, true
		return m.loadPreview()
	}
	m.preview = false
	return nil
}
