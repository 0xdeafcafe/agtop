package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// Scrolled into the middle of a long conversation, the first prompt and
// the latest are pinned at the top; clicking one goes to it. At the end,
// where the latest is in view, only the first is pinned.
func TestPromptPins(t *testing.T) {
	m, _ := benchModel(200, 50)
	c := m.host
	c.sess, c.bodyBuf, c.historyMode = benchConvo(400), nil, convo.HistoryOpen
	pinned := func() (rows []string) {
		for i, r := range m.rushPane(120, 50) {
			if i < len(c.rowRefs) && strings.HasPrefix(c.rowRefs[i], pinRefPrefix) {
				rows = append(rows, ansi.Strip(r))
			}
		}
		return rows
	}
	if got := pinned(); len(got) != 1 || !strings.Contains(got[0], "first") || !strings.Contains(got[0], "turn 1:") {
		t.Fatalf("at the end, only the first: %q", got)
	}
	c.scroll, c.scrollOnly = 2000, true
	got := pinned()
	if len(got) != 2 || !strings.Contains(got[1], "latest") || !strings.Contains(got[1], "now make streaming quicker") {
		t.Fatalf("mid-way, first and latest: %q", got)
	}
	m.clickRef(c, pinRefPrefix+c.sess.TurnRef(0))
	if c.sel != c.sess.TurnRef(0) || !c.selMoved {
		t.Fatalf("a pin goes to its turn: sel %q", c.sel)
	}
	for i, r := range m.rushPane(120, 50) {
		if i < len(c.rowRefs) && !strings.HasPrefix(c.rowRefs[i], pinRefPrefix) && strings.Contains(ansi.Strip(r), "turn 1:") {
			return
		}
	}
	t.Fatal("after the click, the first prompt is in view")
}
