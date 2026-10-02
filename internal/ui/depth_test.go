package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/charmbracelet/x/ansi"
)

// Each depth shows less than the one below it, and m0 shows no step rows
// but what's said.
func TestDepthsShowLessAtEachLevel(t *testing.T) {
	m, _ := benchModel(200, 45)
	s := m.host.sess
	rows := func(d convo.Depth, verbose bool) []convo.Line {
		return s.Render(convo.Options{Width: 120, Now: time.Now(), Depth: d, Verbose: verbose, History: convo.HistoryOpen})
	}
	m0, m1, m2, m3 := rows(convo.DepthProse, false), rows(convo.DepthRuns, false), rows(convo.DepthDefault, false), rows(convo.DepthDefault, true)
	if !(len(m0) < len(m1) && len(m1) <= len(m2) && len(m2) < len(m3)) {
		t.Fatalf("rows m0..m3: %d %d %d %d", len(m0), len(m1), len(m2), len(m3))
	}
	var b strings.Builder
	for _, l := range m0 {
		b.WriteString(ansi.Strip(l.Text) + "\n")
	}
	text := b.String()
	if strings.Contains(text, "✎ internal/convo/render.go") || strings.Contains(text, "show 2 steps") {
		t.Fatalf("m0 should be prose alone:\n%s", text)
	}
	if !strings.Contains(text, "That should make streaming feel instant") {
		t.Fatal("m0 lost the prose")
	}
}
