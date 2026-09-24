package ui

import (
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// --- the menu bar icon ---

// menuBarSheet offers, once, on a Mac, to put agtop in the menu bar.
type menuBarSheet struct{ cur int }

var menuBarChoices = []struct{ what, about string }{
	{"Put it in the menu bar", "usage, what's working, and questions to answer from their notification"},
	{"Not now", "agtop notifies from the terminal as it does now"},
}

// askMenuBar asks, the first time agtop opens on a Mac (after the layout),
// whether to run the menu bar icon.
func (m *Model) askMenuBar() {
	c := &m.store.Config
	if runtime.GOOS != "darwin" || m.offline || c.MenuBar || c.MenuBarAsked || m.sheet != nil {
		return
	}
	m.sheet = &menuBarSheet{}
}

func (b *menuBarSheet) width(*Model) int { return 76 }

func (b *menuBarSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Menu bar", "keep agtop in the macOS menu bar?", w), ""}
	for i, c := range menuBarChoices {
		out = append(out, sheetRow(paint(cText+bold, c.what)+"  "+dim(c.about), i == b.cur, w))
	}
	return append(out, "", dim("Settings › General or agtop menubar [off] changes it"), "",
		keysFit(w, "↑↓", "choose", "enter", "pick", "esc", "not now"))
}

func (b *menuBarSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "up", "shift+tab", "down", "tab":
		b.cur = 1 - b.cur
	case "y":
		b.cur = 0
		return b.pick(m)
	case "n", "esc", "ctrl+c":
		b.cur = 1
		return b.pick(m)
	case "enter":
		return b.pick(m)
	}
	return nil
}

func (b *menuBarSheet) pick(m *Model) tea.Cmd {
	m.sheet = nil
	c := &m.store.Config
	c.MenuBarAsked, c.MenuBar = true, b.cur == 0
	_ = m.store.SaveConfig()
	if !c.MenuBar {
		return nil
	}
	m.flash("putting agtop in the menu bar…", false)
	return m.startMenuBar()
}
