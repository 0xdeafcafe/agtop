// Package cellw measures styled text in terminal cells, the way
// ansi.StringWidth does, without segmenting plain ASCII into graphemes.
package cellw

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

// String is ansi.StringWidth. An ASCII character followed by another ASCII
// byte is a grapheme of its own, one cell wide, so only text around
// anything else goes through segmentation.
func String(s string) int {
	pstate := parser.GroundState
	w := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if pstate == parser.GroundState && c >= 0x20 && c < 0x7f && (i+1 == len(s) || s[i+1] < 0x80) {
			w++
			continue
		}
		state, action := parser.Table.Transition(pstate, c)
		if action == parser.PrintAction || state == parser.Utf8State {
			cluster, cw := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			w += cw
			i += len(cluster) - 1
			pstate = parser.GroundState
			continue
		}
		pstate = state
	}
	return w
}
