package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// Once the API can be reached, each session an API error stopped is told
// to continue once per halt, backing off, up to onlineMost times, and not
// while its own queue waits to go to it.
func TestNetworkBackContinuesWhatItStopped(t *testing.T) {
	now := time.Now()
	cut := func(key string) *fleet.Agent {
		a := &fleet.Agent{Key: key, DisplayName: key, PID: 7}
		a.State, a.UpdatedAt = "done", now.Add(-time.Minute)
		a.Spend.Halt = &claude.Halt{Kind: "server_error", Text: "API Error: Unable to connect to API (ENOTFOUND)", At: now.Add(-time.Minute)}
		return a
	}
	a, queued := cut("a"), cut("queued")
	m := &Model{localQ: map[string]*localQueue{"queued": {items: []string{"hi"}}}}
	m.snap = &fleet.Snapshot{At: now, Agents: []*fleet.Agent{a, queued}}
	keys := func() (out []string) {
		for _, a := range m.continueWaiting() {
			out = append(out, a.Key)
		}
		return out
	}
	if got := keys(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("waiting %v", got)
	}
	m.online.sent, m.online.tries = map[string]time.Time{"a": a.Spend.Halt.At}, map[string]int{"a": 1}
	if got := keys(); len(got) != 0 {
		t.Fatalf("told again for the same halt: %v", got)
	}
	a.Spend.Halt = &claude.Halt{Kind: "server_error", Text: "API Error: Connection dropped (ECONNRESET)", At: now.Add(-10 * time.Second)}
	if got := keys(); len(got) != 0 {
		t.Fatalf("told again before its backoff: %v", got)
	}
	a.Spend.Halt.At = now.Add(-20 * time.Second)
	if got := keys(); len(got) != 1 {
		t.Fatalf("a new halt past its backoff should be told again: %v", got)
	}
	m.online.tries["a"] = onlineMost
	if got := keys(); len(got) != 0 {
		t.Fatalf("told past onlineMost: %v", got)
	}
	a.Spend.Halt = nil
	m.continueWaiting()
	if m.online.tries["a"] != 0 {
		t.Fatal("answering should give its tries back")
	}
}

func TestContinueIsClaimedOnce(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	at := time.Now()
	if !claimContinue("k", at) || claimContinue("k", at) {
		t.Fatal("the second agtop should find it claimed")
	}
	if !claimContinue("k", at.Add(time.Second)) {
		t.Fatal("a new halt is a new claim")
	}
}
