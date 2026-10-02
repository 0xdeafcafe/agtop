package host

import (
	"strconv"
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
	if got := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit - 1, ToolUses: 900}); got != "" {
		t.Fatalf("stopped early: %q", got)
	}
	reason := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit, ToolUses: 12})
	if !strings.Contains(reason, "long search") || !strings.Contains(reason, "narrower next task") {
		t.Fatalf("reason = %q", reason)
	}
	if got := w.taskReason(event.TaskProgress{ID: "child", Tokens: subagentInputLimit + 1, ToolUses: 13}); got != "" {
		t.Fatalf("stopped twice: %q", got)
	}
}

func TestHostedSubagentWatchdogResetsForResume(t *testing.T) {
	w := subagentWatchdog{}
	w.resetTurn()
	for i := 0; i < 500; i++ { // many tool calls alone aren't a runaway
		if reason := w.observeMessage(event.Message{Role: "assistant", ID: "m", Parts: []event.Part{{Kind: event.ToolCall,
			Call: &tool.Call{ID: strconv.Itoa(i), Name: "exec"}}}}); reason != "" {
			t.Fatalf("stopped at tool %d", i+1)
		}
	}
	if w.observeMessage(event.Message{Role: "assistant", ID: "big", Tokens: &usage.TokenUsage{CacheRead: subagentInputLimit}}) == "" {
		t.Fatal("did not stop at the token ceiling")
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
	s.watchTaskProgress(c, event.TaskProgress{ID: "child", Tokens: subagentInputLimit})
	select {
	case id := <-c.stopped:
		if id != "child" {
			t.Fatalf("stopped %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop the child")
	}
}
