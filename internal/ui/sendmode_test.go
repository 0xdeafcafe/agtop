package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ctrl+t goes round queue, guide and stop & send for Claude Code, each
// session keeping its own, and the box's border says what enter does.
func TestSendModeCycles(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	if c == nil {
		t.Skip("no session open")
	}
	var saw []string
	for range 3 {
		m.cycleSendMode(c)
		saw = append(saw, sendModeNames[m.sendModeOf(c)])
	}
	if got := strings.Join(saw, ","); got != "guide,stop & send,queue" {
		t.Fatalf("modes went %s", got)
	}
	m.cycleSendMode(c)
	if top := ansi.Strip(m.sendModeTop(c)); !strings.Contains(top, "coalescing") || !strings.Contains(top, "stop & send") || strings.Contains(top, "to ") {
		t.Fatalf("border %q", top)
	}
}
