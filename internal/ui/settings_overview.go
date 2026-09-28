package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// Overview is every installed provider at a glance: the account each is
// on and its limits, what its sessions cost today and this week, how many
// run, and what needs you: an account nearly out, or one agtop can't
// read. enter goes to the provider's own page.

// provUse is one provider's line of Overview.
type provUse struct {
	kind           agent.Kind
	inUse          acctRow // the account in use, or the provider itself
	accts, out     int     // accounts kept, and how many are nearly out
	running, today int     // sessions running, and active today
	spentToday     float64
	spentWeek      float64 // by sessions active in the last 7 days
	priced         bool
}

func (m *Model) providerUse() []provUse {
	rows := m.accountRows()
	var out []provUse
	for _, ad := range m.agentOrder() {
		k := ad.Kind()
		u := provUse{kind: k, priced: agent.Supports(k, agent.FeaturePricing)}
		for _, r := range rows {
			switch {
			case r.kind != k:
			case r.head:
				if u.inUse.kind == "" {
					u.inUse = r
				}
			default:
				u.accts++
				if nearlyOut(r.q) {
					u.out++
				}
				if r.current {
					u.inUse = r
				}
			}
		}
		out = append(out, u)
	}
	now := m.snap.At
	for _, a := range m.snap.Agents {
		for i := range out {
			if state.KindOf(a.Kind) != string(out[i].kind) {
				continue
			}
			u := &out[i]
			if a.Live() {
				u.running++
			}
			if a.Spend.Today > 0 || sameDay(a.Spend.Last, now) {
				u.today++
			}
			u.spentToday += a.Spend.Today
			if now.Sub(a.Spend.Last) < 7*24*time.Hour {
				u.spentWeek += a.Spend.Cost
			}
		}
	}
	return out
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func (m *Model) overviewLen() int { return len(m.agentOrder()) }

func (m *Model) overviewKey(s string) tea.Cmd {
	switch s {
	case "enter", "right", "l":
		if order := m.agentOrder(); m.dialog.cursor < len(order) {
			m.showAgentSettings(order[m.dialog.cursor].Kind())
		}
	case "a":
		m.setSettingsPage(pageProviders)
	case "r":
		m.flash("reading every provider's limits again…", false)
		return tea.Batch(m.fetchUsage(), m.fetchQuotas())
	}
	return nil
}

func (m *Model) overviewBody(w int) []string {
	d := m.dialog
	use := m.providerUse()
	var out []string

	// Totals first.
	var running, today int
	var spentToday, spentWeek float64
	for _, u := range use {
		running += u.running
		today += u.today
		spentToday += u.spentToday
		spentWeek += u.spentWeek
	}
	out = append(out, paint(cText, money(spentToday))+dim(" today")+faint(" · ")+
		paint(cText, money(spentWeek))+dim(" this week")+faint(" · ")+
		paint(cText, fmt.Sprint(running))+dim(" running")+faint(" · ")+
		paint(cText, fmt.Sprint(today))+dim(" sessions today"), "")

	// A line per provider.
	cols := []int{22, 24, 19, 19, 9, 9, 8}
	if w < 124 {
		cols[3] = 0
	}
	cols[1] = max(10, min(28, w-4-cols[0]-cols[2]-cols[3]-cols[4]-cols[5]-cols[6]))
	out = append(out, "  "+faint(fit("PROVIDER", cols[0])+fit("ON", cols[1])+fit("LIMITS", cols[2]+cols[3])+
		right("TODAY", cols[4])+right("WEEK", cols[5])+right("RUNNING", cols[6])))
	if len(use) == 0 {
		out = append(out, "", dim("  No coding agent is installed where agtop looks: install Claude Code, Codex or another, and it shows here."))
	}
	for i, u := range use {
		star := " "
		if string(u.kind) == m.store.Config.DefaultAgent() {
			star = paint(cOrange, "★")
		}
		name := glyph(u.kind) + " " + paint(cText+bold, fit(kindName(u.kind), cols[0]-5)) + star + " "
		on := u.inUse.name()
		if u.inUse.head {
			on = firstNonEmpty(u.inUse.q.Email, "its own sign-in")
		}
		if u.accts > 1 {
			on += fmt.Sprintf(" (%d)", u.accts)
		}
		spend := func(v float64, w int) string {
			if !u.priced && v == 0 {
				return faint(right("—", w))
			}
			return paint(cText, right(money(v), w))
		}
		run := faint(right("·", cols[6]))
		if u.running > 0 {
			run = paint(cSub, right(fmt.Sprint(u.running), cols[6]))
		}
		line := name + dim(fit(on, cols[1])) + m.limits(u.inUse, cols[2], cols[3]) + spend(u.spentToday, cols[4]) + spend(u.spentWeek, cols[5]) + run
		if i == d.cursor {
			out = append(out, highlight(paint(cOrange, "▍")+" "+line, w))
		} else {
			out = append(out, "  "+line)
		}
	}

	// This week's spend, as bars.
	var top float64
	for _, u := range use {
		top = max(top, u.spentWeek)
	}
	if top > 0 {
		out = append(out, "", rule("This week", "sessions active in the last 7 days", w))
		for _, u := range use {
			if u.spentWeek == 0 {
				continue
			}
			n := max(1, int(u.spentWeek/top*30))
			out = append(out, "  "+fit(kindName(u.kind), 12)+paint(lookOf(u.kind).colour(), strings.Repeat("█", n))+" "+paint(cText, money(u.spentWeek)))
		}
	}

	// What needs you.
	out = append(out, "", rule("Needs you", "", w))
	var needs []string
	for _, r := range m.accountRows() {
		if r.head && switches(r.kind) {
			continue // its accounts speak for it
		}
		who := kindName(r.kind)
		if !r.head {
			who += " · " + r.name()
		}
		switch {
		case r.q.Problem != "" && len(r.q.Windows) == 0:
			needs = append(needs, paint(cYellow, "! ")+paint(cText, who)+dim(": "+r.q.Problem))
		case nearlyOut(r.q):
			win, _ := r.q.Tightest("")
			s := paint(cYellow, "! ") + paint(cText, who) + dim(fmt.Sprintf(": %s at %.0f%%", firstNonEmpty(win.Name, win.Label), win.Percent))
			if !win.ResetsAt.IsZero() && win.ResetsAt.After(m.snap.At) {
				s += dim(" · resets in " + dur(win.ResetsAt.Sub(m.snap.At)))
			}
			if r.current {
				s += faint(" · in use")
			}
			needs = append(needs, s)
		}
	}
	if sp := m.accts.spill; sp != "" {
		needs = append(needs, paint(cYellow, "→ ")+dim("new sessions run ")+paint(cText, agentName(sp))+dim(" for now: the first provider's accounts are all nearly out"))
	}
	if len(needs) == 0 {
		needs = append(needs, paint(cGreen, "✓ ")+dim("every account agtop can read has room"))
	}
	for _, l := range needs {
		out = append(out, "  "+l)
	}

	def := m.store.Config.Default()
	out = append(out, "", dim("New sessions: ")+paint(cText, m.startAccount())+dim("   ·   profile ")+paint(cText, def.Name)+dim(": ")+m.chain(def)+dim(" · at a limit: ")+paint(cText, limitWords(def.Limit())))
	out = append(out, "", keysFit(w, append([]string{"enter", "its settings", "a", "accounts", "r", "read limits again"}, pagesKeys...)...))
	for i, l := range out {
		if cellw.String(ansi.Strip(l)) > w {
			out[i] = ansi.Truncate(l, w-1, "…")
		}
	}
	return out
}
