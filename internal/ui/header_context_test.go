package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/statusline"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestQuietHeaderContextAndDetailsTarget(t *testing.T) {
	m, _ := benchModel(220, 55)
	c := m.host
	m.bars.Agent = statusline.DefaultAgent()
	c.sess.Context, c.sess.Window = 148000, 200000
	c.input = []rune("unsent draft")
	m.paneTop = m.topH()
	rows := m.paneHeader(m.focused(), c, m.w-m.paneX())
	text := ansi.Strip(rows[paneTitleRow])
	if !strings.Contains(text, "context 74%") || strings.Contains(text, "148k/") || strings.Contains(text, "━") {
		t.Fatalf("cluttered usage: %s", text)
	}
	x, y := m.paneX()+c.contextLabel[0], m.paneTop+paneTitleRow
	if !m.contextAt(c, x, y) || m.contextAt(c, x, y+1) {
		t.Fatal("context hit target differs from its display")
	}
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if !c.pointerHover.context || string(c.input) != "unsent draft" {
		t.Fatalf("context hover=%v draft=%q x=%d width=%d", c.pointerHover.context, string(c.input), x, m.w)
	}
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if _, ok := m.sheet.(*infoSheet); !ok || string(c.input) != "unsent draft" || len(c.sending) != 0 {
		t.Fatal("context click must open details without sending")
	}
}

func TestQuietHeaderDefaultsKeepDetailsOptional(t *testing.T) {
	d := statusline.DefaultAgent()
	for _, name := range []string{"session", "mode", "cache", "tmp", "context-detail"} {
		if d.Shown(name) {
			t.Fatalf("routine detail %s crowds header", name)
		}
	}
	for _, name := range []string{"model", "effort", "context", "billing", "folder", "branch"} {
		if !d.Shown(name) {
			t.Fatalf("missing %s", name)
		}
	}
	m, a, c := barAgentFixture(t)
	m.bars.Agent = d
	c.sleeping = true
	c.client = nil
	rows := ansi.Strip(strings.Join(m.paneHeader(a, c, 180), "\n"))
	if strings.Contains(rows, "stopped") || strings.Contains(rows, "send to resume") {
		t.Fatalf("sleep reported as failure: %s", rows)
	}
	c.sess.Context = 0
	if headerContext(c) != "" {
		t.Fatal("missing measurement shown as zero usage")
	}
}
