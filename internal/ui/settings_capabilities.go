package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Capabilities sets every installed agent side by side: how far each has
// been tried, then a row for each feature rush has, ✓ where it can do it,
// – where it can't and ◌ where it's planned, a * where there's more to
// say, said under the table for the feature the cursor is on; then what each agent's models take. ↑↓ go down the
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
	const labelW = 24
	col := max(6, min(16, (w-8-labelW)/len(kinds)))
	inner := 2 + labelW + 2 + col*len(kinds) // gutter, label, split, marks
	i := 0
	edge := func(l, fill, r string) string { return paint(cEdge, l+strings.Repeat(fill, inner+2)+r) }
	// row boxes line on bg after a gutter of two: the cursor's bar when on
	// it.
	row := func(line, bg string) string {
		gutter := "  "
		if bg == selBG {
			gutter = paint(cOrange, "▍") + " "
		}
		s := fit(gutter+line, inner)
		if bg != "" {
			s = bg + strings.ReplaceAll(s, reset, reset+bg) + reset
		}
		return paint(cEdge, "│") + " " + s + " " + paint(cEdge, "│")
	}
	split := paint(cEdge, "│") + " "
	head, level := fit("", labelW)+split, dim(fit("support", labelW))+split
	for _, k := range kinds {
		head += fit(glyph(k)+" "+paint(cText+bold, kindName(k)), col-1) + " "
		level += fit(levelChip(k), col)
	}
	out := []string{edge("╭", "─", "╮"), row(head, ""), row(level, ""), edge("├", "─", "┤")}
	features := agent.AllFeatures()
	for n, f := range features {
		line := paint(cText, fit(f.Label, labelW)) + split
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
				mark += paint(cYellow, "*")
			}
			line += fit(mark, col)
		}
		bg := ""
		if n%2 == 1 {
			bg = hoverBG
		}
		if i == d.cursor {
			bg = selBG
		}
		out = append(out, row(line, bg))
		i++
	}
	out = append(out, edge("╰", "─", "╯"), "  "+dim("✓ yes   – no   ◌ planned   ")+paint(cYellow, "*")+dim(" more to say: put the cursor on it"))
	// What the * say, for the feature under the cursor only.
	if d.cursor < len(features) {
		f := features[d.cursor]
		var notes []string
		for _, k := range kinds {
			if s := agent.FeatureOf(k, f.Feature); s.Note != "" {
				notes = append(notes, "  "+fit(glyph(k)+" "+paint(cText, kindName(k)), 18)+dim(s.Note))
			}
		}
		if len(notes) > 0 {
			out = append(out, "", rule(f.Label, "", w))
			out = append(out, notes...)
		}
	}

	out = append(out, "", rule("Models", "what each takes", w))
	for _, k := range kinds {
		models := m.agentModels(k)
		for j, l := range modelTable(k, models) {
			if j >= 2 && j < 2+len(models) {
				if i == d.cursor {
					l = highlight(paint(cOrange, "▍")+" "+strings.TrimPrefix(l, "  "), w)
				}
				i++
			}
			out = append(out, l)
		}
	}
	if missing := m.notInstalled(); missing != "" {
		out = append(out, "", faint("Not installed here: ")+missing)
	}
	return append(out, "", keysFit(w, append([]string{"↑↓", "features and models"}, pagesKeys...)...))
}
