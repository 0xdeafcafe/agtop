package acp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Kimi's execute call, as it sends it: the command in rawInput.
func TestShellCall(t *testing.T) {
	s, f := start(t)
	nextOf[event.Init](t, s)
	f.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "Bash", "kind": "execute", "status": "in_progress",
		"rawInput": map[string]any{"command": "go test ./...", "description": "Run tests"}})
	c := nextOf[event.Message](t, s).Parts[0].Call
	if c.Kind != tool.Shell || c.Input.Command != "go test ./..." || c.Input.Background {
		t.Fatalf("call: %+v", c)
	}
}

// Copilot's subagent: kind other, a prompt and an agent type. It's a task
// until its call ends.
func TestSubagentCall(t *testing.T) {
	s, f := start(t)
	nextOf[event.Init](t, s)
	f.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "say ok", "kind": "other", "status": "pending",
		"rawInput": map[string]any{"name": "say-ok", "prompt": "Reply ok.", "agent_type": "general-purpose", "description": "say ok"}})
	c := nextOf[event.Message](t, s).Parts[0].Call
	if c.Kind != tool.Subagent || c.Input.Description != "say ok" || c.Input.Prompt != "Reply ok." || c.Input.Agent != "general-purpose" {
		t.Fatalf("call: %+v", c)
	}
	if got := nextOf[event.TaskStarted](t, s); got != (event.TaskStarted{ID: "c1", CallID: "c1", Kind: event.SubagentTask, Label: "say ok", Agent: "general-purpose"}) {
		t.Fatalf("started: %+v", got)
	}
	f.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed", "rawOutput": map[string]any{"content": "ok"}})
	if out := nextOf[event.Message](t, s).Parts[0].Output; out.Text != "ok" {
		t.Fatalf("result: %+v", out)
	}
	if got := nextOf[event.TaskDone](t, s); got != (event.TaskDone{ID: "c1", CallID: "c1", Status: "completed"}) {
		t.Fatalf("done: %+v", got)
	}
}

// Kimi's background shell: its result names the task, and a notification
// later says it ended.
func TestBackgroundShell(t *testing.T) {
	s, f := start(t)
	nextOf[event.Init](t, s)
	f.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "Bash", "kind": "execute", "status": "in_progress",
		"rawInput": map[string]any{"command": "sleep 2; echo hi", "run_in_background": true, "description": "sleep then hi"}})
	if c := nextOf[event.Message](t, s).Parts[0].Call; c.Kind != tool.Shell || !c.Input.Background {
		t.Fatalf("call: %+v", c)
	}
	f.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "c1", "status": "completed",
		"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "task_id: bash-x1\npid: 1\nstatus: running"}}}})
	nextOf[event.CallUpdated](t, s)
	nextOf[event.Message](t, s)
	if got := nextOf[event.TaskStarted](t, s); got != (event.TaskStarted{ID: "bash-x1", CallID: "c1", Kind: event.ShellTask, Label: "sleep then hi", Background: true}) {
		t.Fatalf("started: %+v", got)
	}
	if got := nextOf[event.Background](t, s); !reflect.DeepEqual(got.Tasks, []event.BackgroundTask{{ID: "bash-x1", Kind: event.ShellTask, Label: "sleep then hi"}}) {
		t.Fatalf("background: %+v", got)
	}
	f.update(text("user_message_chunk", `<notification id="task:bash-x1:completed" category="task" type="task.completed" source_kind="background_task" source_id="bash-x1">`))
	if got := nextOf[event.TaskDone](t, s); got != (event.TaskDone{ID: "bash-x1", CallID: "c1", Status: "completed"}) {
		t.Fatalf("done: %+v", got)
	}
	if got := nextOf[event.Background](t, s); len(got.Tasks) != 0 {
		t.Fatalf("background: %+v", got)
	}
}

// Kimi's wire keeps when each background task started and ended; Vibe's
// log keeps its task tool by name.
func TestHistoryTasks(t *testing.T) {
	dir := t.TempDir()
	wire := filepath.Join(dir, "wire.jsonl")
	if err := os.WriteFile(wire, []byte(`{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.call","toolCallId":"t1","name":"Agent","args":{"description":"dig","prompt":"Find it.","subagent_type":"coder","run_in_background":true}}}
{"type":"task.started","agentId":"main","info":{"taskId":"agent-1","description":"dig","kind":"agent","subagentType":"coder","parentToolCallId":"t1","detached":true}}
{"type":"task.terminated","agentId":"main","info":{"taskId":"agent-1","status":"completed","kind":"agent","parentToolCallId":"t1","detached":true}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	evs, _, err := readKimiWireFile(wire, 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	c := evs[0].(event.Message).Parts[0].Call
	if c.Kind != tool.Subagent || c.Input.Agent != "coder" || !c.Input.Background || c.Input.Prompt != "Find it." {
		t.Fatalf("call: %+v", c)
	}
	if evs[1] != (event.TaskStarted{ID: "agent-1", CallID: "t1", Kind: event.SubagentTask, Label: "dig", Agent: "coder", Background: true}) ||
		evs[2] != (event.TaskDone{ID: "agent-1", CallID: "t1", Status: "completed"}) {
		t.Fatalf("tasks: %+v", evs[1:])
	}

	log := filepath.Join(dir, "messages.jsonl")
	if err := os.WriteFile(log, []byte(`{"role":"assistant","content":"","tool_calls":[{"id":"v1","function":{"name":"task","arguments":"{\"task\":\"Look around.\",\"agent\":\"explore\"}"}},{"id":"v2","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	evs, err = ReadMessages(log)
	if err != nil {
		t.Fatal(err)
	}
	parts := evs[0].(event.Message).Parts
	if sub, sh := parts[0].Call, parts[1].Call; sub.Kind != tool.Subagent || sub.Input.Prompt != "Look around." || sub.Input.Agent != "explore" ||
		sh.Kind != tool.Shell || sh.Input.Command != "ls" {
		t.Fatalf("calls: %+v %+v", sub, sh)
	}
}
