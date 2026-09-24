package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
)

// A message you've sent shows as sending, under the conversation, until
// the agent has it: a turn or an interjection of yours in the conversation,
// or a message in its queue. Until then a slow host or a transcript not
// yet written would leave it looking lost.
type sending struct {
	text string
	base int // how many of yours the agent had, and those sending before it
	at   time.Time
}

// sendingFor is how long a message shows as sending before agtop stops
// waiting to see it arrive.
const sendingFor = 90 * time.Second

// yours counts what the agent has of yours: your turns and interjections,
// and what waits in its queue.
func (m *Model) yours(c *hostConn) int {
	n := len(m.queueOf(c).items)
	for _, t := range c.sess.Turns {
		if t.From == "" {
			n++
		}
		for _, it := range t.Items {
			if it.Kind == convo.KInterject {
				n++
			}
		}
	}
	return n
}

// markSending shows text as sending until the agent has it.
func (m *Model) markSending(c *hostConn, text string) {
	c.sending = append(c.sending, sending{text: text, base: m.yours(c) + len(c.sending), at: time.Now()})
}

// settleSending drops the messages the agent now has, oldest first, and
// those waited on too long.
func (m *Model) settleSending(c *hostConn) {
	if len(c.sending) == 0 {
		return
	}
	n := m.yours(c)
	for len(c.sending) > 0 && (n > c.sending[0].base || time.Since(c.sending[0].at) > sendingFor) {
		c.sending = c.sending[1:]
	}
}

// sendingLines are the rows for messages still on their way, w wide.
func (m *Model) sendingLines(c *hostConn, w int) []string {
	m.settleSending(c)
	var out []string
	for i, s := range c.sending {
		text := oneLine(s.text)
		if text == "" {
			text = "images"
		}
		mark := paint(cOrange, spinner[(m.tick+i)%len(spinner)]) + " " + paint(cSub, "sending")
		out = append(out, "  "+mark+"  "+paint(cText, ansi.Truncate(shortImages(text), w-16, "…")))
	}
	return out
}

// sendFailedMsg is a send that didn't go: it stops showing as sending.
type sendFailedMsg struct {
	key string
	err error
}

// sendingVia runs send, and on an error stops showing the message as
// sending.
func sendingVia(key string, send tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		msg := send()
		if d, ok := msg.(doneMsg); ok && d.err != nil {
			return sendFailedMsg{key: key, err: d.err}
		}
		return msg
	}
}

// sendFailed takes the last message sending off, and says why it failed.
func (m *Model) sendFailed(msg sendFailedMsg) {
	if c := m.host; c != nil && c.key == msg.key && len(c.sending) > 0 {
		c.sending = c.sending[:len(c.sending)-1]
	}
	m.flash(msg.err.Error(), true)
}
