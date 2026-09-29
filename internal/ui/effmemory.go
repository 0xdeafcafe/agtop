package ui

import "github.com/0xdeafcafe/agtop/internal/agent"

// effMemoryPlace is the account and folder whose memory Efficiency's
// findings look at: the selected agent's, or the active account's in the
// folder agtop was started in. ok is false when that agent keeps no
// memory agtop reads.
func (m *Model) effMemoryPlace() (cfg, cwd string, ok bool) {
	cfg, kind := m.store.Config.ActiveAccount().ConfigDir, loginsKind
	if a := m.selected(); a != nil && a.Acct.Dir != "" {
		cfg, kind = a.Acct.Dir, agent.Kind(a.Kind)
	}
	return cfg, m.effFolder(), agent.Supports(kind, agent.FeatureMemory)
}
