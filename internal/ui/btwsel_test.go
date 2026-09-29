package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Text in the /btw panel can be dragged over and copied, as in the chat: on
// release, or with cmd+c when copy on select is off.
func TestBtwTextSelects(t *testing.T) {
	m, c := infoModel(t)
	off := false
	m.store.Config.CopyOnSelect = &off
	m.btws = map[string]*btwThread{c.key: {focused: true, qa: []btwQA{{Question: "ssh command please", Response: "Run ssh root@example.test to get in."}}}}
	bt := m.btwFor(c.key)
	draw := func() (int, int) {
		rows := make([]string, 30)
		for i := range rows {
			rows[i] = strings.Repeat("c", 100)
		}
		m.btwOverlay(c, rows, 2, 26, 100)
		for i, r := range rows {
			if p := ansi.Strip(r); strings.Contains(p, "ssh root@") {
				return ansi.StringWidth(p[:strings.Index(p, "ssh root@")]) + m.paneX(), i + m.paneTop
			}
		}
		t.Fatalf("answer not drawn:\n%s", ansi.Strip(strings.Join(rows, "\n")))
		return 0, 0
	}
	x, y := draw()
	if !m.clickBtw(c, x, y) {
		t.Fatal("the click missed the panel")
	}
	m.update(tea.MouseMotionMsg{X: x + len("ssh root@example.test") - 1, Y: y, Button: tea.MouseLeft})
	m.update(tea.MouseReleaseMsg{X: x + len("ssh root@example.test") - 1, Y: y, Button: tea.MouseLeft})
	if m.pendingCopy != "" || !bt.sel.on || bt.sel.drag {
		t.Fatalf("release copied %q or dropped the selection", m.pendingCopy)
	}
	if !strings.Contains(strings.Join(bt.lines(c, bt.at[2], 26, true), ""), selBlue) {
		t.Fatal("the selection isn't painted")
	}
	m.paneKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper}, "super+c")
	if m.pendingCopy != "ssh root@example.test" {
		t.Fatalf("cmd+c copied %q", m.pendingCopy)
	}

	// On, the release copies; a click outside the panel drops it.
	m.store.Config.CopyOnSelect, m.pendingCopy = nil, ""
	m.clickBtw(c, x, y)
	bt.drag(x+2, y)
	m.endBtwDrag(bt)
	if m.pendingCopy != "ssh" {
		t.Fatalf("release copied %q", m.pendingCopy)
	}
	m.clickBtw(c, 0, y)
	if bt.sel.on {
		t.Fatal("a click elsewhere kept the selection")
	}
}
