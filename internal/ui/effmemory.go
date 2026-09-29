package ui

import "github.com/0xdeafcafe/agtop/internal/agent"

// effMemoryPlace is the account and folder whose memory Efficiency's
// findings look at: the selected agent's, or the active account's in the
// folder agtop was started in. ok is false when that agent keeps no
// memory agtop reads.
func (m *Model) effMemoryPlace() (p agent.Profile, cwd string, ok bool) {
	p = m.store.Config.ActiveAccount().Profile()
	if a := m.selected(); a != nil && a.Acct.Dir != "" {
		p = a.Acct
		p.Kind = agent.Kind(a.Kind)
	}
	return p, m.effFolder(), agent.Supports(p.Kind, agent.FeatureMemory)
}
