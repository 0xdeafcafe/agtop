package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// agentHistory is another agent's session as its history tells it, up to
// before (or all of it, when before is zero); empty when its adapter
// can't read one.
func agentHistory(kind agent.Kind, s agent.Session, before time.Time) *convo.Session {
	sess := convo.New()
	a, ok := agent.Get(kind)
	if !ok {
		return sess
	}
	hr, ok := a.(agent.HistoryReader)
	if !ok {
		return sess
	}
	evs, err := hr.History(s, before)
	if err != nil {
		return sess
	}
	for _, ev := range evs {
		sess.Apply(ev, time.Time{})
	}
	return sess
}

// openHistory shows a past session of another agent, read once: a message
// resumes it, and the pane then follows the host.
func openHistory(a *fleet.Agent) tea.Cmd {
	key, id := a.Key, a.ID
	s := agent.Session{ID: a.SessionID, Transcript: a.History, Profile: agent.Profile{Kind: agent.Kind(a.Kind), Dir: a.Acct.ConfigDir}}
	return func() tea.Msg {
		sess := agentHistory(agent.Kind(a.Kind), s, time.Time{})
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, sess: sess, open: map[string]bool{}, ready: true}}
	}
}
