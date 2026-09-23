package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A line after a quiet spell is handed over at once; lines in a burst
// gather for at most a frame; the replay gathers whole.
func TestHostLinesLatency(t *testing.T) {
	lines := make(chan []byte, 64)
	c := &hostConn{key: "k", client: &host.Client{Lines: lines}}

	lines <- []byte("a")
	go func() { time.Sleep(5 * time.Millisecond); lines <- []byte("b") }()
	if msg := c.next()().(hostLinesMsg); len(msg.lines) != 2 {
		t.Fatalf("the replay should gather: %d lines", len(msg.lines))
	}

	c.flushed = time.Now().Add(-time.Second)
	cmd := c.next()
	lines <- []byte("c")
	go func() { time.Sleep(12 * time.Millisecond); lines <- []byte("x") }()
	if msg := cmd().(hostLinesMsg); len(msg.lines) != 1 {
		t.Fatalf("after a quiet spell a line shouldn't wait for more: %d lines", len(msg.lines))
	}
	<-time.After(15 * time.Millisecond)
	<-lines

	c.flushed = time.Now()
	cmd = c.next()
	lines <- []byte("d")
	go func() { time.Sleep(3 * time.Millisecond); lines <- []byte("e") }()
	start := time.Now()
	msg := cmd().(hostLinesMsg)
	if took := time.Since(start); len(msg.lines) != 2 || took > frame+100*time.Millisecond {
		t.Fatalf("in a burst: %d lines in %v", len(msg.lines), took)
	}
}

// A followed transcript that grows is noticed within a few polls, not on
// the next second's tick.
func TestTranscriptWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"user","timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644)
	tl := convo.NewTail(path)
	tl.Read()
	m := &Model{snap: &fleet.Snapshot{}, host: &hostConn{key: "k", tail: tl, sess: tl.Sess}}
	cmd := m.syncWatch()
	if cmd == nil || m.syncWatch() != nil {
		t.Fatal("one watch at a time")
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	start := time.Now()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","timestamp":"2026-09-23T20:00:01Z","message":{"id":"m","role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n")
	f.Close()
	select {
	case msg := <-got:
		if _, ok := msg.(growMsg); !ok || time.Since(start) > 200*time.Millisecond {
			t.Fatalf("got %T after %v", msg, time.Since(start))
		}
		m.onGrow(msg.(growMsg))
	case <-time.After(time.Second):
		t.Fatal("the watch never fired")
	}
	if len(tl.Sess.Turns) != 1 || len(tl.Sess.Turns[0].Items) != 1 {
		t.Fatalf("the new line wasn't taken in: %+v", tl.Sess.Turns)
	}
	next := m.syncWatch()
	if next == nil {
		t.Fatal("the watch should start again")
	}
	m.dropHost()
	if next() != nil {
		t.Fatal("a dropped session's watch should end quietly")
	}
}
