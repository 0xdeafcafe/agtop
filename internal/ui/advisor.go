package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/advisor"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

// advState is the advisor as the Efficiency place sees it: its record,
// and whether a pass is running.
type advState struct {
	rec     *advisor.Record
	running bool
	checked time.Time // when the tick last asked whether a pass is due
}

type advRanMsg struct {
	store   *efficiency.Store
	skipped bool // not due: nothing ran
	res     advisor.Result
}

// advCheck is how often the tick asks whether a pass is due; the question
// itself costs a scan of new transcript lines.
const advCheck = 10 * time.Minute

func (m *Model) advRecord() *advisor.Record {
	a := &m.eff.adv
	if a.rec == nil {
		a.rec = advisor.Load()
	}
	return a.rec
}

// advTick starts a pass when the advisor is on and one is due.
func (m *Model) advTick() tea.Cmd {
	if !m.store.Config.Advisor || m.solo != "" {
		return nil
	}
	a := &m.eff.adv
	rec := m.advRecord()
	if a.running || time.Since(a.checked) < advCheck || !rec.LastRun.IsZero() && time.Since(rec.LastRun) < advisor.Gap {
		return nil
	}
	a.checked = time.Now()
	return m.advRun(false)
}

// advRun runs a pass: when it's due, or regardless when force is set.
func (m *Model) advRun(force bool) tea.Cmd {
	a := &m.eff.adv
	acct, ok := advisor.Account(m.store.Config)
	if a.running || !ok {
		return nil
	}
	a.running = true
	rec := m.advRecord()
	since, known, pending, reviews := rec.LastRun, rec.Known(), rec.Pending(), rec.ReviewsLeft(time.Now())
	due := advisor.Record{LastRun: since} // the goroutine's own, not the one the UI changes
	store := m.eff.store
	return func() tea.Msg {
		if store == nil {
			store = efficiency.Open()
		}
		store.Refresh([]claude.Account{acct})
		now := time.Now()
		week := efficiency.NewQuery(efficiency.Ranges[1], now)
		week.Accounts = []string{acct.ConfigDir}
		fresh := week
		if since.After(fresh.From) {
			fresh.From, fresh.Daily = since, false
		}
		if !force && !due.Due(now, store.View(fresh).Total.Req) {
			return advRanMsg{store: store, skipped: true}
		}
		v := store.View(week)
		found := efficiency.Observe(efficiency.LoadEnv(acct))
		in := advisor.Input{View: v, Found: found, Findings: efficiency.Findings(v, found), Top: store.TopSessions(week, 8), Known: known, Pending: pending}
		return advRanMsg{store: store, res: advisor.Pass(context.Background(), acct, in, reviews)}
	}
}

func (m *Model) onAdvRan(msg advRanMsg) {
	a := &m.eff.adv
	a.running = false
	if m.eff.store == nil {
		m.eff.store = msg.store
	}
	if msg.skipped {
		return
	}
	rec := m.advRecord()
	fresh := rec.Merge(msg.res)
	_ = rec.Save()
	m.eff.findings = m.withAdvice(m.eff.base)
	switch {
	case len(fresh) > 0:
		m.flash("✦ advisor: "+fresh[0].Title+" · #eff findings", false)
	case msg.res.Err != nil && m.mode == modeEff:
		m.flash("advisor: "+msg.res.Err.Error(), true)
	}
}

// withAdvice is the Efficiency place's findings with the advisor's: those
// Opus confirmed first, Haiku's unchecked guesses last, with no figure,
// since Haiku's are rough.
func (m *Model) withAdvice(base []efficiency.Finding) []efficiency.Finding {
	if !m.store.Config.Advisor {
		return base
	}
	var top, rest []efficiency.Finding
	for _, f := range m.advRecord().Shown() {
		ef := efficiency.Finding{Title: "✦ " + f.Title, Fix: f.Fix, Open: f.Open, Advice: f.ID}
		var d []string
		if f.Detail != "" {
			d = append(d, f.Detail)
		}
		if f.Status == advisor.Confirmed {
			if f.Weekly > 0 {
				d = append(d, "≈"+efficiency.Money(f.Weekly)+" a week.")
			}
			ef.Detail = strings.Join(append(d, "Checked by Opus: "+f.Note), " ")
			top = append(top, ef)
		} else {
			ef.Detail = strings.Join(append(d, "Haiku's guess, not checked yet."), " ")
			rest = append(rest, ef)
		}
	}
	out := append(top, base...)
	return append(out, rest...)
}

// advDismiss puts the chosen advisor finding away for good.
func (m *Model) advDismiss() {
	e := &m.eff
	if e.finding >= len(e.findings) || e.findings[e.finding].Advice == "" {
		return
	}
	rec := m.advRecord()
	rec.Dismiss(e.findings[e.finding].Advice)
	_ = rec.Save()
	e.findings = m.withAdvice(e.base)
	e.finding = min(e.finding, max(0, len(e.findings)-1))
	m.flash("put away · the advisor won't raise it again", false)
}

// advLine is the advisor's state, over the findings.
func (m *Model) advLine() string {
	if !m.store.Config.Advisor {
		return paint(cOrange, "✦ ") + dim("The advisor is off. ") + paint(cOrange+bold, "#advisor on") + dim(" lets Haiku look over these figures now and then, with Opus checking what it finds.")
	}
	rec := m.advRecord()
	parts := []string{paint(cOrange, "✦ ") + paint(cText, "Advisor")}
	switch {
	case m.eff.adv.running:
		parts = append(parts, paint(cOrange, spinner[m.tick%len(spinner)])+dim(" looking…"))
	case rec.LastRun.IsZero():
		parts = append(parts, dim("looks once your agents have done some work"))
	default:
		parts = append(parts, dim("looked "+age(time.Since(rec.LastRun))+" ago"))
	}
	if rec.Spent > 0 {
		parts = append(parts, dim("it has cost "+efficiency.Money(rec.Spent)))
	}
	if rec.Err != "" {
		parts = append(parts, paint(cYellow, "last pass: "+rec.Err))
	}
	return strings.Join(parts, faint(" · ")) + faint("  #advisor now · off")
}

// advCommand is #advisor.
func (m *Model) advCommand(arg string) tea.Cmd {
	c := &m.store.Config
	switch strings.TrimSpace(arg) {
	case "on":
		c.Advisor = true
		_ = m.store.SaveConfig()
		m.didStep("advisor")
		m.flash("✦ advisor on · it looks after your agents have done some work, at most every 3h", false)
		m.eff.findings = m.withAdvice(m.eff.base)
		m.eff.adv.checked = time.Now()
		return m.advRun(false)
	case "off":
		c.Advisor = false
		_ = m.store.SaveConfig()
		m.eff.findings = m.withAdvice(m.eff.base)
		m.flash("advisor off", false)
	case "now":
		if !c.Advisor {
			m.flash("the advisor is off · #advisor on", true)
			return nil
		}
		if m.eff.adv.running {
			m.flash("the advisor is already looking", false)
			return nil
		}
		m.flash("✦ advisor looking · its findings land in #eff findings", false)
		return m.advRun(true)
	case "":
		if !c.Advisor {
			m.flash("the advisor is off · #advisor on", false)
			return nil
		}
		rec := m.advRecord()
		m.flash(fmt.Sprintf("✦ advisor on · %d findings · it has cost %s · #advisor now · off", len(rec.Shown()), efficiency.Money(rec.Spent)), false)
	default:
		m.flash("#advisor on, off or now", true)
	}
	return nil
}
