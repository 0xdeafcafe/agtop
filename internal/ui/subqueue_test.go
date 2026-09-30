package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// stubRuns says whether every run is working, and nothing else.
type stubRuns struct {
	agent.SubagentRuns
	live bool
}

func (r *stubRuns) Gone() bool { return false }
func (r *stubRuns) State(string, string) (agent.RunState, string, time.Time) {
	return agent.RunRunning, "", time.Time{}
}
func (r *stubRuns) Going(string, string, time.Time, time.Time) (bool, string) { return r.live, "done" }

func TestSubQueue(t *testing.T) {
	runs := &stubRuns{live: true}
	sa := convo.Subagent{ID: "s1", Type: "lane"}
	c := &hostConn{key: "k", client: &host.Client{}, sess: convo.New(), subs: []convo.Subagent{sa}, subRuns: runs}
	m := &Model{host: c}
	q := m.localQueueOf(subQKey("k", "s1"))
	m.queueSub(c, q, sa, "one")
	m.queueSub(c, q, sa, "two")

	if m.flushSubQueues(); len(q.items) != 2 {
		t.Fatal("nothing goes before it takes a step")
	}
	q.at = -1 // it took one
	if m.flushSubQueues(); len(q.items) != 1 || q.items[0] != "two" {
		t.Fatalf("one message a step, the first first: %v", q.items)
	}
	if m.flushSubQueues(); len(q.items) != 1 {
		t.Fatal("the next waits for the next step")
	}
	runs.live = false
	if m.flushSubQueues(); m.localQ[subQKey("k", "s1")] != nil {
		t.Fatal("a finished run's queue goes to the main session")
	}
}
