package ui

import (
	"context"
	"errors"
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
	checked time.Time          // when the tick last asked whether a pass is due
	cancel  context.CancelFunc // stops the pass running
}

type advRanMsg struct {
	rec   *advisor.Record // as saved, when a pass ran
	fresh []advisor.Finding
	err   error
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

// advTick starts a pass when the advisor is on and one may be due.
func (m *Model) advTick() tea.Cmd {
	if !m.store.Config.Advisor || m.hosted != "" || m.offline {
		return nil
	}
	a := &m.eff.adv
	if a.checked.IsZero() {
		a.checked = time.Now() // not while agtop is still starting up
	}
	if a.running || time.Since(a.checked) < advCheck {
		return nil
	}
	a.checked = time.Now()
	if !advisor.Enabled() {
		// Turned off in another agtop: don't run, and don't write it back
		// on when this one saves its config.
		m.store.Config.Advisor = false
		return nil
	}
	// Read again: another agtop may have run one since.
	a.rec = advisor.Load()
	if !a.rec.LastRun.IsZero() && time.Since(a.rec.LastRun) < advisor.Gap {
		return nil
	}
	cmd, _ := m.advRun(false)
	return cmd
}

// advRun runs a pass: when it's due, or regardless when force is set. It
// says why not when it can't start one.
func (m *Model) advRun(force bool) (tea.Cmd, string) {
	a := &m.eff.adv
	if a.running {
		return nil, "the advisor is already looking"
	}
	if m.offline {
		return nil, "the advisor doesn't run offline"
	}
	// It runs Claude Code, as whoever ~/.claude is signed in as: only when
	// the profile here lists Claude Code and one of its logins has room.
	// Another login can't be used without switching ~/.claude.
	_, ok := m.store.Config.ProfileFor(m.launchDir, "").PickFor(string(loginsKind), m.room())
	acct := m.store.Config.ActiveAccount()
	if !ok || acct.ConfigDir == "" {
		return nil, "the advisor runs on Claude Code: it isn't in this profile, or every login is nearly out"
	}
	a.running = true
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	// The Efficiency place's figures when it has them; otherwise read for
	// this pass and let go after, so the advisor keeps nothing in memory.
	store := m.eff.store
	return func() tea.Msg {
		defer cancel()
		unlock, ok := advisor.Lock()
		if !ok {
			if force {
				return advRanMsg{err: errors.New("another agtop is already looking")}
			}
			return advRanMsg{} // another agtop is running one
		}
		defer unlock()
		rec := advisor.Load()
		if !force && (!rec.LastRun.IsZero() && time.Since(rec.LastRun) < advisor.Gap || !advisor.Active(acct, rec.LastRun)) {
			return advRanMsg{}
		}
		if store == nil {
			store = efficiency.Open()
		}
		store.Refresh([]claude.Account{acct})
		now := time.Now()
		week := efficiency.NewQuery(efficiency.Ranges[1], now)
		week.Accounts = []string{acct.ConfigDir}
		fresh := week
		if rec.LastRun.After(fresh.From) {
			fresh.From, fresh.Daily = rec.LastRun, false
		}
		if !force && !rec.Due(now, store.View(fresh).Total.Req) {
			return advRanMsg{}
		}
		v := store.View(week)
		if v.Total.Req == 0 {
			return advRanMsg{err: errors.New("nothing to look at in the last week")}
		}
		if err := advisor.Begin(now); err != nil {
			return advRanMsg{err: err}
		}
		found := efficiency.Observe(efficiency.LoadEnv(acct))
		in := advisor.Input{View: v, Found: found, Findings: efficiency.Findings(v, found), Top: store.TopSessions(week, 8),
			Known: rec.Known(), Settled: rec.Settled(), Pending: rec.Pending(),
			Reserve: func() error { return advisor.Reserve(time.Now()) }}
		res := advisor.Pass(ctx, acct, in, rec.ReviewsLeft(now))
		// Merged into what's on disk now, under the lock: you may have put
		// one away while it ran.
		rec = advisor.Load()
		got := rec.Merge(res)
		if err := rec.Save(); err != nil && res.Err == nil {
			res.Err = err
		}
		return advRanMsg{rec: rec, fresh: got, err: res.Err}
	}, ""
}

func (m *Model) onAdvRan(msg advRanMsg) {
	a := &m.eff.adv
	a.running, a.cancel = false, nil
	if msg.rec == nil {
		if msg.err != nil {
			m.flash("advisor: "+msg.err.Error(), true)
		}
		return // nothing ran
	}
	a.rec = msg.rec
	m.advRefresh()
	switch {
	case len(msg.fresh) > 0:
		m.flash("✦ advisor: "+msg.fresh[0].Title+" · #eff findings", false)
	case msg.err != nil && m.mode == modeEff:
		m.flash("advisor: "+msg.err.Error(), true)
	}
}

// advRefresh puts the advisor's findings in again, keeping the one chosen:
// new ones go first, and x mustn't put away one you didn't pick.
func (m *Model) advRefresh() {
	e := &m.eff
	var was string
	if e.finding < len(e.findings) {
		was = e.findings[e.finding].Title
	}
	e.findings = m.withAdvice(e.base)
	e.finding = min(e.finding, max(0, len(e.findings)-1))
	for i, f := range e.findings {
		if f.Title == was {
			e.finding = i
		}
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
	rec := advisor.Load() // another agtop may have added to it
	rec.Dismiss(e.findings[e.finding].Advice)
	_ = rec.Save()
	m.eff.adv.rec = rec
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
		m.advRefresh()
		m.eff.adv.checked = time.Now()
		cmd, why := m.advRun(false)
		if why != "" {
			m.flash("✦ advisor on · but "+why, true)
			return nil
		}
		m.flash("✦ advisor on · it looks after your agents have done some work, at most every 3h", false)
		return cmd
	case "off":
		c.Advisor = false
		_ = m.store.SaveConfig()
		m.didStep("advisor") // a no is an answer too: Getting started stops asking
		if cancel := m.eff.adv.cancel; cancel != nil {
			cancel() // what it has already spent is counted; nothing more
		}
		m.advRefresh()
		m.flash("advisor off", false)
	case "now":
		if !c.Advisor {
			m.flash("the advisor is off · #advisor on", true)
			return nil
		}
		cmd, why := m.advRun(true)
		if why != "" {
			m.flash(why, true)
			return nil
		}
		m.flash("✦ advisor looking · its findings land in #eff findings", false)
		return cmd
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
