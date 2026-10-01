package acp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// A Kimi subagent's own wire is found from main's: by task.started, by the
// call's result, or, still running in the foreground, by its prompt. It
// reads as a conversation of its own, and the Agent call names it.
func TestKimiFindChild(t *testing.T) {
	home := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	child := func(id, prompt, answer string) string {
		return `{"type":"metadata","protocol_version":"1.5","created_at":1000}
{"type":"context.append_message","agentId":"` + id + `","message":{"role":"user","origin":{"kind":"system_trigger"},"content":[{"type":"text","text":"<git-context>x</git-context>\n` + prompt + `"}]},"time":1100}
{"type":"context.append_loop_event","agentId":"` + id + `","event":{"type":"content.part","uuid":"a","part":{"type":"text","text":"` + answer + `"}},"time":1200}
{"type":"turn.ended","agentId":"` + id + `","reason":"completed","time":1300}
`
	}
	s := "sessions/wd_a/session_P/agents/"
	write(s+"main/wire.jsonl", `{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.call","toolCallId":"bg","name":"Agent","args":{"prompt":"find bugs","description":"bugs","run_in_background":true}},"time":1000}
{"type":"task.started","agentId":"main","info":{"taskId":"agent-x","kind":"agent","agentId":"agent-1","parentToolCallId":"bg"},"time":1001}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.call","toolCallId":"done","name":"Agent","args":{"prompt":"say ok"}},"time":1002}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.result","toolCallId":"done","result":{"output":"agent_id: agent-0\nstatus: completed"}},"time":1003}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.call","toolCallId":"fg","name":"Agent","args":{"prompt":"count lines"}},"time":1004}
`)
	write(s+"agent-0/wire.jsonl", child("agent-0", "say ok", "ok"))
	write(s+"agent-1/wire.jsonl", child("agent-1", "find bugs", "none found"))
	write(s+"agent-2/wire.jsonl", child("agent-2", "count lines", "42"))
	write("sessions/wd_b/session_Q/agents/agent-1/wire.jsonl", child("agent-1", "count lines", "decoy"))

	k := Kimi{Agent{ID: "kimi"}}
	p := agent.Profile{Kind: "kimi", Dir: home}
	for call, want := range map[string]string{"bg": "agent-1", "0:bg": "agent-1", "done": "agent-0", "fg": "agent-2"} {
		got, ok := k.FindChild(p, "session_P", call, time.Now().Add(-time.Minute))
		if !ok || got.ID != want || got.Transcript != filepath.Join(home, s+want, "wire.jsonl") || got.Kind != "kimi" {
			t.Fatalf("%s: %+v %v", call, got, ok)
		}
	}
	if _, ok := k.FindChild(p, "session_P", "nope", time.Time{}); ok {
		t.Fatal("found a child no call started")
	}

	got, _ := k.FindChild(p, "session_P", "bg", time.Time{})
	evs, err := k.History(got, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	answered := false
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok && m.Role == "assistant" && m.Parts[0].Text == "none found" {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("child conversation: %+v", evs)
	}

	main, _, err := readKimiWireFile(filepath.Join(home, s+"main/wire.jsonl"), 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := main[0].(event.Message); !ok || m.Parts[0].Call.Kind != tool.Subagent || m.Parts[0].Call.Input.Child != "bg" {
		t.Fatalf("call: %+v", main[0])
	}
}

// Live, Kimi's Agent call names its run by the call, as its wire does.
func TestKimiLiveSubagentChild(t *testing.T) {
	s, f := start(t)
	nextOf[event.Init](t, s)
	s.mu.Lock()
	s.o.Adapter = "kimi"
	s.mu.Unlock()
	f.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "Agent", "kind": "other", "status": "pending",
		"rawInput": map[string]any{"prompt": "say ok", "description": "ok", "subagent_type": "explore"}})
	if c := nextOf[event.Message](t, s).Parts[0].Call; c.Kind != tool.Subagent || c.Input.Child != "c1" {
		t.Fatalf("call: %+v", c)
	}
}
