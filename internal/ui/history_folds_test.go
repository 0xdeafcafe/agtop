package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/convo"
)

func TestHistoryBulkControlsKeepDraftAndToolChoices(t *testing.T) {
	m, c := draftModel()
	c.sess.Turns = []*convo.Turn{{N: 1, Prompt: "earlier"}, {N: 2, Prompt: "latest", Live: true}}
	c.open = map[string]bool{"t1": false, "t2": false, "t1:s:tool": true}
	c.sel, c.input, c.scrollOnly = "t1:s:tool", []rune("keep my draft"), true
	m.runAction("session.history.open")
	if c.historyMode != convo.HistoryOpen || c.sel != "t1:s:tool" || c.selMoved || c.scrollOnly || !c.open["t1:s:tool"] || string(c.input) != "keep my draft" {
		t.Fatal("bulk open changed tool choices, draft or navigation")
	}
	if _, ok := c.open["t2"]; ok {
		t.Fatal("latest turn retained its old closed override")
	}
	m.command(nil, "#collapse")
	if c.historyMode != convo.HistoryCompact || !m.isOpen(c, "t2") || m.isOpen(c, "t1") {
		t.Fatal("collapse must keep latest open and older turns previewed")
	}
	m.command(nil, "#expand")
	if !m.isOpen(c, "t1") {
		t.Fatal("expand command did not open older turns")
	}
	// History fetched after the action inherits the mode too.
	whole := convo.New()
	whole.Turns = []*convo.Turn{{N: 1, Prompt: "loaded later"}, {N: 2, Prompt: "earlier"}, {N: 3, Prompt: "latest", Live: true}}
	m.takeWhole(c, whole)
	if !m.isOpen(c, "t1") {
		t.Fatal("late-loaded turns lost open-all mode")
	}
}

func TestHistoryBulkKeyBindings(t *testing.T) {
	m, _ := benchModel(200, 50)
	if !m.paneFocus {
		pressKeys(m, "}")
	}
	m.host.input = []rune("unfinished message")
	pressKeys(m, "ctrl+]", "e")
	if m.host.historyMode != convo.HistoryOpen {
		t.Fatal("open-all chord was not dispatched")
	}
	pressKeys(m, "ctrl+]", "s")
	if m.host.historyMode != convo.HistoryCompact || string(m.host.input) != "unfinished message" {
		t.Fatalf("close-older: mode=%v draft=%q status=%q focus=%v", m.host.historyMode, string(m.host.input), m.status, m.paneFocus)
	}
}

func TestHistoryHeaderChipsClickOnce(t *testing.T) {
	m, _ := benchModel(200, 50)
	c := m.host
	m.View()
	_, paneW, _ := m.layout()
	head := m.paneHeader(m.focused(), c, paneW)
	if row := ansi.Strip(head[paneTabsRow]); !strings.Contains(row, "open all") || !strings.Contains(row, "collapse older") {
		t.Fatalf("header lacks the history control: %q", row)
	}
	if len(c.histTabs) != 2 {
		t.Fatalf("no click targets: %v", c.histTabs)
	}
	for _, tab := range c.histTabs {
		c.historyMode = convo.HistoryAuto
		if !m.clickHistory(c, m.paneX()+tab.start, m.paneTop+paneTabsRow) {
			t.Fatalf("click missed %v", tab)
		}
		want := convo.HistoryCompact
		if tab.view == 1 {
			want = convo.HistoryOpen
		}
		if c.historyMode != want {
			t.Fatalf("mode %v after one click on %v", c.historyMode, tab)
		}
	}
}
