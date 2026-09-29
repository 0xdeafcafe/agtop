package ui

// effMemoryPlace is the account and folder whose memory Efficiency's
// findings look at: the selected agent's, or the active account's in the
// folder agtop was started in.
func (m *Model) effMemoryPlace() (cfg, cwd string) {
	cfg = m.store.Config.ActiveAccount().ConfigDir
	if a := m.selected(); a != nil && a.Acct.Dir != "" {
		cfg = a.Acct.Dir
	}
	return cfg, m.effFolder()
}
