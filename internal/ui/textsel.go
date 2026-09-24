package ui

import (
	"strings"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/charmbracelet/x/ansi"
)

// cell is a place in the pane's text: a row of the view's body (not of the
// screen, so scrolling doesn't move it) and a column within that row.
type cell struct{ row, col int }

// textSel is text dragged over in the pane. It stays inside the pane, as a
// terminal's own selection can't, and goes to the clipboard on release.
type textSel struct {
	on     bool   // there's a selection to show
	drag   bool   // the mouse button is still down
	moved  bool   // the drag has left the cell it started in
	a, b   cell   // where the drag began and where it is now
	view   string // the view it was made in; another view drops it
	pressY int    // the row pressed on, for a click that never moved
}

// selBlue is the dragged-over text: the colour a terminal selects with.
const selBlue = "\x1b[48;2;58;78;122m\x1b[38;2;240;236;228m"

// paneX is the screen column where the pane's text starts.
func (m *Model) paneX() int {
	if m.listW > 0 {
		return m.listW + 3
	}
	return 2
}

// textCell is the body cell under the pointer, clamped to what's shown:
// above or below the body it's the first or last row in view.
func (m *Model) textCell(c *hostConn, x, y int) (cell, bool) {
	first, last := -1, -1
	for i, r := range c.rowBody {
		if r >= 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return cell{}, false
	}
	i := min(max(y-m.paneTop, first), last)
	r := c.rowBody[i]
	for j := i; r < 0 && j >= first; j-- {
		r = c.rowBody[j] // the "more below" pill: the row above it
	}
	return cell{row: r, col: min(max(x-m.paneX(), 0), c.paneW)}, true
}

// startTextSel begins a drag in the pane's text, reporting whether the
// press was on it. What a click does there waits for the release, so a
// drag doesn't also open or close the row it started on.
func (m *Model) startTextSel(c *hostConn, x, y int) bool {
	i := y - m.paneTop
	if i < 0 || i >= len(c.rowBody) || c.rowBody[i] < 0 || m.viewName(c) == "screen" {
		return false
	}
	at, _ := m.textCell(c, x, y)
	c.txt = textSel{drag: true, a: at, b: at, view: m.viewName(c), pressY: y}
	return true
}

// dragTextSel moves the drag's end to the pointer. Past the top or bottom
// of the pane the conversation scrolls a row, so a drag can reach text out
// of view.
func (m *Model) dragTextSel(c *hostConn, x, y int) {
	i := y - m.paneTop
	switch {
	case i < c.bodyTop:
		c.scroll++
	case i >= c.bodyTop+c.bodyRows:
		c.scroll = max(0, c.scroll-1)
	}
	if at, ok := m.textCell(c, x, y); ok && at != c.txt.b {
		c.txt.b = at
		c.txt.moved, c.txt.on = true, true
	}
}

// endTextSel finishes a drag: what it covered goes to the clipboard. A
// press that never moved is a click on the row.
func (m *Model) endTextSel(c *hostConn) {
	c.txt.drag = false
	if !c.txt.moved {
		y := c.txt.pressY
		c.txt = textSel{}
		m.clickRow(c, y)
		return
	}
	if t := selectedText(c.shown, c.txt.a, c.txt.b, c.paneW); t != "" {
		m.copyText(t)
	}
}

// span orders a selection's ends.
func (s textSel) span() (cell, cell) {
	a, b := s.a, s.b
	if b.row < a.row || b.row == a.row && b.col < a.col {
		a, b = b, a
	}
	return a, b
}

// cols is the part of body row r the selection covers, [from, to).
func (s textSel) cols(r, w int) (int, int, bool) {
	a, b := s.span()
	if !s.on || r < a.row || r > b.row {
		return 0, 0, false
	}
	from, to := 0, w
	if r == a.row {
		from = a.col
	}
	if r == b.row {
		to = min(b.col+1, w) // the cell under the pointer is taken in
	}
	return from, to, from < to
}

// paintSel shows the selection on the rows drawn for the pane.
func (c *hostConn) paintSel(out []string) {
	for i, r := range c.rowBody {
		if r < 0 || i >= len(out) {
			continue
		}
		if from, to, ok := c.txt.cols(r, c.paneW); ok {
			out[i] = paintCols(out[i], from, to)
		}
	}
}

// paintCols paints cells [from, to) of a styled row as selected, keeping
// the styles either side.
func paintCols(s string, from, to int) string {
	if n := cellw.String(s); n < to {
		s += blanks(to - n) // a short row is selected out to where the drag reached
	}
	return ansi.Truncate(s, from, "") + reset + selBlue + ansi.Strip(ansi.Cut(s, from, to)) + reset + ansi.TruncateLeft(s, to, "")
}

// selectedText is the text between two cells of the drawn lines, as it was
// written rather than as it's laid out: rows the pane wrapped are joined
// again, the conversation's spine and the indent every row shares are
// dropped, and trailing blanks go.
func selectedText(lines []convo.Line, a, b cell, w int) string {
	if b.row < a.row || b.row == a.row && b.col < a.col {
		a, b = b, a
	}
	s := textSel{on: true, a: a, b: b}
	var out []string
	for r := a.row; r <= b.row && r < len(lines); r++ {
		from, to, ok := s.cols(r, w)
		seg := ""
		if ok {
			seg = ansi.Strip(ansi.Cut(lines[r].Text, from, to))
		}
		if from == 0 {
			// The spine and selection marker sit in column 0.
			if rs := []rune(seg); len(rs) > 0 && strings.ContainsRune("▏▍│", rs[0]) {
				seg = " " + string(rs[1:])
			}
		}
		seg = strings.TrimRight(seg, " ")
		if lines[r].Wrap && r > a.row && len(out) > 0 {
			prev := out[len(out)-1]
			sep := " "
			if strings.HasSuffix(prev, "-") || prev == "" {
				sep = "" // it was wrapped after a hyphen
			}
			out[len(out)-1] = prev + sep + strings.TrimLeft(seg, " ")
			continue
		}
		out = append(out, seg)
	}
	// The indent every row shares is the layout's, not the text's. A first
	// row that starts mid-line has none to judge by.
	cut := -1
	for i, l := range out {
		if strings.TrimSpace(l) == "" || i == 0 && a.col > 0 {
			continue
		}
		if n := len(l) - len(strings.TrimLeft(l, " ")); cut < 0 || n < cut {
			cut = n
		}
	}
	for i, l := range out {
		if i == 0 && a.col > 0 {
			out[i] = strings.TrimLeft(l, " ")
		} else if len(l) >= cut && cut > 0 {
			out[i] = l[cut:]
		}
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}
