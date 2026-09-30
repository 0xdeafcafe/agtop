package ui

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Frame renders one screen at a fixed size, for --render and tests.
func (m *Model) Frame(w, h int, keys ...tea.KeyPressMsg) string {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.Update(scanMsg(m.scanner.Run(m.targets())))
	m.refreshNow()
	time.Sleep(500 * time.Millisecond)
	m.refreshNow()
	m.drain(m.refreshFolders())
	// RUSH_RENDER_SELECT picks an agent by name, for checking one pane.
	if want := os.Getenv("RUSH_RENDER_SELECT"); want != "" {
		for _, a := range m.order {
			if strings.Contains(strings.ToLower(a.DisplayName), strings.ToLower(want)) {
				m.sel = a.Key
				m.preview = true
				break
			}
		}
		// Let the pane connect and take in the replay. Frame drops the
		// commands Update returns, so forget any open it already started.
		m.hostOpening = ""
		for i := 0; i < 20; i++ {
			m.drain(m.syncHost())
			if m.host != nil && m.host.ready {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	for _, k := range keys {
		if cmd := m.key(k); cmd != nil {
			if msg := cmd(); msg != nil {
				if pm, ok := msg.(previewMsg); ok {
					m.previews[pm.key] = pm.e
				}
				if sm, ok := msg.(sheetMsg); ok {
					sm.apply(m) // a sheet's first read, e.g. /plugins' lists
				}
				if fm, ok := msg.(foldersMsg); ok {
					m.onFolders(fm) // what git says, for the Projects page
				}
				if bm, ok := msg.(tea.BatchMsg); ok {
					for _, c := range bm {
						m.drain(c) // what git says, a project at a time
					}
				}
			}
		}
	}
	return m.render()
}

// Offline keeps the view from asking Anthropic for plan usage; it shows
// the readings other rushes made. For --soak, run many times in a row.
func (m *Model) Offline() { m.offline = true }

// Select picks the first agent whose name contains part and shows its
// Session, for --soak.
func (m *Model) Select(part string) {
	m.refreshNow()
	for _, a := range m.order {
		if strings.Contains(strings.ToLower(a.DisplayName), strings.ToLower(part)) {
			m.sel, m.preview = a.Key, true
			return
		}
	}
}

// drain runs a command and feeds what it returns back in, for Frame.
func (m *Model) drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case hostOpenMsg:
		m.drain(m.onHostOpen(msg))
	case hostLinesMsg:
		m.onHostLines(msg)
	case foldersMsg:
		m.onFolders(msg)
	case tea.BatchMsg:
		for _, c := range msg {
			m.drain(c)
		}
	case paneMsg:
		m.drain(m.onPane(msg))
		if c := m.host; c != nil && c.paneKick { // as Update follows it
			c.paneKick = false
			m.drain(m.refreshSubs())
		}
	case wholeMsg:
		m.onWhole(msg)
	case preMsg:
		m.drain(m.onPre(msg))
	case replayMsg:
		m.drain(m.onReplay(msg))
	case applyMsg:
		m.drain(msg.applyTo(m))
	}
}
