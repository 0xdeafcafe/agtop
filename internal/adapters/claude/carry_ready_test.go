package claude

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

func TestCarriedHistoryWaitsForSuccessfulInitialize(t *testing.T) {
	c := &conn{initID: "init", carried: "imported", events: make(chan event.Event, 8), waits: map[string]chan headless.ControlReply{}}
	c.own(headless.ControlReply{ID: "other"})
	c.own(headless.ControlReply{ID: "init", Error: "login required"})
	if len(c.events) != 0 {
		t.Fatal("failed or unrelated initialize declared the handoff ready")
	}
	if c.carried != "imported" {
		t.Fatal("failed initialize forgot pending import")
	}
	c.own(headless.ControlReply{ID: "init"})
	init, ok := (<-c.events).(event.Init)
	if !ok || init.SessionID != "imported" {
		t.Fatalf("wrong readiness: %+v", init)
	}
	<-c.events // commands
	c.own(headless.ControlReply{ID: "init"})
	if _, ok := (<-c.events).(event.Commands); !ok {
		t.Fatal("repeated initialize duplicated readiness")
	}
}
