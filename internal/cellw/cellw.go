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
// anything else goes through segmentation. A colour or other CSI sequence
// is skipped whole rather than stepped through the parser a byte at a time.
func String(s string) int {
	pstate := parser.GroundState
	w := 0
	for i := 0; i < len(s); i++ {
		if pstate == parser.GroundState {
			j := printable(s, i)
			if j < len(s) && s[j] >= 0x80 && j > i {
				j-- // the last may start a cluster with what follows
			}
			w += j - i
			if i = j; i == len(s) {
				break
			}
			if k := csiEnd(s, i); k > i {
				i = k - 1
				continue
			}
		}
		c := s[i]
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

// printable is where the run of printable ASCII from i ends. Eight bytes
// are looked at a time: a frame is tens of kilobytes, mostly this.
func printable(s string, i int) int {
	const ones, highs = 0x0101010101010101, 0x8080808080808080
	for ; i+8 <= len(s); i += 8 {
		x := uint64(s[i]) | uint64(s[i+1])<<8 | uint64(s[i+2])<<16 | uint64(s[i+3])<<24 |
			uint64(s[i+4])<<32 | uint64(s[i+5])<<40 | uint64(s[i+6])<<48 | uint64(s[i+7])<<56
		// Any byte at or over 0x80, under 0x20, or 0x7f.
		y := x ^ 0x7f*ones
		if (x|(x-0x20*ones)&^x|(y-ones)&^y)&highs != 0 {
			break
		}
	}
	for i < len(s) && s[i] >= 0x20 && s[i] < 0x7f {
		i++
	}
	return i
}

// csiEnd is the end of the CSI sequence (ESC [ parameters, intermediates,
// final byte) starting at i, or i when there's none whole there: the
// parser goes back to ground after one and prints nothing of it.
// Anything else, a parameter after an intermediate or a control inside,
// is left to the parser.
func csiEnd(s string, i int) int {
	if i+1 >= len(s) || s[i] != 0x1b || s[i+1] != '[' {
		return i
	}
	k := i + 2
	for k < len(s) && s[k] >= 0x30 && s[k] < 0x40 {
		k++
	}
	for k < len(s) && s[k] >= 0x20 && s[k] < 0x30 {
		k++
	}
	if k < len(s) && s[k] >= 0x40 && s[k] < 0x7f {
		return k + 1
	}
	return i
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
	buf.Grow(len(s))

	for i < len(s) {
		// Not ansi's: printable ASCII and CSI sequences go whole, as
		// they would a byte at a time below.
		if pstate == parser.GroundState {
			if k := csiEnd(s, i); k > i {
				buf.WriteString(s[i:k])
				i = k
				continue
			}
			if j := printable(s, i); j > i {
				if !ignoring {
					n := min(j-i, length-curWidth)
					buf.WriteString(s[i : i+n])
					curWidth += n
					if n < j-i {
						ignoring = true
						buf.WriteString(tail)
					}
				}
				i = j
				continue
			}
		}
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
