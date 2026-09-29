package convo

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// PasteChip is how a paste shows in a box and in a sent message: its
// number, its lines, and how it starts and ends, whitespace squeezed out so
// the preview always has words in it:
// [pasted text #1 · 12 lines: package main … return nil }].
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
	const each = 8
	preview := string(flat)
	if len(flat) > 2*each+3 {
		// Cut at a word, where that keeps at least half of each end.
		start, end := flat[:each], flat[len(flat)-each:]
		for i, r := range slices.Backward(start) {
			if r == ' ' && i >= each/2 {
				start = start[:i]
				break
			}
		}
		if i := slices.Index(end, ' '); i >= 0 && len(end)-i-1 >= each/2 {
			end = end[i+1:]
		}
		preview = strings.TrimSpace(string(start)) + " … " + strings.TrimSpace(string(end))
	}
	if preview == "" {
		return fmt.Sprintf("[pasted text #%d · %d %s]", n, lines, unit)
	}
	return fmt.Sprintf("[pasted text #%d · %d %s: %s]", n, lines, unit, preview)
}

// PasteChipRe matches a paste chip, its number the first group. Older chips
// kept in drafts ([#N L lines: …] and [Pasted text #N +L lines]) still match.
var PasteChipRe = regexp.MustCompile(`\[(?:[Pp]asted text )?#(\d+) (?:· )?(?:\+\d+ lines|\d+ lines?(?:: [^\]\n]*)?)\]`)

// HasPasteChip is a quick look for whether s may hold a chip.
func HasPasteChip(s string) bool {
	return strings.Contains(s, "[#") || strings.Contains(s, "asted text #")
}
