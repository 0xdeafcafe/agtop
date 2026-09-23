package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The input box sits on the brightest surface on screen, so where you type
// is never in doubt; its edge turns orange while your keys go there.
var (
	bgInput = "\x1b[48;2;40;36;32m"
	cEdge   = rgb(79, 73, 67)
)

// box describes one input box: the edge labels and what's inside.
type box struct {
	w       int
	focused bool
	topL    string // who the text goes to, and what enter will do
	topR    string
	footL   string
	footR   string
	text    []rune
	cursor  int
	holder  string // shown faint while the box is empty
	lead    string // before the text on its first line, e.g. ❯
	maxRows int
}

// lines draws the box: a top edge carrying its labels, the text wrapped
// with the cursor in place, and a bottom edge.
func (b box) lines() []string {
	w := max(20, b.w)
	edge := cEdge
	if b.focused {
		edge = cOrange
	}
	inner := w - 4 // "│ " … " │"
	border := func(l, r, left, right string) string {
		head := paint(edge, l+"─")
		if left != "" {
			head += " " + left + " "
		}
		tail := paint(edge, "─"+r)
		if right != "" {
			tail = " " + right + " " + tail
		}
		if ansi.StringWidth(head)+ansi.StringWidth(tail) > w {
			tail = paint(edge, "─"+r)
		}
		if over := ansi.StringWidth(head) + ansi.StringWidth(tail) - w; over > 0 {
			head = ansi.Truncate(head, ansi.StringWidth(head)-over-1, "…")
		}
		fill := w - ansi.StringWidth(head) - ansi.StringWidth(tail)
		return head + paint(edge, strings.Repeat("─", max(0, fill))) + tail
	}
	out := []string{border("╭", "╮", b.topL, b.topR)}
	for _, row := range b.content(inner) {
		pad := inner - ansi.StringWidth(row)
		body := row + strings.Repeat(" ", max(0, pad))
		body = bgInput + strings.ReplaceAll(body, reset, reset+bgInput) + reset
		out = append(out, paint(edge, "│")+bgInput+" "+reset+body+bgInput+" "+reset+paint(edge, "│"))
	}
	return append(out, border("╰", "╯", b.footL, b.footR))
}

// cursorMark stands in for the cursor while text is wrapped; it's one cell
// wide, like the bar that replaces it.
const cursorMark = ''

func (b box) content(w int) []string {
	lead := b.lead
	lw := ansi.StringWidth(lead)
	if len(b.text) == 0 {
		cur := ""
		if b.focused {
			cur = paint(cOrange, "▏")
		}
		return []string{lead + cur + faint(ansi.Truncate(b.holder, max(1, w-lw-1), "…"))}
	}
	pos := max(0, min(b.cursor, len(b.text)))
	marked := string(b.text[:pos]) + string(cursorMark) + string(b.text[pos:])
	var rows []string
	for _, para := range strings.Split(marked, "\n") {
		rows = append(rows, strings.Split(ansi.Wrap(para, max(10, w-lw), ""), "\n")...)
	}
	// Keep the cursor's row in view when the text is taller than the box.
	limit := max(1, b.maxRows)
	if len(rows) > limit {
		at := 0
		for i, r := range rows {
			if strings.ContainsRune(r, cursorMark) {
				at = i
			}
		}
		start := max(0, min(at-limit+1, len(rows)-limit))
		rows = rows[start : start+limit]
	}
	cur := " "
	if b.focused {
		cur = paint(cOrange, "▏") + cText
	}
	for i, r := range rows {
		r = paint(cText, strings.ReplaceAll(r, string(cursorMark), reset+cur))
		if i == 0 {
			r = lead + r
		} else {
			r = strings.Repeat(" ", lw) + r
		}
		rows[i] = r
	}
	return rows
}
