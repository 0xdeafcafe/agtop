package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// quotaMsg is readings of other agents' limits, by where they're kept: a
// profile folder for the account it's signed in to, or an account's key.
type quotaMsg map[string]usage.Quota

// fetchQuotas reads the limits of every other installed agent's accounts
// that can say them, and who each is signed in as. Readings are shared by
// every agtop and session, and a hosted session's live ones land there
// too, so each account is asked at most every few minutes. An account not
// in use keeps the last reading made while it was.
func (m *Model) fetchQuotas() tea.Cmd {
	offline := m.offline
	cfg := m.store.Config
	cmds := []tea.Cmd{m.findSignIns(), func() tea.Msg { return quotaMsg(usage.Load(host.QuotasPath())) }}
	for _, ad := range agent.InstalledAll() {
		if ad.Kind() == "claude" {
			continue
		}
		src, ok := ad.(agent.QuotaSource)
		p, found := m.profileOf(ad)
		if !ok || !found {
			continue
		}
		if _, any := ad.(agent.AnyAccountQuota); any && len(cfg.SignInsOf(string(ad.Kind()))) > 0 {
			for _, s := range cfg.SignInsOf(string(ad.Kind())) {
				a := signInAccount(s)
				cmds = append(cmds, func() tea.Msg {
					q := usage.Refresh(host.QuotasPath(), a.Key, offline, func(ctx context.Context) (usage.Quota, error) {
						return src.Quota(ctx, p, a)
					})
					return quotaMsg{a.Key: q}
				})
			}
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			q := usage.Refresh(host.QuotasPath(), host.QuotaKey(p), offline, func(ctx context.Context) (usage.Quota, error) {
				return src.Quota(ctx, p, agent.Account{Kind: p.Kind})
			})
			out := quotaMsg{p.Dir: q}
			if q.Account != "" && q.Problem == "" && !q.FetchedAt.IsZero() {
				// Kept as the account's too, for when it's no longer the
				// one in use.
				out[q.Account] = q
				if old, ok := usage.Load(host.QuotasPath())[q.Account]; !ok || old.FetchedAt.Before(q.FetchedAt) {
					_ = usage.Record(host.QuotasPath(), q.Account, q)
				}
			}
			return out
		})
	}
	return tea.Batch(cmds...)
}

// applyTo keeps the newest of each reading.
func (msg quotaMsg) applyTo(m *Model) tea.Cmd {
	if m.quotas == nil {
		m.quotas = map[string]usage.Quota{}
	}
	for k, q := range msg {
		if old, ok := m.quotas[k]; ok && old.FetchedAt.After(q.FetchedAt) {
			continue
		}
		m.quotas[k] = q
	}
	return m.checkLimits()
}

// fresh is how old a reading of the account in use may be and still say
// it's nearly out.
const fresh = 15 * time.Minute

// checkLimits switches another agent to another of its accounts when the
// one in use is nearly out, and, when you let it move on to the next
// agent, picks which agent new sessions run.
func (m *Model) checkLimits() tea.Cmd {
	cfg := m.store.Config
	if m.offline || cfg.SwitchOnLimit == state.OnLimitOff {
		m.accts.spill = ""
		return nil
	}
	m.accts.ready()
	rows := m.accountRows()
	var cmds []tea.Cmd
	for _, ad := range m.agentOrder() {
		k := ad.Kind()
		if k == "claude" || !switches(k) || time.Since(m.accts.switchedAt[string(k)]) < switchGap {
			continue
		}
		if to, why, ok := nextAccount(accountsOf(rows, k)); ok {
			m.accts.switchedAt[string(k)] = time.Now()
			cmds = append(cmds, m.switchAccount(to.acct, why))
		}
	}
	m.spillTo(rows)
	return tea.Batch(cmds...)
}

// nextAccount is the account to switch to when the one in use is nearly
// out: the one with the most room, as long as it isn't nearly out itself.
func nextAccount(accts []acctRow) (acctRow, string, bool) {
	var cur *acctRow
	var others []acctRow
	for i, r := range accts {
		switch {
		case r.current:
			cur = &accts[i]
		case len(r.q.Windows) > 0 && time.Since(r.q.FetchedAt) < time.Hour:
			others = append(others, r)
		}
	}
	if cur == nil || len(others) == 0 || !nearlyOut(cur.q) {
		return acctRow{}, "", false
	}
	best := others[0]
	for _, o := range others[1:] {
		if o.q.Used("") < best.q.Used("") {
			best = o
		}
	}
	if best.q.Used("") >= state.SwitchAt {
		return acctRow{}, "", false
	}
	return best, cur.name() + " was at " + pct(cur.q.Used("")), true
}

// nearlyOut is whether a fresh reading says the account is nearly out.
func nearlyOut(q usage.Quota) bool {
	return len(q.Windows) > 0 && time.Since(q.FetchedAt) < fresh && q.Used("") >= state.SwitchAt
}

// agentOut is whether every account of agent k agtop has a reading of is
// nearly out: its default can't start anything.
func agentOut(rows []acctRow, k agent.Kind) bool {
	accts := accountsOf(rows, k)
	if len(accts) == 0 {
		for _, r := range rows {
			if r.head && r.kind == k {
				return nearlyOut(r.q)
			}
		}
		return false
	}
	for _, r := range accts {
		if !nearlyOut(r.q) {
			return false
		}
	}
	return true
}

// spillTo picks the agent new sessions run while the default's accounts
// are all nearly out: the next in your order that isn't. Running sessions
// stay where they are.
func (m *Model) spillTo(rows []acctRow) {
	cfg := m.store.Config
	was := m.accts.spill
	m.accts.spill = ""
	def := cfg.DefaultAgent()
	if cfg.SwitchOnLimit == state.OnLimitAgent && (agentOut(rows, agent.Kind(def)) || !agent.Installed(agent.Kind(def))) {
		for _, ad := range m.agentOrder() {
			if k := ad.Kind(); string(k) != def && !agentOut(rows, k) {
				if _, ok := ad.(agent.Driver); ok && agent.Runs(k) {
					m.accts.spill = string(k)
					break
				}
			}
		}
	}
	switch {
	case m.accts.spill == was:
	case m.accts.spill != "":
		m.flash(agentName(def)+"'s accounts are all nearly out · new sessions run "+agentName(m.accts.spill)+" until they reset", false)
	default:
		m.flash("new sessions run "+agentName(def)+" again", false)
	}
}

func pct(p float64) string { return fmt.Sprintf("%.0f%%", p) }
