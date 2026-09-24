package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

func TestAskCold(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}}
	c.sess.Context = 120_000
	sent := 0
	send := func() tea.Cmd { sent++; return nil }

	c.sess.Requests = []convo.Request{{At: time.Now().Add(-10 * time.Minute)}}
	if m.askCold(c, "hi", send) {
		t.Fatal("a warm cache shouldn't ask")
	}

	c.sess.Requests = []convo.Request{{At: time.Now().Add(-3 * time.Hour)}}
	c.sess.Info.State = "working"
	if m.askCold(c, "hi", send) {
		t.Fatal("a turn under way keeps the cache warm")
	}
	c.sess.Info.State = "idle"
	if m.askCold(c, "/clear", send) {
		t.Fatal("/clear re-reads nothing")
	}
	if !m.askCold(c, "hi", send) || m.confirm == nil || sent != 0 {
		t.Fatalf("a cold cache should ask first: confirm=%v sent=%d", m.confirm, sent)
	}
	m.confirmKey("y")
	if sent != 1 {
		t.Fatal("y should send")
	}
	if m.askCold(c, "again", send) {
		t.Fatal("once you've said send, the same cold cache doesn't ask again")
	}

	c2 := &hostConn{key: "k2", sess: convo.New()}
	c2.sess.Requests = []convo.Request{{At: time.Now().Add(-3 * time.Hour)}}
	c2.sess.Context = 20_000
	if m.askCold(c2, "hi", send) {
		t.Fatal("a small context isn't worth asking about")
	}
	c2.sess.Requests = nil
	c2.sess.Context = 0
	if m.askCold(c2, "hi", send) {
		t.Fatal("a session with no request yet has no cache to lose")
	}
}
