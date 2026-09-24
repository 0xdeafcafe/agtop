package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// The question card's grounds: the card itself, the option under the
// cursor, and the keycaps that number the options.
const (
	qCard  = "\x1b[48;2;42;36;25m"
	qSel   = "\x1b[48;2;60;49;34m"
	qCap   = "\x1b[48;2;68;58;43m"
	qCapOn = "\x1b[48;2;217;119;87m"
	qChip  = "\x1b[48;2;229;181;103m"
	qInk   = "\x1b[38;2;33;28;22m" // dark text on a lit chip or keycap
)

// keycap is a key drawn as a key: " 1 " on its own ground, lit when it's
// the one under the cursor.
func keycap(k string, on bool) string {
	if on {
		return qCapOn + qInk + bold + " " + k + " " + reset
	}
	return qCap + cSub + " " + k + " " + reset
}

// chip is a lit label, the card's heading.
func chip(s string) string { return qChip + qInk + bold + " " + s + " " + reset }

// questionCard draws Claude's questions as one form: a heading, the
// question as prose with the ask itself set apart, each option as a block
// (a numbered keycap, its label, and the whole of its description), and
// beside them the preview of the option under the cursor when Claude gave
// one. With more than one question, a strip names them all (answered ones
// with their answer, so you can go back and change one) and a last step
// lists your answers before they go. It keeps to maxH rows by shortening
// the options not under the cursor first, then the question's lead-in.
func (m *Model) questionCard(c *hostConn, req *headless.PermissionRequest, w, maxH int) []string {
	c.syncQuestion(req)
	title, qs := questions(req)
	if len(qs) == 0 {
		return nil
	}
	var out []string
	for fold := 0; fold <= 3; fold++ {
		out = drawQuestion(c, title, qs, w, fold)
		if maxH <= 0 || len(out) <= maxH {
			break
		}
	}
	return out
}

// drawQuestion draws the card, folded: 0 shows everything, and each step up
// shows less of what isn't under the cursor.
func drawQuestion(c *hostConn, title string, qs []question, w, fold int) []string {
	var out []string
	edge := paint(cYellow, "▍")
	if c.cardFocus {
		edge = paint(cOrange, "▍")
	}
	cl := func(txt string) { out = append(out, onBg(qCard, edge+txt, w)) }
	review := c.qIdx >= len(qs)
	textW := max(20, min(w-8, 108))

	// The heading: what it's about, and where you are among several.
	head, right := "", ""
	switch {
	case len(qs) == 1 && qs[0].Header != "":
		head = chip("? "+oneLine(qs[0].Header)) + "  " + paint(cSub, "Claude asks")
	default:
		head = chip("?") + " " + paint(cYellow+bold, "Claude asks")
	}
	if title != "" {
		head += "  " + dim("·") + "  " + paint(cText, oneLine(title))
	}
	if len(qs) > 1 {
		right = fmt.Sprintf("%d of %d answered", len(c.qAnswer), len(qs))
	}
	cl(spread(" "+head, paint(cSub, right)+"  ", w-1))
	if len(qs) > 1 {
		cl("")
		cl("  " + questionStrip(c, qs, w-4))
	}
	cl("")

	if review {
		labelW := 0
		for i, q := range qs {
			labelW = max(labelW, cellw.String(firstNonEmpty(q.Header, fmt.Sprintf("Question %d", i+1))))
		}
		labelW = min(labelW, 24)
		cl("  " + paint(cText+bold, "Send these answers?"))
		cl("")
		for i, q := range qs {
			name := ansi.Truncate(firstNonEmpty(q.Header, fmt.Sprintf("Question %d", i+1)), labelW, "…")
			ans, ok := c.qAnswer[q.Question]
			a := paint(cGreen, "✓ ") + paint(cText+bold, ansi.Truncate(shownAnswer(ans), max(10, w-labelW-16), "…"))
			if !ok {
				a = paint(cYellow, "○ not answered")
			}
			cl("  " + keycap(fmt.Sprint(i+1), false) + "  " + paint(cSub, name) + strings.Repeat(" ", labelW-cellw.String(name)+3) + a)
		}
		cl("")
		// The button enter presses: lit while the card has the keys.
		if c.cardFocus {
			cl("  " + tabOn + " ⏎ Send answers " + reset)
		} else {
			cl("  " + tabOff + " ⏎ Send answers " + reset)
		}
		cl("")
		if c.cardFocus {
			cl("  " + keysFit(w-6, "enter", "sends your answers", "← · 1–"+fmt.Sprint(len(qs)), "changes one", "s", "skips them all", "esc", "type instead"))
		} else {
			cl("  " + dim("↑ to send or change your answers"))
		}
		return out
	}

	// The question: a lead-in as prose, then the ask itself in bold.
	q := qs[c.qIdx]
	lead, ask := splitAsk(q.Question)
	if lead != "" {
		lines := wrap(convo.Inline(lead, cText), textW)
		if fold >= 3 && len(lines) > 4 {
			lines = append(lines[:3:3], lines[3]+" …")
		}
		for _, l := range lines {
			cl("  " + paint(cText, l))
		}
		cl("")
	}
	for _, l := range wrap(convo.Inline(ask, cText+bold), textW) {
		cl("  " + paint(cText+bold, l))
	}
	cl("")

	// The options, then the preview beside them when there's room, or
	// under them when there isn't.
	cursor := -1
	if c.cardFocus {
		cursor = c.qCursor
	}
	var preview, previewOf string
	if at := c.qCursor; at < len(q.Options) {
		preview, previewOf = q.Options[at].Preview, q.Options[at].Label
	}
	side := preview != "" && w >= 96
	lw := w - 1
	if side {
		lw = min(60, (w-1)/2)
	}
	type orow struct {
		text string
		sel  bool
	}
	var rows []orow
	descW := max(16, min(lw-10, 104))
	picked := c.picks(c.qIdx)
	prev := c.qAnswer[q.Question]
	for i, o := range q.Options {
		on := cursor == i
		label, rec := optionLabel(o.Label)
		bar := " "
		if on {
			bar = paint(cOrange, "▌")
		}
		mark := ""
		switch {
		case q.MultiSelect && picked[i]:
			mark = paint(cGreen, "☑ ")
		case q.MultiSelect:
			mark = dim("☐ ")
		}
		name := paint(cText+bold, label)
		if !on {
			name = paint(cText, label)
		}
		name = keycap(fmt.Sprint(i+1), on) + " " + mark + name
		if rec {
			name += "  " + paint(cGreen, "★ recommended")
		}
		if !q.MultiSelect && prev == o.Label {
			name += "  " + paint(cGreen, "✓ your answer")
		}
		if o.Preview != "" && !side && o.Preview != preview {
			name += "  " + dim("◇ preview")
		}
		rows = append(rows, orow{" " + bar + " " + name, on})
		if d := strings.TrimSpace(o.Description); d != "" {
			ink := cSub
			if on {
				ink = cText
			}
			lines := wrap(convo.Inline(d, ink), descW)
			keep := [...]int{6, 2, 1, 0}[fold]
			if on {
				keep = 12
			}
			if len(lines) > keep {
				lines = lines[:keep]
				if keep > 0 {
					lines[keep-1] += " …"
				}
			}
			for _, l := range lines {
				rows = append(rows, orow{" " + bar + "     " + paint(ink, l), on})
			}
		}
		if fold < 2 {
			rows = append(rows, orow{"", false})
		}
	}
	own := cursor == len(q.Options)
	bar := " "
	if own {
		bar = paint(cOrange, "▌")
	}
	ownText := paint(cText, "Something else") + "  " + dim("type your own answer below")
	if own {
		ownText = paint(cText+bold, "Something else") + "  " + paint(cSub, "enter, then type it below")
	}
	rows = append(rows, orow{" " + bar + " " + keycap("✎", own) + " " + ownText, own})

	var pv []string
	if preview != "" {
		pw := w - 1 - 5
		if side {
			pw = w - 1 - lw - 3
		}
		name, _ := optionLabel(previewOf)
		pv = previewBox(name, preview, pw)
	}
	n := len(rows)
	if side {
		n = max(n, len(pv))
	}
	for i := 0; i < n; i++ {
		left, sel := "", false
		if i < len(rows) {
			left, sel = rows[i].text, rows[i].sel
		}
		bg := qCard
		if sel {
			bg = qSel
		}
		if !side {
			cl(onBg(bg, left, w-3) + "  ")
			continue
		}
		r := ""
		if i < len(pv) {
			r = pv[i]
		}
		cl(onBg(bg, left, lw) + "  " + r)
	}
	if !side && len(pv) > 0 {
		cl("")
		for _, r := range pv {
			cl("     " + r)
		}
	}
	cl("")
	if c.cardFocus {
		pairs := []string{"↑↓", "choose", "enter", "picks", "1–" + fmt.Sprint(len(q.Options)), "pick one"}
		if q.MultiSelect {
			pairs = []string{"↑↓", "choose", "space", "ticks", "enter", "confirms"}
		}
		if len(qs) > 1 {
			pairs = append(pairs, "←→", "questions")
		}
		pairs = append(pairs, "s", "skips", "esc", "type instead")
		cl("  " + keysFit(w-6, pairs...))
	} else {
		cl("  " + paint(cSub, "↑") + dim(" to choose   ·   or type your own answer below and press ") + paint(cSub, "enter"))
	}
	return out
}

// splitAsk parts a question into its lead-in and the ask: Claude often
// explains first and asks last ("… so I need your go-ahead. Proceed?").
// The ask is the last sentence when it's a question and there's something
// before it; otherwise it's the whole text.
func splitAsk(q string) (lead, ask string) {
	q = strings.TrimSpace(q)
	if !strings.HasSuffix(q, "?") {
		return "", q
	}
	if i := strings.LastIndex(q, "\n\n"); i > 0 {
		return strings.TrimSpace(q[:i]), strings.TrimSpace(q[i+2:])
	}
	for i := len(q) - 2; i > 0; i-- {
		if q[i] != ' ' && q[i] != '\n' || !strings.ContainsRune(".!?", rune(q[i-1])) {
			continue
		}
		rest := strings.TrimSpace(q[i:])
		if r := []rune(rest); len(r) > 0 && unicode.IsUpper(r[0]) {
			return strings.TrimSpace(q[:i]), rest
		}
		break
	}
	return "", q
}

// questionStrip is a row naming every question: the one at hand lit, ✓
// and its answer once answered, ○ the rest, then send.
func questionStrip(c *hostConn, qs []question, w int) string {
	var parts []string
	for i, q := range qs {
		name := firstNonEmpty(q.Header, fmt.Sprintf("Question %d", i+1))
		ans, ok := c.qAnswer[q.Question]
		switch {
		case i == c.qIdx:
			parts = append(parts, chip(name))
		case ok:
			parts = append(parts, paint(cGreen, "✓ ")+paint(cSub, name)+" "+dim(ansi.Truncate(shownAnswer(ans), 18, "…")))
		default:
			parts = append(parts, dim("○ "+name))
		}
	}
	send := dim("○ send")
	if c.qIdx >= len(qs) {
		send = chip("send")
	}
	parts = append(parts, send)
	return ansi.Truncate(strings.Join(parts, faint("  ›  ")), w, "…")
}

// previewBox frames an option's preview under its name: Claude writes it as markdown, most
// often a mockup or code, so its lines are kept as they are (fences
// dropped), cut to the width and to 14 rows.
func previewBox(name, md string, w int) []string {
	var body []string
	for _, l := range strings.Split(strings.ReplaceAll(md, "\t", "    "), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			continue
		}
		body = append(body, ansi.Strip(strings.TrimRight(l, " \r")))
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	const maxRows = 14
	more := 0
	if len(body) > maxRows {
		more = len(body) - maxRows + 1
		body = body[:maxRows-1]
	}
	inner := 8
	for _, l := range body {
		inner = max(inner, cellw.String(l))
	}
	inner = min(inner, max(8, w-4))
	label := ansi.Truncate(oneLine(name), max(4, inner-2), "…")
	top := faint("╭─") + " " + paint(cSub, label) + " " + faint(strings.Repeat("─", max(0, inner-cellw.String(label)-1))+"╮")
	out := []string{top}
	for _, l := range body {
		if cellw.String(l) > inner {
			l = ansi.Truncate(l, inner-1, "") + faint("›")
		}
		out = append(out, faint("│")+" "+paint(cText, l)+strings.Repeat(" ", inner-cellw.String(l))+" "+faint("│"))
	}
	if more > 0 {
		t := fmt.Sprintf("… %d more lines", more)
		out = append(out, faint("│")+" "+dim(t)+strings.Repeat(" ", max(0, inner-cellw.String(t)))+" "+faint("│"))
	}
	return append(out, faint("╰"+strings.Repeat("─", inner+2)+"╯"))
}
