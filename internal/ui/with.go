package ui

import (
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// withAgent sets the agent new sessions run, or, with no agent named, says
// which it is and what else is installed.
func (m *Model) withAgent(kind string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	d := &m.store.Config.Dispatch
	if kind == "" {
		var names []string
		for _, a := range host.Installed() {
			names = append(names, string(a.Kind()))
		}
		m.flash("new sessions run "+agentName(d.Kind)+" · installed: "+strings.Join(names, ", "), false)
		return
	}
	if k := agent.Kind(kind); agent.Installed(k) && !agent.Runs(k) && agent.Hint(k) != "" {
		// There, but only for what it can do without its own program.
		m.flash(agentName(kind)+" can't run sessions here yet: "+agent.Hint(k), false)
		return
	}
	cfg := host.Config{}
	if err := cfg.UseAgent(kind); err != nil {
		m.flash(err.Error(), true)
		return
	}
	// New sessions run it: first in the default profile.
	m.store.Config.SetDefaultProvider(kind)
	_ = m.store.SaveConfig()
	m.flash("new sessions run "+agentName(kind), false)
}

// agentName is an agent's name, by its kind; empty is the built-in one.
func agentName(kind string) string {
	k := agent.KindOf(kind)
	if a, ok := agent.Get(k); ok {
		return a.Name()
	}
	return string(k)
}
