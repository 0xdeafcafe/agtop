package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Frame renders one screen at a fixed size, for --render and tests.
func (m *Model) Frame(w, h int, keys ...tea.KeyPressMsg) string {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(scanMsg(m.scanner.Run(m.targets())))
	m.snap = m.loader.Load(true)
	time.Sleep(500 * time.Millisecond)
	m.refresh()
	for _, k := range keys {
		if cmd := m.key(k); cmd != nil {
			if msg := cmd(); msg != nil {
				if pm, ok := msg.(previewMsg); ok {
					m.previews[pm.key] = pm.e
				}
			}
		}
	}
	return m.render()
}
