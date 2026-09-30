package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// tailBytes is how much of a transcript's end a session opens on: a few
// turns, read in milliseconds. The rest is read after, in the background.
const tailBytes = 2 << 20

// wholeMsg brings a session's whole transcript, read after its end.
type wholeMsg struct {
	key  string
	tail *convo.Tail
}

// readWhole reads the whole transcript of a session opened on its end,
// and draws it once as warmed does. It gives up if the pane lets go first.
func (c *hostConn) readWhole(o convo.Options) tea.Cmd {
	key, path, closed := c.key, c.tail.Path, &c.closed
	return func() tea.Msg {
		t := convo.NewTail(path)
		t.Stop = closed
		if _, err := t.Read(); err != nil || closed.Load() {
			return nil
		}
		t.Sess.Spawns()
		if o.Width > 0 {
			o.Now = time.Now()
			t.Sess.Render(o)
		}
		return wholeMsg{key: key, tail: t}
	}
}

func (m *Model) onWhole(msg wholeMsg) {
	c := m.host
	if c == nil || c.key != msg.key || c.tail == nil || !c.sess.Partial {
		return
	}
	m.takeWhole(c, msg.tail.Sess)
	c.tail = msg.tail
}

// takeWhole puts the whole conversation where its end was. Refs name turns
// by number, and the end numbered its own from 1: what was opened, picked
// or scrolled to moves to the same turn, so nothing on screen jumps.
func (m *Model) takeWhole(c *hostConn, whole *convo.Session) {
	part := c.sess
	shift := len(whole.Turns) - len(part.Turns)
	if n := len(part.Turns); n > 0 {
		last := part.Turns[n-1]
		for i := len(whole.Turns) - 1; i >= 0; i-- {
			if t := whole.Turns[i]; t.Start.Equal(last.Start) && t.Prompt == last.Prompt {
				shift = i - (n - 1)
				break
			}
		}
	}
	c.sel, c.top.ref = renumber(c.sel, shift), renumber(c.top.ref, shift)
	c.open, c.looks = renumbered(c.open, shift), renumbered(c.looks, shift)
	c.sess = whole
	c.paneKick = true // what was written while it was read
	m.followTail()
}

// renumber moves a ref of turn N ("t12", "t12:s:…") to turn N+shift.
func renumber(ref string, shift int) string {
	if shift == 0 || !strings.HasPrefix(ref, "t") {
		return ref
	}
	turn, rest, more := strings.Cut(ref, ":")
	n, err := strconv.Atoi(turn[1:])
	if err != nil {
		return ref
	}
	if more {
		rest = ":" + rest
	}
	return "t" + strconv.Itoa(n+shift) + rest
}

func renumbered[V any](m map[string]V, shift int) map[string]V {
	if shift == 0 || m == nil {
		return m
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[renumber(k, shift)] = v
	}
	return out
}

// agentHistoryTail is agentHistory read from only the end of the history
// when the adapter can, its session Partial when that left some out.
func agentHistoryTail(kind agent.Kind, s agent.Session) *convo.Session {
	a, _ := agent.Get(kind)
	if tr, ok := a.(agent.TailReader); ok {
		if evs, cut, err := tr.HistoryTail(s, tailBytes); err == nil {
			sess := convo.New()
			for _, ev := range evs {
				sess.Apply(ev, time.Time{})
			}
			sess.Partial = cut
			return sess
		}
	}
	return agentHistory(kind, s, time.Time{})
}
