package codex

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"strings"
	"testing"
)

func TestNativeCompactionLifecycle(t *testing.T) {
	c, f, _ := start(t, agent.StartOptions{})
	next(t, c)
	sent := make(chan error, 1)
	go func() { sent <- c.Send(agent.Input{Text: " /compact "}) }()
	req := f.expect("thread/compact/start")
	if p := params(t, req); p["threadId"] != "th1" || len(p) != 1 {
		t.Fatalf("params: %v", p)
	}
	f.respond(req.ID, map[string]any{})
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	f.notify("turn/started", map[string]any{"threadId": "th1", "turn": map[string]any{"id": "compact-turn"}})
	if e, ok := next(t, c).(event.Status); !ok || !e.Busy {
		t.Fatalf("missing busy: %+v", e)
	}
	if err := c.Send(agent.Input{Text: "/compact"}); err == nil {
		t.Fatal("accepted concurrent compaction")
	}
	f.notify("item/started", map[string]any{"threadId": "th1", "item": map[string]any{"id": "compact-item", "type": "contextCompaction"}})
	if e, ok := next(t, c).(event.Status); !ok || e.Text != "compacting" {
		t.Fatalf("missing progress: %+v", e)
	}
	f.notify("item/completed", map[string]any{"threadId": "th1", "item": map[string]any{"id": "compact-item", "type": "contextCompaction"}})
	if _, ok := next(t, c).(event.Compacted); !ok {
		t.Fatal("missing compaction completion")
	}
	f.notify("turn/completed", map[string]any{"threadId": "th1", "turn": map[string]any{"id": "compact-turn", "status": "completed"}})
	if e, ok := next(t, c).(event.TurnEnd); !ok || e.Reason != "done" {
		t.Fatalf("missing turn end: %+v", e)
	}
	c.mu.Lock()
	turn := c.turn
	c.mu.Unlock()
	if turn != "" {
		t.Fatalf("still busy: %s", turn)
	}
}

func TestNativeCompactionRejectionLeavesThreadUsable(t *testing.T) {
	c, f, _ := start(t, agent.StartOptions{})
	next(t, c)
	if err := c.Send(agent.Input{Text: "/compact", Images: []string{"photo.png"}}); err == nil {
		t.Fatal("silently dropped attachment")
	}
	sent := make(chan error, 1)
	go func() { sent <- c.Send(agent.Input{Text: "/compact"}) }()
	req := f.expect("thread/compact/start")
	f.write(map[string]any{"id": req.ID, "error": map[string]any{"code": -32601, "message": "method unavailable"}})
	if err := <-sent; err == nil || !strings.Contains(err.Error(), "method unavailable") {
		t.Fatalf("lost rejection: %v", err)
	}
	go func() { sent <- c.Send(agent.Input{Text: "continue"}) }()
	req = f.expect("turn/start")
	f.respond(req.ID, map[string]any{"turn": map[string]any{"id": "next"}})
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
}
