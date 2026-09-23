package convo

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
)

func fg(r, g, b int) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b) }
func bg(r, g, b int) string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b) }

// agtop's palette, with dim and faint raised so anything you read clears
// 4.5:1 and faint is left for decoration.
var (
	cText   = fg(226, 221, 211)
	cSub    = fg(168, 162, 152)
	cDim    = fg(138, 132, 122)
	cFaint  = fg(94, 89, 82)
	cWhite  = fg(240, 236, 228)
	cOrange = fg(217, 119, 87)
	cGreen  = fg(127, 191, 138)
	cYellow = fg(229, 181, 103)
	cRed    = fg(224, 104, 92)
	cBlue   = fg(143, 179, 217)
)

// Surfaces. The ground is the terminal's own background, so nothing here
// paints a slab over a themed terminal; only raised or tinted rows get one.
var (
	bgWell = bg(0x1a, 0x18, 0x16) // finished turn heading, output, diffs
	bgLive = bg(0x21, 0x18, 0x14) // heading of the running turn
	bgErr  = bg(0x2a, 0x17, 0x15) // failed output, a turn that crashed
	bgSel  = bg(0x2c, 0x28, 0x24) // selection where your keys go
	bgSelU = bg(0x1f, 0x1d, 0x1a) // selection on the other side
	bgAdd  = bg(0x16, 0x30, 0x1a)
	bgDel  = bg(0x3a, 0x17, 0x14)
)

func paint(c, s string) string {
	if s == "" {
		return ""
	}
	return c + s + reset
}

func text(s string) string  { return paint(cText, s) }
func sub(s string) string   { return paint(cSub, s) }
func dim(s string) string   { return paint(cDim, s) }
func faint(s string) string { return paint(cFaint, s) }

var spinner = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// row lays left and right out across width cells on background b ("" for
// the terminal's own), with the right half ending at the content cap so
// numbers stay near their labels on very wide panes.
func row(b, left, right string, width, capw int) string {
	if capw > width || capw <= 0 {
		capw = width
	}
	rw := ansi.StringWidth(right)
	if rw > capw-4 {
		right, rw = "", 0
	}
	room := capw - rw
	if rw > 0 {
		room -= 2
	}
	if ansi.StringWidth(left) > room {
		left = ansi.Truncate(left, max(0, room), "…")
	}
	gap := capw - ansi.StringWidth(left) - rw
	s := left + strings.Repeat(" ", max(0, gap)) + right + strings.Repeat(" ", max(0, width-capw))
	if b == "" {
		return s
	}
	return b + strings.ReplaceAll(s, reset, reset+b) + reset
}

func dur(d time.Duration) string {
	switch {
	case d < 0:
		return ""
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func money(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", v)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// wrap breaks styled text to w cells.
func wrap(s string, w int) []string {
	if w < 10 {
		w = 10
	}
	rows := strings.Split(ansi.Wrap(s, w, ""), "\n")
	// A row holding nothing but style codes is an artefact of the wrap;
	// fold its codes into the row before it.
	for len(rows) > 1 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		rows[len(rows)-2] += rows[len(rows)-1]
		rows = rows[:len(rows)-1]
	}
	return rows
}
