package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// A subagent another agent says of only as a task is listed with the
// subagents, working until its task ends; once a row of its own is found
// (a transcript, a found thread) it gives way to that.
func TestSubagentFromItsTask(t *testing.T) {
	now := time.Now()
	s := convo.New()
	s.Info.Kind = "codex"
	call := tool.Call{ID: "c1", Name: "spawn_agent", Kind: tool.Subagent, Input: tool.Input{Description: "echoer"}}
	s.Apply(event.Message{Role: "assistant", ID: "c1", Parts: []event.Part{{Kind: event.ToolCall, Call: &call}}}, now)
	s.Apply(event.TaskStarted{ID: "kid", CallID: "c1", Kind: event.SubagentTask, Label: "echoer", Background: true}, now)
	c := &hostConn{kind: "codex", key: "k", sess: s, open: map[string]bool{}}

	subs := c.withJobSubs(nil)
	if len(subs) != 1 || subs[0].ID != jobSubPrefix+"kid" || subs[0].Description != "echoer" || subs[0].ToolUseID != "c1" {
		t.Fatalf("subs %+v", subs)
	}
	if _, live := c.subState(subs[0]); !live {
		t.Fatal("running task read as finished")
	}
	s.Apply(event.TaskDone{ID: "kid", CallID: "c1", Status: "completed"}, now.Add(time.Second))
	if st, live := c.subState(subs[0]); live || st != "completed" {
		t.Fatalf("ended task: %q %v", st, live)
	}
	found := []convo.Subagent{{ID: spawnPrefix + "c1", ToolUseID: "c1"}}
	if subs := c.withJobSubs(found); len(subs) != 1 || subs[0].ID != spawnPrefix+"c1" {
		t.Fatalf("listed twice: %+v", subs)
	}
}
