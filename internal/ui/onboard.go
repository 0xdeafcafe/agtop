package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
)

// Onboarding teaches agtop three ways: a tour on the first run that lights
// up each part of the screen in turn, a Getting started card under the list
// that ticks off as you do each thing, and a one-time tip the first time
// something happens that a key helps with.

// steps are Getting started: each is ticked off when you do it.
var steps = []struct{ id, what, key string }{
	{"start", "Start an agent", "enter"},
	{"session", "Open its Session", "tab"},
	{"hash", "Run a command", "#"},
	{"next", "Jump to who needs you", "ctrl+n"},
	{"zen", "Try zen", "ctrl+z"},
}

// didStep ticks off a Getting started step.
func (m *Model) didStep(id string) {
	if !m.onboard {
		return
	}
	o := &m.store.Config.Onboarding
	if slices.Contains(o.Steps, id) {
		return
	}
	o.Steps = append(o.Steps, id)
	_ = m.store.SaveConfig()
	if m.stepsDone() == len(steps) && !o.Hidden {
		m.flash("✓ all set · ? for keys", false)
	}
}

func (m *Model) stepsDone() int {
	n := 0
	for _, s := range steps {
		if slices.Contains(m.store.Config.Onboarding.Steps, s.id) {
			n++
		}
	}
	return n
}

// showCard is whether Getting started is under the list: until every step
// is done, or you put it away (#tour off).
func (m *Model) showCard() bool {
	return m.onboard && !m.store.Config.Onboarding.Hidden && m.stepsDone() < len(steps)
}

// startedLines is Getting started, w wide, with a blank line above.
func (m *Model) startedLines(w int) []string {
	w = min(w-2, 40)
	done := m.store.Config.Onboarding.Steps
	out := []string{"", " " + paint(cOrange+bold, "✦ Getting started") + faint(fmt.Sprintf("  %d/%d", m.stepsDone(), len(steps)))}
	for _, s := range steps {
		gap := w - cellw.String(s.what) - cellw.String(s.key) - 2
		if slices.Contains(done, s.id) {
			out = append(out, " "+paint(cGreen, "✓ ")+faint(s.what))
			continue
		}
		l := " " + dim("○ ") + paint(cText, s.what)
		if gap >= 2 {
			l += strings.Repeat(" ", gap) + paint(cOrange+bold, s.key)
		}
		out = append(out, l)
	}
	return out
}

// tips show once each, the first time they'd help.
var tips = []struct {
	id, text string
	when     func(m *Model, t tally) bool
}{
	{"needs", "● needs you · ctrl+n to go there", func(m *Model, t tally) bool {
		f := m.focused()
		return t.blocked > 0 && (f == nil || !f.NeedsYou())
	}},
	{"zen", "● several need you · ctrl+z for one at a time", func(m *Model, t tally) bool {
		return t.blocked > 1 && !m.zen
	}},
	{"session", "[ ] to switch view · esc to go back", func(m *Model, t tally) bool {
		return m.paneFocus && m.host != nil && !m.zen
	}},
	{"finished", "✓ finished · #done to put it away", func(m *Model, t tally) bool {
		f := m.focused()
		return f != nil && !f.Done && f.JustFinished(m.snap.At)
	}},
	{"hash", "enter to run it on the picked agent", func(m *Model, t tally) bool {
		return m.inKind == inPrompt && typingHash(string(m.input))
	}},
}

// noteProgress ticks off the steps that are a state rather than a key
// (being in a Session, zen), and on a tick shows the first tip that's due
// when nothing else is being said.
func (m *Model) noteProgress(tick bool) {
	if !m.onboard || m.tour > 0 {
		return
	}
	if m.sessionFocused() && !m.zen && m.focused() != nil {
		m.didStep("session")
	}
	if m.zen {
		m.didStep("zen")
	}
	o := &m.store.Config.Onboarding
	if !tick || len(o.Tips) == len(tips) || m.confirm != nil || m.dialog != nil || m.mode != modeList || m.snap == nil {
		return
	}
	if m.status != "" && m.snap.At.Sub(m.statusAt).Seconds() < 6 {
		return
	}
	t := m.tally()
	for _, tp := range tips {
		if !slices.Contains(o.Tips, tp.id) && tp.when(m, t) {
			o.Tips = append(o.Tips, tp.id)
			_ = m.store.SaveConfig()
			m.flash(tp.text, false)
			return
		}
	}
}

// tourSteps are the tour's stops: each lights up one part of the screen
// and says the keys for it.
var tourSteps = []struct {
	title, area string
	keys        [][2]string
}{
	{"▤ Your agents", "list", [][2]string{{"↑↓", "pick one"}, {"enter", "open it"}}},
	{"◧ The Session", "pane", [][2]string{{"tab", "jump here and back"}}},
	{"✎ The box", "box", [][2]string{{"enter", "start an agent"}, {"#", "run a command"}}},
	{"◈ Up top", "head", [][2]string{{"ctrl+n", "who needs you"}, {"?", "all the keys"}}},
}

// keyRows are "key  what" rows, each key a lit keycap, the keys in a column.
func keyRows(rows [][2]string) []string {
	kw := 0
	for _, r := range rows {
		kw = max(kw, cellw.String(r[0]))
	}
	var out []string
	for _, r := range rows {
		out = append(out, keycap(r[0], true)+strings.Repeat(" ", kw-cellw.String(r[0])+2)+paint(cText, r[1]))
	}
	return out
}

// startTour shows the tour from the top.
func (m *Model) startTour() {
	m.tour = 1
	m.mode = modeList
}

func (m *Model) endTour() {
	m.tour = 0
	if !m.store.Config.Onboarding.Toured {
		m.store.Config.Onboarding.Toured = true
		_ = m.store.SaveConfig()
	}
}

func (m *Model) tourKey(s string) {
	switch s {
	case "enter", " ", "space", "right", "tab", "l":
		if m.tour++; m.tour > len(tourSteps) {
			m.endTour()
		}
	case "left", "shift+tab", "h":
		m.tour = max(1, m.tour-1)
	case "esc", "q", "ctrl+c":
		m.endTour()
	}
}

type rect struct{ x0, y0, x1, y1 int }

func (r rect) empty() bool { return r.x1 <= r.x0 || r.y1 <= r.y0 }

// tourArea is where on the screen a tour stop's part is, from the frame
// just drawn.
func (m *Model) tourArea(area string, rows int) rect {
	head := m.headH() + 1
	switch area {
	case "head":
		return rect{0, 0, m.w, m.headH()}
	case "list":
		if m.listW > 0 {
			return rect{0, head, m.listW, m.promptTop}
		}
	case "pane":
		switch {
		case m.listW > 0 && m.listW < m.w:
			return rect{m.listW + 1, head, m.w, rows}
		case m.listW == 0:
			return rect{0, head, m.w, m.promptTop}
		}
	case "box":
		return rect{0, m.promptTop, m.promptW(m.listW, m.w-m.listW-1), rows}
	}
	return rect{}
}

// tourView is the screen with everything but the stop's part faded, and
// the stop's words in a box beside it.
func (m *Model) tourView() string {
	lines := strings.Split(m.listView(), "\n")
	st := tourSteps[m.tour-1]
	r := m.tourArea(st.area, len(lines))
	for y, l := range lines {
		plain := faint(ansi.Strip(fit(l, m.w)))
		if r.empty() || y < r.y0 || y >= r.y1 {
			lines[y] = plain
			continue
		}
		lines[y] = ansi.Truncate(plain, r.x0, "") + reset + ansi.Cut(fit(l, m.w), r.x0, r.x1) + reset + ansi.TruncateLeft(plain, r.x1, "")
	}
	bw := min(m.w-4, 38)
	head := paint(cOrange+bold, st.title)
	count := faint(fmt.Sprintf("%d/%d", m.tour, len(tourSteps)))
	body := []string{head + strings.Repeat(" ", max(1, bw-4-cellw.String(ansi.Strip(head+count)))) + count, ""}
	body = append(body, keyRows(st.keys)...)
	next := "next"
	if m.tour == len(tourSteps) {
		next = "done"
	}
	body = append(body, "", faint("enter "+next+" · esc skip"))
	box := edgedBox(body, bw, cOrange)
	top, left := m.tourPlace(r, len(box), bw, len(lines))
	return strings.Join(pasteAt(lines, box, top, left), "\n")
}

// tourPlace puts the words below the part, else above it, beside it, or
// in the middle of the screen.
func (m *Model) tourPlace(r rect, bh, bw, rows int) (top, left int) {
	mid := func(a, b, n int) int { return max(0, a+(b-a-n)/2) }
	if r.empty() {
		return mid(0, rows, bh), mid(0, m.w, bw)
	}
	switch {
	case rows-r.y1 >= bh+1:
		return r.y1 + 1, min(max(2, r.x0+2), m.w-bw)
	case r.y0 >= bh+1:
		return r.y0 - bh - 1, min(max(2, r.x0+2), m.w-bw)
	case m.w-r.x1 >= bw+3:
		return mid(r.y0, r.y1, bh), r.x1 + 2
	case r.x0 >= bw+3:
		return mid(r.y0, r.y1, bh), r.x0 - bw - 2
	}
	return mid(0, rows, bh), mid(0, m.w, bw)
}
