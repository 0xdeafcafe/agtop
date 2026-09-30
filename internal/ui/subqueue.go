package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A subagent you're watching has a queue of its own: what you send it
// waits there, where you can edit, reorder or hold it, and one message
// goes in each time it finishes a step. ctrl+enter still goes at once.

func subQKey(key, id string) string { return key + "\x00" + id }

// subQueue is the watched subagent's queue, when what you type goes
// straight to it; nil otherwise.
func (m *Model) subQueue(c *hostConn) (*localQueue, convo.Subagent) {
	sa, ok := m.relaySub(c)
	if !ok || !c.sess.Info.Inbox {
		return nil, sa
	}
	return m.localQueueOf(subQKey(c.key, sa.ID)), sa
}

// queueSub adds text to the watched subagent's queue.
func (m *Model) queueSub(c *hostConn, q *localQueue, sa convo.Subagent, text string) {
	if len(q.items) == 0 {
		q.since, q.at = time.Now(), c.subSteps(sa.ID) // goes after the step it's on
	}
	q.items = append(q.items, text)
	m.flash(fmt.Sprintf("queued for %s · goes in after its next step", sa.Type), false)
}

func (c *hostConn) subSteps(id string) int {
	if t := c.subTails[id]; t != nil {
		return t.Sess.Totals(time.Now()).ToolCalls
	}
	return 0
}

// tellSub sends text into subagent id, at its next step.
func tellSub(c *hostConn, name, id, text string) tea.Cmd {
	cl := c.client
	return func() tea.Msg {
		if err := cl.Tell(id, text); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "sent to " + name}
	}
}

// flushSubQueues sends each of the open pane's subagents the next message
// it has waiting, once it has taken a step since the last; a run that has
// finished hands what it still had to the main session.
// ponytail: only the open pane's are sent; another pane's wait for it to open.
func (m *Model) flushSubQueues() tea.Cmd {
	c := m.host
	if c == nil || c.client == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, sa := range c.subs {
		key := subQKey(c.key, sa.ID)
		q := m.localQ[key]
		if q == nil || len(q.items) == 0 {
			continue
		}
		if _, live := c.subState(sa); !live {
			text := "For your subagent " + sa.Type + ", which finished before it got this:\n\n" + host.JoinQueue(q.items)
			delete(m.localQ, key)
			cl := c.client
			cmds = append(cmds, hostCmd(func() error { return cl.Send(text) }))
			m.flash(sa.Type+" finished · what it had queued went to the main session", false)
			continue
		}
		if steps := c.subSteps(sa.ID); !q.held && steps > q.at {
			text := q.items[0]
			q.items, q.at, q.sentAt = q.items[1:], steps, time.Now()
			cmds = append(cmds, tellSub(c, sa.Type, sa.ID, text))
		}
	}
	return tea.Batch(cmds...)
}

// sendSubQueueNow sends the watched subagent all it has queued, and extra
// after it, as one message now.
func (m *Model) sendSubQueueNow(c *hostConn, q *localQueue, sa convo.Subagent, extra string) tea.Cmd {
	items := q.items
	if extra != "" {
		items = append(items, extra)
	}
	if len(items) == 0 {
		m.flash("nothing queued", false)
		return nil
	}
	q.items = nil
	return tellSub(c, sa.Type, sa.ID, strings.TrimSpace(host.JoinQueue(items)))
}
