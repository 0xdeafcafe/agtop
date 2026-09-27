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
	cfg := host.Config{}
	if err := cfg.UseAgent(kind); err != nil {
		m.flash(err.Error(), true)
		return
	}
	if kind == "claude" {
		kind = ""
	}
	d.Kind = kind
	_ = m.store.SaveConfig()
	m.flash("new sessions run "+agentName(kind), false)
}

// agentName is an agent's name, by its kind; empty is Claude Code.
func agentName(kind string) string {
	if kind == "" {
		kind = "claude"
	}
	if a, ok := agent.Get(agent.Kind(kind)); ok {
		return a.Name()
	}
	return kind
}
