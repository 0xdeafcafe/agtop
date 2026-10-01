package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

func TestIncompleteReplayNeverPublishesAnOlderPrefix(t *testing.T) {
	m, _ := benchModel(120, 40)
	lines := make(chan []byte, 4)
	initial := convo.New()
	c := &hostConn{key: m.host.key, kind: "claude", client: &host.Client{Lines: lines}, sess: initial, open: map[string]bool{}}
	m.host = c
	partial := convo.New()
	partial.Apply(host.Sent{Text: "older message"}, time.Now())
	partial.Apply(headless.Result{Subtype: "success"}, time.Now())
	cmd := m.onReplay(replayMsg{owner: c, key: c.key, sess: partial})
	if c.sess != initial || c.ready || cmd == nil {
		t.Fatal("partial replay became visible")
	}
	lines <- []byte(`{"agtop_sent":true,"message":{"content":"latest message","role":"user"},"type":"user"}`)
	lines <- []byte(`{"info":{"state":"idle"},"type":"agtop_info"}`)
	done := cmd().(replayMsg)
	m.onReplay(done)
	if !c.ready || c.sess.Turns[len(c.sess.Turns)-1].Prompt != "latest message" {
		t.Fatal("first published replay was not current")
	}
}

func TestReopenedSessionRejectsOldConnectionResults(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	old := &hostConn{key: c.key}
	original := c.sess
	if cmd := m.onReplay(replayMsg{owner: old, key: c.key, sess: convo.New(), whole: true}); cmd != nil {
		t.Fatal("stale replay accepted")
	}
	m.onHostLines(hostLinesMsg{owner: old, key: c.key, closed: true})
	m.onWhole(wholeMsg{owner: old, key: c.key, sess: convo.New()})
	if m.host != c || c.sess != original {
		t.Fatal("old connection changed reopened session")
	}
}

func TestScrollThroughLongPromptDoesNotSnapBackward(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
	c.sess.Apply(host.Sent{Text: strings.Repeat("a long prompt that wraps into many rows ", 100)}, time.Now())
	c.sess.Apply(headless.Message{Role: "assistant", ID: "scroll-end", Blocks: []headless.Block{{Type: "text", Text: "last answer"}}}, time.Now())
	c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
	m.View()
	checked := 0
	for scroll := 1; scroll < 45; scroll++ {
		c.scroll = scroll
		m.View()
		start := len(c.shown) - c.scroll - c.bodyRows
		if start < 0 || start >= len(c.shown) || !isTurnRef(c.shown[start].Ref) {
			continue
		}
		checked++
		if got := c.rowBody[c.bodyTop]; got != start {
			t.Fatalf("scroll %d snapped to row %d instead of %d", scroll, got, start)
		}
	}
	if checked < 3 {
		t.Fatal("fixture did not scroll inside prompt")
	}
}
