// Package cellw measures styled text in terminal cells, the way
// ansi.StringWidth does, without segmenting plain ASCII into graphemes.
package cellw

import (
	"strings"

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

// Truncate is ansi.Truncate, its widths measured by String: ansi's own
// measure segments every line into graphemes before it cuts.
func Truncate(s string, length int, tail string) string {
	if String(s) <= length {
		return s
	}
	length -= String(tail)
	if length < 0 {
		return ""
	}
	// ansi v0.11.8's truncate from here.
	var cluster string
	var buf strings.Builder
	curWidth := 0
	ignoring := false
	pstate := parser.GroundState // initial state
	i := 0

	for i < len(s) {
		state, action := parser.Table.Transition(pstate, s[i])
		if state == parser.Utf8State {
			var width int
			cluster, width = ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			i += len(cluster)
			curWidth += width

			if ignoring {
				continue
			}

			if curWidth > length && !ignoring {
				ignoring = true
				buf.WriteString(tail)
			}

			if curWidth > length {
				continue
			}

			buf.WriteString(cluster)

			pstate = parser.GroundState
			continue
		}

		switch action {
		case parser.PrintAction:
			if curWidth >= length && !ignoring {
				ignoring = true
				buf.WriteString(tail)
			}

			if ignoring {
				i++
				continue
			}

			curWidth++
			fallthrough
		case parser.ExecuteAction:
			if ignoring {
				i++
				continue
			}
			fallthrough
		default:
			buf.WriteByte(s[i])
			i++
		}

		pstate = state

		if curWidth > length && !ignoring {
			ignoring = true
			buf.WriteString(tail)
		}
	}

	return buf.String()
}
