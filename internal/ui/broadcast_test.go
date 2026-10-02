package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

func TestBroadcastPicks(t *testing.T) {
	fix := &fleet.Agent{Key: "a", DisplayName: "fix login bug"}
	docs := &fleet.Agent{Key: "b", DisplayName: "docs"}
	done := &fleet.Agent{Key: "c", DisplayName: "old work", Done: true}
	term := &fleet.Agent{Key: "d", DisplayName: "terminal one", Interactive: true}
	m := &Model{order: []*fleet.Agent{fix, docs, done, term}, groupOf: map[string]string{}}

	m.broadcastCommand("all")
	if len(m.broadcast) != 2 || !m.broadcast["a"] || !m.broadcast["b"] {
		t.Fatalf("all picks the agents at work a message can reach: %v", m.broadcast)
	}
	m.broadcastCommand("@docs")
	if len(m.broadcast) != 1 || !m.broadcast["b"] {
		t.Fatalf("a tag picks that agent: %v", m.broadcast)
	}
	m.broadcastPick("a")
	m.broadcastPick("d") // can't be messaged from here
	m.broadcastPick("b")
	if len(m.broadcast) != 1 || !m.broadcast["a"] {
		t.Fatalf("a click picks, a second unpicks: %v", m.broadcast)
	}
	if m.broadcastCommand("@nobody hi"); len(m.broadcast) != 1 {
		t.Fatal("an unknown tag changes nothing")
	}
	m.broadcastCommand("hello there")
	if len(m.broadcast) != 0 || string(m.input) != "hello there" {
		t.Fatalf("a message with nobody named waits for picks: %v %q", m.broadcast, string(m.input))
	}
}
