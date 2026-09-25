package convo

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/cellw"
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
	cOut    = fg(119, 113, 106) // what tools print: under faint text, over faint rules
	cOKq    = fg(95, 138, 104)  // a finished step's tick, quiet like its row
	// A card's frame says what its command did: made something (cOKq),
	// rewrote or set something aside (cWarnQ), threw something away or
	// failed (cLostQ); cLost is that last one's glyph.
	cWarnQ = fg(168, 136, 82)
	cLost  = fg(224, 104, 92)
	cLostQ = fg(170, 86, 76)
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
	// A diff line's changed words, a step brighter than the line.
	bgAddHi = bg(0x22, 0x52, 0x2b)
	bgDelHi = bg(0x62, 0x24, 0x1e)
)

// palette is how many times the colours have changed, so a cached drawing
// in the old ones isn't used.
var palette int

// SetColorBlind swaps green and red, what agtop uses for added and
// removed, done and failed, for sky blue and amber (from the Okabe-Ito
// palette), which stay apart for every common kind of colour blindness.
func SetColorBlind(on bool) {
	if on {
		cGreen, cRed, cOKq = fg(86, 180, 233), fg(230, 159, 0), fg(80, 140, 180)
		// Amber is taken by failed; lost is vermillion, clear of warn.
		cLost, cLostQ = fg(213, 94, 0), fg(160, 74, 12)
		bgAdd, bgDel = bg(0x10, 0x2a, 0x3c), bg(0x30, 0x24, 0x0e)
		bgAddHi, bgDelHi = bg(0x1a, 0x46, 0x64), bg(0x52, 0x3a, 0x10)
		bgErr = bg(0x30, 0x24, 0x10)
		spineErr = paint(cRed, "▏")
	} else {
		cGreen, cRed, cOKq = fg(127, 191, 138), fg(224, 104, 92), fg(95, 138, 104)
		cLost, cLostQ = fg(224, 104, 92), fg(170, 86, 76)
		bgAdd, bgDel = bg(0x16, 0x30, 0x1a), bg(0x3a, 0x17, 0x14)
		bgAddHi, bgDelHi = bg(0x22, 0x52, 0x2b), bg(0x62, 0x24, 0x1e)
		bgErr = bg(0x2a, 0x17, 0x15)
		spineErr = paint(cRed, "▏")
	}
	palette++
}

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

// folded says n lines are folded away: the count in bold and the key that
// unfolds them in orange, so the gap reads as something you can open.
func folded(n int) string {
	return dim("… ") + paint(cText+bold, fmt.Sprint(n)) + dim(" lines ") + faint("·") + " " +
		paint(cOrange+bold, "ctrl+o") + dim(" shows all")
}

var spinner = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// row lays left and right out across width cells on background b ("" for
// the terminal's own), with the right half ending at the content cap so
// numbers stay near their labels on very wide panes.
func row(b, left, right string, width, capw int) string {
	if capw > width || capw <= 0 {
		capw = width
	}
	rw := cellw.String(right)
	if rw > capw-4 {
		right, rw = "", 0
	}
	room := capw - rw
	if rw > 0 {
		room -= 2
	}
	lw := cellw.String(left)
	if lw > room {
		left = ansi.Truncate(left, max(0, room), "…")
		lw = cellw.String(left)
	}
	gap := max(0, capw-lw-rw)
	var sb strings.Builder
	sb.Grow(len(b) + len(left) + gap + len(right) + max(0, width-capw) + 32)
	if b == "" {
		sb.WriteString(left)
		sb.WriteString(blanks(gap))
		sb.WriteString(right)
		sb.WriteString(blanks(width - capw))
		return sb.String()
	}
	// Every reset inside returns to the row's background.
	sb.WriteString(b)
	writeIn(&sb, left, b)
	sb.WriteString(blanks(gap))
	writeIn(&sb, right, b)
	sb.WriteString(blanks(width - capw))
	sb.WriteString(reset)
	return sb.String()
}

// writeIn writes s with every reset followed by bg.
func writeIn(sb *strings.Builder, s, bg string) {
	for {
		i := strings.Index(s, reset)
		if i < 0 {
			sb.WriteString(s)
			return
		}
		sb.WriteString(s[:i+len(reset)])
		sb.WriteString(bg)
		s = s[i+len(reset):]
	}
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
	if single(s) {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}

// single is whether s is already one line of single-spaced ASCII words, so
// oneLine has nothing to do.
func single(s string) bool {
	if s == "" || s[0] == ' ' || s[len(s)-1] == ' ' {
		return s == ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 && (c != ' ' || s[i+1] == ' ') || c >= 0x7f {
			return false
		}
	}
	return true
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
