package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Capabilities sets every installed agent side by side: how far each has
// been tried, then a row for each feature rush has, ✓ where it can do it,
// – where it can't and ◌ where it's planned, a * where there's more to
// say, said below; then what each agent's models take. ↑↓ go down the
// features and the models, so a long page scrolls.

// capabilitiesLen is the features, then every agent's models.
func (m *Model) capabilitiesLen() int {
	n := len(agent.AllFeatures())
	for _, ad := range m.agentOrder() {
		n += len(m.agentModels(ad.Kind()))
	}
	return n
}

func (m *Model) capabilitiesBody(w int) []string {
	d := m.dialog
	var kinds []agent.Kind
	for _, ad := range m.agentOrder() {
		kinds = append(kinds, ad.Kind())
	}
	if len(kinds) == 0 {
		return []string{dim("No coding agent is installed where rush looks: install Claude Code, Codex or another, and it shows here.")}
	}
	const labelW = 26
	col := max(6, min(14, (w-4-labelW)/len(kinds)))
	row, i := func(line string) string { return "  " + line }, 0
	// cursor is the row the cursor goes through next, highlighted when on.
	cursor := func(line string) string {
		defer func() { i++ }()
		if i == d.cursor {
			return highlight(paint(cOrange, "▍")+" "+line, w)
		}
		return row(line)
	}
	head, level := fit("", labelW), dim(fit("support", labelW))
	for _, k := range kinds {
		head += fit(glyph(k)+" "+paint(cText+bold, kindName(k)), col)
		level += fit(levelChip(k), col)
	}
	out := []string{row(head), row(level), ""}
	var notes []string
	for _, f := range agent.AllFeatures() {
		line := paint(cText, fit(f.Label, labelW))
		for _, k := range kinds {
			s := agent.FeatureOf(k, f.Feature)
			mark := faint("–")
			switch s.Is {
			case agent.StateYes:
				mark = paint(cGreen, "✓")
			case agent.StatePlanned:
				mark = paint(cYellow, "◌")
			}
			if s.Note != "" {
				mark += faint("*")
				notes = append(notes, glyph(k)+" "+dim(kindName(k)+" · "+f.Label+": ")+faint(s.Note))
			}
			line += fit(" "+mark, col)
		}
		out = append(out, cursor(line))
	}
	out = append(out, row(faint("✓ yes   – no   ◌ planned   * see below")))
	for _, n := range notes {
		out = append(out, row(n))
	}

	out = append(out, "", rule("Models", "what each takes", w))
	for _, k := range kinds {
		models := m.agentModels(k)
		for j, l := range modelTable(k, models) {
			if j >= 2 && j < 2+len(models) {
				l = cursor(strings.TrimPrefix(l, "  "))
			}
			out = append(out, l)
		}
	}
	if missing := m.notInstalled(); missing != "" {
		out = append(out, "", faint("Not installed here: ")+missing)
	}
	return append(out, "", keysFit(w, append([]string{"↑↓", "features and models"}, pagesKeys...)...))
}
