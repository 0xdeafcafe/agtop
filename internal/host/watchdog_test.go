package host

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
)

type watchdogConn struct{ stopped chan string }

func (c *watchdogConn) Events() <-chan event.Event  { return make(chan event.Event) }
func (c *watchdogConn) Send(agent.Input) error      { return nil }
func (c *watchdogConn) Answer(string, string) error { return nil }
func (c *watchdogConn) Interrupt() error            { return nil }
func (c *watchdogConn) SetModel(string) error       { return nil }
func (c *watchdogConn) SetMode(string) error        { return nil }
func (c *watchdogConn) Close() error                { return nil }
func (c *watchdogConn) StopTask(id string) error    { c.stopped <- id; return nil }

func TestSubagentWatchdogStopsOnlyAfterLimit(t *testing.T) {
	w := subagentWatchdog{}
	w.startTask(event.TaskStarted{ID: "child", Kind: event.SubagentTask, Label: "long search"})
	if got := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit - 1, ToolUses: subagentToolLimit - 1}); got != "" {
		t.Fatalf("stopped early: %q", got)
	}
	reason := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit, ToolUses: 12})
	if !strings.Contains(reason, "long search") || !strings.Contains(reason, "resume the same subagent") {
		t.Fatalf("reason = %q", reason)
	}
	if got := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit + 1, ToolUses: 13}); got != "" {
		t.Fatalf("stopped twice: %q", got)
	}
}

func TestHostedSubagentWatchdogResetsForResume(t *testing.T) {
	w := subagentWatchdog{}
	w.resetTurn()
	for i := 0; i < subagentToolLimit; i++ {
		reason := w.observeMessage(event.Message{Role: "assistant", ID: "m", Parts: []event.Part{{Kind: event.ToolCall,
			Call: &tool.Call{ID: string(rune('a' + i)), Name: "exec"}}}})
		if i < subagentToolLimit-1 && reason != "" {
			t.Fatalf("stopped at tool %d", i+1)
		}
		if i == subagentToolLimit-1 && reason == "" {
			t.Fatal("did not stop at the tool limit")
		}
	}
	w.resetTurn()
	reason := w.observeMessage(event.Message{Role: "assistant", ID: "tokens", Tokens: &usage.TokenUsage{CacheRead: subagentInputLimit}})
	if reason == "" {
		t.Fatal("resumed turn did not get a fresh token ceiling")
	}
}

func TestTaskWatchdogCallsTargetedStop(t *testing.T) {
	c := &watchdogConn{stopped: make(chan string, 1)}
	s := &server{conn: c, clients: map[*conn]struct{}{}}
	s.watchdog.startTask(event.TaskStarted{ID: "child", Kind: event.SubagentTask})
	s.watchTaskProgress(c, event.TaskProgress{ID: "child", ToolUses: subagentToolLimit})
	select {
	case id := <-c.stopped:
		if id != "child" {
			t.Fatalf("stopped %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop the child")
	}
}
