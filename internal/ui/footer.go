package ui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// The side list's footer: what the next agent starts as, where, and the
// setups agents in the fleet ran lately, a click away.

const (
	footNextKey   = "⚙next"   // a click opens the start sheet
	footRecentKey = "⚙recent" // a click takes the setup under it
)

// footHit is a recent setup on the footer's last line, by column.
type footHit struct {
	x0, x1 int
	o      startOver
}

// footLines is the footer, w wide, with each row's key for a click.
func (m *Model) footLines(w int) (lines, keys []string) {
	dir := m.startDir()
	head := "  next agent "
	lines = []string{
		"",
		faint(head + strings.Repeat("─", max(0, w-cellw.String(head)-1))),
		"  " + paint(cText, fit(m.startWith(dir, true), w-3)),
		"  " + faint(shortPath(tildify(dir), w-3)),
	}
	keys = []string{"", "", footNextKey, footNextKey}
	m.footHits = m.footHits[:0]
	cur := m.nextStart(dir)
	line, x := "", 2
	for i, o := range m.recentSetups(cur) {
		word := cmp.Or(modelWord(o.kind, o.model), agent.HarnessLabel(agent.Kind(o.kind)))
		if o.kind != cur.kind && o.model != "" {
			word = strings.ToLower(agent.HarnessLabel(agent.Kind(o.kind))) + "·" + word
		}
		if o.effort != "" {
			word += "·" + o.effort
		}
		seg := strconv.Itoa(i+1) + " " + word
		if x+cellw.String(seg) > w-1 {
			break
		}
		m.footHits = append(m.footHits, footHit{x, x + cellw.String(seg), o})
		line += "  " + dim(strconv.Itoa(i+1)) + " " + paint(cText, word)
		x += cellw.String(seg) + 2
	}
	if line != "" {
		lines, keys = append(lines, line), append(keys, footRecentKey)
	}
	return lines, keys
}

// recentSetups are up to 3 setups the fleet's agents ran, newest first,
// each once, cur left out.
func (m *Model) recentSetups(cur startOver) []startOver {
	if m.snap == nil {
		return nil
	}
	as := slices.Clone(m.snap.Agents)
	slices.SortStableFunc(as, func(a, b *fleet.Agent) int { return b.CreatedAt.Compare(a.CreatedAt) })
	seen := []string{strings.Join(m.startWords(cur), " · ")}
	var out []startOver
	for _, a := range as {
		if a.Advisor || len(out) == 3 {
			continue
		}
		k := cmp.Or(a.Kind, string(a.Acct.Kind))
		if k == "" {
			continue
		}
		o := m.startDefaults(k)
		if a.Spend.Model != "" {
			o.model = a.Spend.Model
		}
		if c := m.host; c != nil && c.key == a.Key && c.sess != nil {
			o = m.sessionStart(c)
		}
		if w := strings.Join(m.startWords(o), " · "); !slices.Contains(seen, w) {
			seen, out = append(seen, w), append(out, o)
		}
	}
	return out
}

// footClick takes the recent setup at column x as the next agent's.
func (m *Model) footClick(x int) {
	for _, h := range m.footHits {
		if x >= h.x0 && x < h.x1 {
			o := h.o
			m.startOver = &o
			m.flash("the next session starts as "+m.startWith(m.startDir(), true), false)
		}
	}
}
