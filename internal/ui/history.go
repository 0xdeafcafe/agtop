package ui

import (
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
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
	key, id, rush := a.Key, a.ID, a.Rush
	h := &history{kind: agent.Kind(a.Kind), s: agent.Session{ID: a.SessionID, Name: a.DisplayName, Transcript: a.History,
		State: a.State, Remote: a.Remote, Profile: agent.Profile{Kind: agent.Kind(a.Kind), Dir: a.Acct.Dir}}}
	if h.s.Transcript == "" {
		h.s.Transcript = agent.TranscriptPath(h.kind, h.s.Profile, a.Cwd, h.s.ID)
	}
	if rush {
		h.hostID = id
	}
	return func() tea.Msg {
		// A stopped rush session's folder is its host's to say.
		if rush && h.s.Profile.Dir == "" {
			if cfg, err := host.ReadConfig(id); err == nil {
				h.s.Profile.Dir = cfg.Account.Dir
			}
		}
		// Its end first; the pane's next read takes in the whole of it.
		sess := agentHistoryTail(h.kind, h.s)
		if h.hostID != "" {
			sess.RestoreExchanges(host.ReadExchanges(h.hostID))
		}
		if !sess.Partial {
			h.stat()
		}
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, kind: h.kind, sess: sess, hist: h, open: map[string]bool{}, ready: true}}
	}
}

// history is where a pane read another agent's session from, and how the
// file stood then.
type history struct {
	hostID string // Rush journal identity, empty for native history
	kind   agent.Kind
	s      agent.Session
	mod    time.Time
	size   int64
	at     time.Time // when it was last read
	// follow reads it on from where it last got to, when its adapter can.
	follow func() ([]event.Event, error)
}

// read is the session as its history tells it now: through a follower,
// which reads only what's new, when the adapter has one; nil once stop is
// set. Only one read of h runs at a time.
func (h *history) read(stop *atomic.Bool) (result *convo.Session) {
	defer func() {
		if result != nil && h.hostID != "" {
			result.RestoreExchanges(host.ReadExchanges(h.hostID))
		}
	}()
	if h.follow == nil && !h.s.Remote {
		if f, ok := agent.As[agent.HistoryFollower](h.kind); ok {
			h.follow = f.FollowHistory(h.s, stop)
		}
	}
	if h.follow == nil {
		return agentHistory(h.kind, h.s, time.Time{})
	}
	evs, err := h.follow()
	if err != nil {
		return nil
	}
	sess := convo.New()
	for _, ev := range evs {
		sess.Apply(ev, time.Time{})
	}
	return sess
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
