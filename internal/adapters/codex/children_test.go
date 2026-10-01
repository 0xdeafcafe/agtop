package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// A spawn_agent call names the task its thread was given, and that thread
// is found by it among the rollouts: its own, apart from its parent's.
func TestSpawnAgentChild(t *testing.T) {
	r := (&rollout{t0: t0}).
		add("session_meta", meta("parent", "cli")).
		response("function_call", map[string]any{"id": "f", "call_id": "c1", "name": "spawn_agent", "namespace": "collaboration",
			"arguments": `{"task_name":"trace_bug","model":"gpt-5","message":"gAAAAencrypted"}`})
	evs, err := readRollout(strings.NewReader(r.text()), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var call *tool.Call
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok {
			for _, p := range m.Parts {
				if p.Kind == event.ToolCall {
					call = p.Call
				}
			}
		}
	}
	if call == nil || call.Kind != tool.Subagent || call.Input.Child != "trace_bug" || call.Input.Description != "trace_bug" {
		t.Fatalf("spawn_agent read as %+v", call)
	}

	dir := t.TempDir()
	day := filepath.Join(dir, "sessions", t0.Format("2006"), t0.Format("01"), t0.Format("02"))
	kid := func(id, parent, path string) map[string]any {
		m := meta(id, map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
			"parent_thread_id": parent, "agent_path": path, "agent_nickname": "Arendt"}}})
		m["parent_thread_id"] = parent
		return m
	}
	(&rollout{t0: t0}).add("session_meta", kid("other", "someone", "/root/trace_bug")).write(t, filepath.Join(day, "rollout-a-other.jsonl"))
	(&rollout{t0: t0}).add("session_meta", kid("mine", "parent", "/root/trace_bug")).write(t, filepath.Join(day, "rollout-b-mine.jsonl"))
	for _, f := range []string{"rollout-a-other.jsonl", "rollout-b-mine.jsonl"} {
		_ = os.Chtimes(filepath.Join(day, f), t0.Add(5e9), t0.Add(5e9))
	}
	s, ok := Adapter{}.FindChild(agent.Profile{Dir: dir}, "parent", call.Input.Child, t0)
	if !ok || s.ID != "mine" || s.Name != "Arendt" || s.Kind != Kind {
		t.Fatalf("child %+v %v", s, ok)
	}
}

// Codex 0.155 tells of a spawn as a subAgentActivity item, not a
// spawnAgent call: its start is the subagent, naming the child's thread.
func TestSubAgentActivitySpawn(t *testing.T) {
	c, ok := callOf(threadItem{Type: "subAgentActivity", ID: "call_1", Kind: "started", AgentThreadID: "kid", AgentPath: "/root/pong"}, nil)
	if !ok || c.Kind != tool.Subagent || c.Input.Child != "kid" || c.Input.Description != "pong" {
		t.Fatalf("started read as %+v %v", c, ok)
	}
	if _, ok := callOf(threadItem{Type: "subAgentActivity", Kind: "completed", AgentThreadID: "kid"}, nil); ok {
		t.Fatal("completed read as a spawn")
	}
}
