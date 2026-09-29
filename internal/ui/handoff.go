package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// conversationOf is an agent row's conversation, told for a hand-off: the
// open pane's when it's this one, else read from its transcript or through
// its adapter.
func (m *Model) conversationOf(a *fleet.Agent) agent.Conversation {
	kind := agent.Kind(a.Kind)
	var sess *convo.Session
	switch {
	case m.host != nil && m.host.key == a.Key:
		sess = m.host.sess
	case a.TranscriptPath != "":
		sess = convo.History(a.TranscriptPath, time.Time{})
	default:
		sess = agentHistory(kind, agent.Session{ID: a.SessionID, Name: a.DisplayName, Transcript: a.History,
			Profile: agent.Profile{Kind: kind, Dir: a.Acct.Dir}}, time.Time{})
	}
	c := sess.Conversation(kind)
	if c.Name == "" {
		c.Name = a.DisplayName
	}
	if c.Cwd == "" {
		c.Cwd = a.Cwd
	}
	return c
}

// handoffTargets are the agents a conversation on from can be handed to:
// installed, able to run here, and able to start from another's.
func handoffTargets(from agent.Kind) []agent.Adapter {
	var out []agent.Adapter
	for _, a := range host.Installed() {
		if a.Kind() != from && agent.Supports(a.Kind(), agent.FeatureHandoffIn) {
			out = append(out, a)
		}
	}
	return out
}

// handoffTo starts a new session on agent to, opened with this one's
// conversation so far. This one is left as it is.
func (m *Model) handoffTo(c *hostConn, a *fleet.Agent, to string) tea.Cmd {
	from := sessionAgent(c)
	if to == "" {
		var names []string
		for _, t := range handoffTargets(from) {
			names = append(names, string(t.Kind()))
		}
		if len(names) == 0 {
			m.flash("no other installed agent can take this conversation on", true)
			return nil
		}
		m.flash("/handoff to which agent? "+strings.Join(names, ", "), false)
		return nil
	}
	k := agent.Kind(strings.ToLower(to))
	if k == from {
		m.flash("this conversation is already on "+agentName(string(k)), true)
		return nil
	}
	if !agent.Supports(k, agent.FeatureHandoffIn) {
		m.flash(agentName(string(k))+" can't take a conversation on from another agent", true)
		return nil
	}
	cfg := host.Config{Cwd: a.Cwd, Name: a.DisplayName + " · on " + agentName(string(k)),
		IdleStop: host.Duration(m.store.Config.Dispatch.Rest())}
	if err := cfg.UseAgent(string(k)); err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	if agent.IsBuiltin(k) {
		cfg.Account = m.store.Config.ActiveAccount().Profile()
	}
	cfg.Prompt = agent.Handoff(m.conversationOf(a)).Text
	m.flash("handing "+a.DisplayName+" to "+agentName(string(k))+"…", false)
	return func() tea.Msg {
		hc, err := host.Spawn(cfg)
		if err != nil {
			return doneMsg{err: err}
		}
		return hostStartedMsg{id: hc.ID, name: cfg.Name}
	}
}
