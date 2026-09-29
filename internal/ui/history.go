package ui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
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

// openHistory shows another agent's session from its history: a past one,
// which a message resumes (the pane then follows the host), or one running
// in a terminal, read again as it grows.
func openHistory(a *fleet.Agent) tea.Cmd {
	key, id := a.Key, a.ID
	h := &history{kind: agent.Kind(a.Kind), s: agent.Session{ID: a.SessionID, Name: a.DisplayName, Transcript: a.History,
		State: a.State, Remote: a.Remote, Profile: agent.Profile{Kind: agent.Kind(a.Kind), Dir: a.Acct.Dir}}}
	return func() tea.Msg {
		h.stat()
		sess := agentHistory(h.kind, h.s, time.Time{})
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, kind: h.kind, sess: sess, hist: h, open: map[string]bool{}, ready: true}}
	}
}

// history is where a pane read another agent's session from, and how the
// file stood then.
type history struct {
	kind agent.Kind
	s    agent.Session
	mod  time.Time
	size int64
	at   time.Time // when it was last read
}

// historyEvery is how often a growing history is read again: it's read
// whole each time.
const historyEvery = 2 * time.Second

// remoteEvery is how often a remote session still working is read again:
// there's no file to watch, only its log to fetch.
const remoteEvery = 10 * time.Second

// stat notes how the file stands, and reports whether it changed. A
// remote session's log is taken to change while it works.
func (h *history) stat() bool {
	if h.s.Remote {
		working := h.s.State == "working" || h.s.State == "blocked"
		return working && time.Since(h.at) >= remoteEvery
	}
	fi, err := os.Stat(h.s.Transcript)
	if err != nil {
		return false
	}
	changed := !fi.ModTime().Equal(h.mod) || fi.Size() != h.size
	h.mod, h.size = fi.ModTime(), fi.Size()
	return changed
}

// followHistory reads the session again when its history has grown, no
// more than every historyEvery, keeping how the pane is looked at.
func (c *hostConn) followHistory() {
	h := c.hist
	if time.Since(h.at) < historyEvery || !h.stat() {
		return
	}
	h.at = time.Now()
	c.sess = agentHistory(h.kind, h.s, time.Time{})
}
