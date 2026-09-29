package convo

import (
	"fmt"
	"regexp"
	"strings"
)

// PasteChip is how a paste shows in a box and in a sent message: its
// number, its lines, and how it starts and ends, whitespace squeezed out so
// the preview always has words in it: [#1 12 lines: package m…return nil].
func PasteChip(n int, text string) string {
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	flat := []rune(strings.Map(func(r rune) rune {
		if r == '[' || r == ']' {
			return -1 // a chip ends at its ]
		}
		return r
	}, strings.Join(strings.Fields(text), " ")))
	const each = 10
	preview := string(flat)
	if len(flat) > 2*each+1 {
		// Cut at a word, where that keeps at least half of each end.
		start, end := string(flat[:each]), string(flat[len(flat)-each:])
		if i := strings.LastIndexByte(start, ' '); i >= each/2 {
			start = start[:i]
		}
		if i := strings.IndexByte(end, ' '); i >= 0 && len(end)-i-1 >= each/2 {
			end = end[i+1:]
		}
		preview = strings.TrimSpace(start) + "…" + strings.TrimSpace(end)
	}
	if preview == "" {
		return fmt.Sprintf("[#%d %d %s]", n, lines, unit)
	}
	return fmt.Sprintf("[#%d %d %s: %s]", n, lines, unit, preview)
}

// PasteChipRe matches a paste chip, its number the first group. Chips from
// before the preview ([Pasted text #N +L lines]) still match.
var PasteChipRe = regexp.MustCompile(`\[(?:Pasted text )?#(\d+) (?:\+\d+ lines|\d+ lines?(?:: [^\]\n]*)?)\]`)

// HasPasteChip is a quick look for whether s may hold a chip.
func HasPasteChip(s string) bool {
	return strings.Contains(s, "[#") || strings.Contains(s, "[Pasted text #")
}
