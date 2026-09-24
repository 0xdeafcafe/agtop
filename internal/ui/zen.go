package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// Zen shows nothing until an agent needs you, then only that agent: what it
// last said and what it needs, answered in its dock. Answer it and the next
// one takes its place.

// zenQueue is every agent waiting on you, oldest first.
func (m *Model) zenQueue() []*fleet.Agent {
	var out []*fleet.Agent
	for _, a := range m.snap.Agents {
		if t, ok := m.openFailed[a.Key]; ok && time.Since(t) < 30*time.Second {
			continue // couldn't be shown; try it again in a bit
		}
		if a.NeedsYou() || a.Waiting() {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out
}

// zenPick keeps the selection on an agent that still needs you, moving on
// to the oldest one when the current one is answered.
func (m *Model) zenPick() {
	if !m.zenFull() {
		return // on zen's list you pick; nothing moves under you
	}
	q := m.zenQueue()
	for _, a := range q {
		if a.Key == m.sel {
			m.paneFocus = true
			return
		}
	}
	// The one on screen is answered. Move on only once you've stopped
	// typing, so a key meant for it never lands on the next agent's card.
	if c := m.host; c != nil && c.key == m.sel && (len(c.input) > 0 || time.Since(m.lastKeyAt) < 2*time.Second) {
		return
	}
	if len(q) > 0 {
		m.sel = q[0].Key
		m.paneFocus = true
	}
}

// zenSkip moves to the next agent waiting on you.
func (m *Model) zenSkip() {
	q := m.zenQueue()
	for i, a := range q {
		if a.Key == m.sel && len(q) > 1 {
			m.sel = q[(i+1)%len(q)].Key
			return
		}
	}
}

// zenQuiet is the screen when nothing needs you.
func (m *Model) zenQuiet(w, h int) []string {
	t := m.tally()
	out := make([]string, max(0, h/3))
	center := func(s string) string {
		pad := max(0, (w-len([]rune(stripAnsi(s))))/2)
		return strings.Repeat(" ", pad) + s
	}
	out = append(out, center(paint(cGreen, "✓ nothing needs you")), "")
	var counts []string
	if t.working > 0 {
		counts = append(counts, paint(cOrange, fmt.Sprintf("✻ %d working", t.working)))
	}
	if t.busy > 0 {
		counts = append(counts, dim(fmt.Sprintf("◌ %d in background", t.busy)))
	}
	counts = append(counts, dim(fmt.Sprintf("%d finished", t.done)))
	out = append(out, center(strings.Join(counts, dim(" · "))), "")
	out = append(out, center(faint("the moment an agent asks for something it appears here · ctrl+z leaves zen")))
	return out
}

func stripAnsi(s string) string { return ansi.Strip(s) }

// zenBody is the middle of zen's Session: which of the waiting agents this
// is, then the last thing it said.
func (m *Model) zenBody(a *fleet.Agent, c *hostConn, w int) []convo.Line {
	q := m.zenQueue()
	pos := 0
	for i, x := range q {
		if x.Key == a.Key {
			pos = i + 1
		}
	}
	waited := dur(time.Since(a.UpdatedAt).Round(time.Second))
	head := paint(cYellow+bold, "● needs you") + "  " + paint(cSub, fmt.Sprintf("%d of %d", pos, len(q))) +
		"   " + dim(tildify(a.Cwd))
	if pos == 0 {
		// Answered, or never waiting: say so rather than "needs you".
		head = paint(cGreen, "✓ answered") + "  " + dim(fmt.Sprintf("%d waiting", len(q))) + "   " + dim(tildify(a.Cwd))
	}
	if a.Branch != "" {
		head += dim(" · " + a.Branch)
	}
	lines := []convo.Line{{Text: fit("  "+spread(head, dim("waiting "+waited)+"  ", w-2), w)}, {Text: ""}}
	said := ""
	if c != nil {
		for i := len(c.sess.Turns) - 1; i >= 0 && said == ""; i-- {
			items := c.sess.Turns[i].Items
			for j := len(items) - 1; j >= 0; j-- {
				if items[j].Kind == convo.KText && strings.TrimSpace(items[j].Text) != "" {
					said = items[j].Text
					break
				}
			}
		}
	}
	if said == "" {
		said = firstNonEmpty(a.Needs, a.Detail)
	}
	lines = append(lines, convo.Line{Text: "    " + dim("it said")})
	for _, para := range strings.Split(strings.TrimSpace(said), "\n") {
		for _, l := range wrap(para, min(w-8, 100)) {
			lines = append(lines, convo.Line{Text: "    " + paint(cText, l)})
		}
	}
	if a.Needs != "" && c != nil && len(c.sess.Pending()) == 0 {
		lines = append(lines, convo.Line{Text: ""}, convo.Line{Text: "    " + dim("it needs  ") + paint(cYellow, oneLine(a.Needs))})
	}
	return append(lines, convo.Line{Text: ""}, convo.Line{Text: "  " + dim("ctrl+n skips to the next · ctrl+z leaves zen")})
}
