package ui

import (
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
	"testing"
)

func TestQueueExchangePreservesOriginAndDraft(t *testing.T) {
	m, c := draftModel()
	c.client = &host.Client{}
	e := &event.Exchange{ID: "from-agent", Sender: event.Peer{Name: "Reviewer", Kind: "codex"}, Text: "agent note"}
	c.sess.Info.Queue = []string{"user note", "agent note"}
	c.sess.Info.QueueExchanges = []*event.Exchange{nil, e}
	m.queueEdit(c, "move", 1, 0)
	if c.sess.Info.Queue[0] != "agent note" || c.sess.Info.QueueExchanges[0] != e {
		t.Fatal("moving loses origin")
	}
	if cmd := m.queueEdit(c, "merge", 0, 0); cmd != nil || len(c.sess.Info.Queue) != 2 {
		t.Fatal("merge should preserve separate origins")
	}
	if cmd := m.sendQueueNow(c, ""); cmd != nil || len(c.sess.Info.Queue) != 2 {
		t.Fatal("bulk send should not clear mixed-origin queue")
	}
	c.input = []rune("unfinished user draft")
	if cmd := m.sendPane(c, true); cmd != nil || string(c.input) != "unfinished user draft" || len(c.sess.Info.Queue) != 2 {
		t.Fatal("blocked bulk send lost draft or queue")
	}
	m.queueEdit(c, "drop", 1, 0)
	if len(c.sess.Info.QueueExchanges) != 1 || c.sess.Info.QueueExchanges[0] != e {
		t.Fatal("drop shifted origin")
	}
}
