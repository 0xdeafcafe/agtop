package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
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

func busy(a *fleet.Agent) bool { return a.State == "working" || a.State == "blocked" }

// queueLocal adds a message to a Claude Code session's queue.
func (m *Model) queueLocal(key, text string) {
	if m.localQ == nil {
		m.localQ = map[string]*localQueue{}
	}
	q := m.localQ[key]
	if q == nil {
		q = &localQueue{}
		m.localQ[key] = q
	}
	if len(q.items) == 0 {
		q.since = time.Now()
	}
	q.items = append(q.items, text)
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
		if len(q.items) == 0 || q.held || time.Since(q.sentAt) < 10*time.Second {
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
		text := strings.Join(q.items, "\n\n")
		items := q.items
		q.items, q.sentAt = nil, time.Now()
		acct, id, name := a.Acct, a.ID, a.DisplayName
		cmds = append(cmds, func() tea.Msg {
			if err := actions.Reply(acct, id, text); err != nil {
				return localQueueFailed{key: key, items: items, err: err}
			}
			return doneMsg{text: "sent the queue to " + name}
		})
	}
	return tea.Batch(cmds...)
}

type localQueueFailed struct {
	key   string
	items []string
	err   error
}

// localQueueKey edits a Claude Code session's queue from the queue view.
func (m *Model) localQueueKey(c *hostConn, s string) (tea.Cmd, bool) {
	q := m.localQ[c.key]
	if q == nil {
		return nil, false
	}
	if s == "alt+h" {
		q.held = !q.held
		return nil, true
	}
	var i int
	if _, err := fmt.Sscanf(c.sel, "q:%d", &i); err != nil || i >= len(q.items) {
		return nil, false
	}
	switch s {
	case "enter":
		c.input, c.back, c.editQ, c.editWas = []rune(q.items[i]), 0, i+1, q.items[i]
		c.sel = ""
	case "shift+up", "shift+down":
		to := i - 1
		if s == "shift+down" {
			to = i + 1
		}
		if to >= 0 && to < len(q.items) {
			q.items[i], q.items[to] = q.items[to], q.items[i]
			c.sel = fmt.Sprintf("q:%d", to)
		}
	case "alt+m":
		if i+1 < len(q.items) {
			q.items[i] += "\n\n" + q.items[i+1]
			q.items = slices.Delete(q.items, i+1, i+2)
		}
	case "ctrl+s":
		text := q.items[i]
		q.items = slices.Delete(q.items, i, i+1)
		c.sel = ""
		a := m.agentByKey(c.key)
		if a == nil {
			return nil, true
		}
		return cmdErr("sent to "+a.DisplayName, func() error { return actions.Reply(a.Acct, a.ID, text) }), true
	case "ctrl+x", "delete":
		q.items = slices.Delete(q.items, i, i+1)
		c.sel = ""
	default:
		return nil, false
	}
	return nil, true
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
