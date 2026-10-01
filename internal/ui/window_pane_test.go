package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// A long conversation is drawn a window at a time, and Home and End reach
// its first and last turns all the same.
func TestPaneDrawsOnlyAWindow(t *testing.T) {
	m, _ := benchModel(200, 50)
	c := m.host
	c.sess, c.bodyBuf, c.historyMode = benchConvo(400), nil, convo.HistoryOpen
	shows := func(want string) bool {
		for _, r := range m.rushPane(120, 50) {
			if strings.Contains(ansi.Strip(r), want) {
				return true
			}
		}
		return false
	}
	if !shows("now make streaming quicker") {
		t.Fatal("the end isn't in view")
	}
	if len(c.shown)*4 > c.shownTotal {
		t.Fatalf("drew %d rows of %d", len(c.shown), c.shownTotal)
	}
	c.scroll, c.scrollOnly = 1<<30, true // Home
	if !shows("turn 1:") || c.shownBase != 0 {
		t.Fatalf("Home doesn't reach the first turn: base %d", c.shownBase)
	}
	c.scroll, c.scrollOnly = 0, true // End
	if !shows("now make streaming quicker") {
		t.Fatal("End doesn't reach the last turn")
	}
}
