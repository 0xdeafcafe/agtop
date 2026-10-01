package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"testing"
)

func TestPaneTabsHoverAndClickPreserveDraft(t *testing.T) {
	m, _ := benchModel(200, 55)
	c := m.host
	m.paneTop = m.topH()
	c.input = []rune("do not send this")
	m.rushPane(120, 45)
	if len(c.paneTabs) < 2 {
		t.Fatal("need visible view tabs")
	}
	tab := c.paneTabs[1]
	x, y := m.paneX()+tab.start+1, m.paneTop+paneTabsRow
	before := m.paneHeader(m.focused(), c, 120)[paneTabsRow]
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	after := m.paneHeader(m.focused(), c, 120)[paneTabsRow]
	if c.pointerHover.tab != 2 || c.view != 0 || before == after || ansi.Strip(before) != ansi.Strip(after) {
		t.Fatal("hover must show feedback without switching or moving text")
	}
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if c.view != 1 || string(c.input) != "do not send this" || len(c.sending) != 0 {
		t.Fatal("view click sent or changed draft")
	}
	m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	if c.pointerHover != (paneHover{}) {
		t.Fatal("pane hover stuck after leaving")
	}
}

func TestPaneHoverHighlightsWithoutChangingContent(t *testing.T) {
	m, _ := benchModel(200, 55)
	c := m.host
	m.paneTop = m.topH()
	rows := m.rushPane(120, 45)
	i := -1
	for j := c.bodyTop; j < len(c.rowRefs); j++ {
		if c.rowRefs[j] != "" {
			i = j
			break
		}
	}
	if i < 0 {
		t.Fatal("missing selectable row")
	}
	original := c.shown[c.rowBody[i]].Text
	selected := c.sel
	if !m.hoverPane(m.paneX()+4, m.paneTop+i) {
		t.Fatal("row did not hover")
	}
	hot := m.rushPane(120, 45)
	if hot[i] == rows[i] || ansi.Strip(hot[i]) != ansi.Strip(rows[i]) {
		t.Fatal("hover must change styling without changing text or geometry")
	}
	if c.sel != selected || c.shown[c.rowBody[i]].Text != original {
		t.Fatal("hover changed selection or transcript cache")
	}
	m.hoverPane(0, 0)
	cooled := m.rushPane(120, 45)
	if cooled[i] != rows[i] {
		t.Fatal("hover fill remained after pointer left")
	}
	m.hoverPane(m.paneX()+4, m.paneTop+i)
	if m.hoverPane(m.paneX()+5, m.paneTop+i) {
		t.Fatal("motion within row requested unnecessary redraw")
	}
}

func TestPaneTabTargetsExcludeClippedAndBlankSpace(t *testing.T) {
	m, _ := benchModel(200, 55)
	c := m.host
	m.paneHeader(m.focused(), c, 42)
	for _, tab := range c.paneTabs {
		if tab.start >= tab.end && m.paneTabAt(c, m.paneX()+tab.start, m.paneTop+paneTabsRow) >= 0 {
			t.Fatal("clipped tab is clickable")
		}
	}
	if m.paneTabAt(c, m.paneX(), m.paneTop) >= 0 {
		t.Fatal("header padding became tab target")
	}
}

func TestHeaderHoverUsesClickTargetsAndKeepsSelectedPage(t *testing.T) {
	m, _ := benchModel(200, 55)
	m.store.Config.HideLogo = true
	row := ansi.Strip(m.header()[2])
	x := -1
	for i := 0; i < len(row); i++ {
		h := m.headerTabAt(i, 2)
		if h.valid && h.index == placeSettings {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("settings target missing")
	}
	old := m.view
	if !m.hoverHeader(x, 2) || m.view != old {
		t.Fatal("hover changed view")
	}
	before := m.tabs()[placeSettings]
	m.hoverHeader(0, m.topH()+1)
	if before == m.tabs()[placeSettings] {
		t.Fatal("hover has no visual feedback")
	}
}
