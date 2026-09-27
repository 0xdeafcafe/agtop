package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// quotaMsg is the limits of the account another agent's profile is signed
// in to, as just read.
type quotaMsg struct {
	dir string
	q   usage.Quota
}

// otherProfiles are the config homes of every agent but Claude Code, which
// Accounts lists by its logins and folders instead.
func otherProfiles() []agent.Profile {
	var out []agent.Profile
	for _, a := range agent.All() {
		if a.Kind() == "claude" {
			continue
		}
		out = append(out, a.Profiles()...)
	}
	return out
}

// fetchQuotas reads the limits of every other agent's account that can say
// them. Readings are shared by every agtop and session, and a hosted
// session's live ones land there too, so each account is asked at most
// every few minutes.
func (m *Model) fetchQuotas() tea.Cmd {
	offline := m.offline
	var cmds []tea.Cmd
	for _, p := range otherProfiles() {
		ad, _ := agent.Get(p.Kind)
		src, ok := ad.(agent.QuotaSource)
		if !ok {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			q := usage.Refresh(host.QuotasPath(), host.QuotaKey(p), offline, func(ctx context.Context) (usage.Quota, error) {
				return src.Quota(ctx, p, agent.Account{Kind: p.Kind})
			})
			return quotaMsg{dir: p.Dir, q: q}
		})
	}
	return tea.Batch(cmds...)
}

// onQuota keeps a reading.
func (m *Model) onQuota(msg quotaMsg) {
	if m.quotas == nil {
		m.quotas = map[string]usage.Quota{}
	}
	m.quotas[msg.dir] = msg.q
}
