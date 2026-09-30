package ui

import (
	"fmt"
	"strings"

	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/efficiency"
)

// overviewSections are the Overview's own rows past the session's report:
// the pages it published, and what its agent reads as memory. Enter on a
// memory file opens the memory view on it; esc comes back.
func (m *Model) overviewSections(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	var out []convo.Line
	head := func(title, meta string) {
		h := "  " + faint("▾") + " " + paint(cSub+bold, title)
		if meta != "" {
			h += "  " + dim(meta)
		}
		out = append(out, convo.Line{}, convo.Line{Text: fit(h+" "+faint(strings.Repeat("─", max(0, w-cellwidth(h)-3))), w)})
	}
	if arts := c.artifactsOf(); len(arts) > 0 {
		head("Artifacts", fmt.Sprintf("%d published · enter opens one", len(arts)))
		out = append(out, m.artifactLines(c, o)[2:]...) // past its own heading
	}
	if canScreen(c, "memory") {
		files := m.memoryOf(c)
		var up int64
		for _, f := range files {
			up += f.Up
		}
		head("Memory", fmt.Sprintf("≈%s tokens every session · %d files · enter opens one", efficiency.Tokens(up), len(files)))
		var when map[string]string
		if c.memInfo != nil {
			when = c.memInfo.When
		}
		out = append(out, memRows(files, when, c.sel, w, o, memNothing(c, files))...)
	}
	return out
}
