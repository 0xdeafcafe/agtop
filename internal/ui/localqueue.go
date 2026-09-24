package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// localQueue holds messages for a Claude Code session agtop doesn't run
// itself: they wait here while it works and go as one message once it is
// idle, the way an agtop-mode host does it.
type localQueue struct {
	items  []string
	held   bool
	sentAt time.Time
	since  time.Time // when the oldest waiting message was queued
	retry  time.Time // after a failed send, when to try again
	fails  int
}

// queued is a session's queue as the queue view shows it, whichever side
// keeps it.
type queued struct {
	items          []string
	held, separate bool
	local          bool
}

func (m *Model) queueOf(c *hostConn) queued {
	if c.client != nil {
		i := c.sess.Info
		return queued{items: i.Queue, held: i.QueueHeld, separate: i.QueueSeparate}
	}
	q := m.localQ[c.key]
	if q == nil {
		return queued{local: true}
	}
	return queued{items: q.items, held: q.held, local: true}
}

// canQueue is whether agtop can hold messages for this agent.
func canQueue(a *fleet.Agent) bool { return a != nil && !a.Agtop && !a.Interactive }

// reply sends a message to a Claude Code background job. A job the daemon
// has let go of (ENOJOB) still has its conversation on disk: it carries on
// in agtop mode, with the message as its first turn.
func reply(a *fleet.Agent, text string) tea.Cmd {
	acct, id, key, name := a.Acct, a.ID, a.Key, a.DisplayName
	return func() tea.Msg {
		err := actions.Reply(acct, id, text)
		switch {
		case daemon.IsRefusal(err, "ENOJOB"):
			return jobGoneMsg{key: key, text: text}
		case err != nil:
			return doneMsg{err: err}
		}
		return doneMsg{text: "sent to " + name}
	}
}

// jobGoneMsg is a message for a job Claude Code no longer has.
type jobGoneMsg struct{ key, text string }

func busy(a *fleet.Agent) bool { return a.State == "working" || a.State == "blocked" }

// queueLocal adds a message to a Claude Code session's queue.
func (m *Model) queueLocal(key, text string) {
	q := m.localQueueOf(key)
	if len(q.items) == 0 {
		q.since = time.Now()
	}
	q.items = append(q.items, text)
}

// localQueueOf is a Claude Code session's queue, made if it has none.
func (m *Model) localQueueOf(key string) *localQueue {
	if m.localQ == nil {
		m.localQ = map[string]*localQueue{}
	}
	q := m.localQ[key]
	if q == nil {
		q = &localQueue{}
		m.localQ[key] = q
	}
	return q
}

// withImages adds image files to a message as paths; Claude Code opens
// them itself.
func withImages(text string, images []string) string {
	if len(images) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	for _, p := range images {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[image: %s]", p)
	}
	return b.String()
}

// flushLocalQueues sends each waiting queue whose agent has gone idle.
func (m *Model) flushLocalQueues() tea.Cmd {
	var cmds []tea.Cmd
	for key, q := range m.localQ {
		if len(q.items) == 0 || q.held || time.Since(q.sentAt) < 10*time.Second || time.Now().Before(q.retry) {
			continue
		}
		a := m.agentByKey(key)
		if !canQueue(a) {
			continue
		}
		// Idle (or stopped: the message resumes it), it goes now. Working,
		// it goes after a short wait, so a burst of messages goes as one:
		// Claude Code takes it at its next step, and an agent that keeps
		// picking up work on its own is never idle to wait for. Waiting on
		// you, it holds until you've answered.
		switch {
		case a.State == "blocked":
			continue
		case busy(a) && time.Since(q.since) < 15*time.Second:
			continue
		}
		cmds = append(cmds, m.sendLocal(key, a, q))
	}
	return tea.Batch(cmds...)
}

// sendLocal sends a Claude Code session's whole queue as one message.
func (m *Model) sendLocal(key string, a *fleet.Agent, q *localQueue) tea.Cmd {
	text := strings.Join(q.items, "\n\n")
	items := q.items
	q.items, q.sentAt = nil, time.Now()
	acct, id, name := a.Acct, a.ID, a.DisplayName
	return func() tea.Msg {
		err := actions.Reply(acct, id, text)
		if daemon.IsRefusal(err, "ENOJOB") {
			return jobGoneMsg{key: key, text: text}
		}
		if err != nil {
			return localQueueFailed{key: key, items: items, err: err}
		}
		return doneMsg{text: "sent the queue to " + name}
	}
}

type localQueueFailed struct {
	key   string
	items []string
	err   error
}

// editLocal saves an edited queued message back in place.
func (m *Model) editLocal(c *hostConn, i int, was, text string) {
	q := m.localQ[c.key]
	if q == nil {
		return
	}
	if i >= len(q.items) || q.items[i] != was {
		if i = slices.Index(q.items, was); i < 0 {
			m.flash("that message has already been sent", true)
			return
		}
	}
	q.items[i] = text
}
