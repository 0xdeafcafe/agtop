package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/host"
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

// modelWord is model id as the agent of kind names it: "Opus 5.5".
func modelWord(kind, id string) string {
	k := agent.Kind(kind)
	if k == "" {
		k = agent.LegacyKind
	}
	return agent.ModelName(k, id)
}

// agentName is an agent's name, by its kind.
func agentName(kind string) string {
	k := agent.Kind(kind)
	if a, ok := agent.Get(k); ok {
		return a.Name()
	}
	return string(k)
}
