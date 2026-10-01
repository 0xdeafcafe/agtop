package acp

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

const kimiWireFixture = `{"type":"profile.bind","agentId":"main","modelAlias":"kimi-code/k3","time":1000}
{"type":"prompt.accepted","agentId":"main","content":[{"type":"text","text":"question"}],"time":2000}
{"type":"turn.prompt","agentId":"main","input":[{"type":"text","text":"question"}],"time":2000}
{"type":"context.append_message","agentId":"main","message":{"id":"u1","role":"user","origin":{"kind":"user"},"content":[{"type":"text","text":"question"}]},"time":2000}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"content.part","uuid":"thinking","part":{"type":"think","think":"checking"}},"time":2100}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.call","uuid":"call","toolCallId":"c1","name":"ReadFile","args":{"path":"file.go"}},"time":2200}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"tool.result","toolCallId":"c1","result":{"output":"file missing","isError":true}},"time":2300}
{"type":"context.append_message","agentId":"main","message":{"id":"injection","role":"user","origin":{"kind":"task"},"content":[{"type":"text","text":"background report"}]},"time":2400}
{"type":"context.append_loop_event","agentId":"agent-1","event":{"type":"content.part","uuid":"child","part":{"type":"text","text":"child output"}},"time":2450}
{"type":"context.append_loop_event","agentId":"main","event":{"type":"content.part","uuid":"answer","part":{"type":"text","text":"answer"}},"time":2500}
{"type":"usage.record","agentId":"main","usage":{"inputOther":10,"output":20,"inputCacheRead":30,"inputCacheCreation":40},"time":2600}
{"type":"turn.ended","agentId":"main","reason":"completed","durationMs":700,"time":2700}
{"type":"context.apply_compaction","agentId":"main","tokensBefore":100,"tokensAfter":20,"time":2800}
{"type":"context.append_message","agentId":"main","message":{"id":"u2","role":"user","origin":{"kind":"user"},"content":[{"type":"text","text":"later question"}]},"time":3000}
{"type":"turn.ended","agentId":"main","reason":"cancelled","time":4000}
`

func TestKimiWireHistory(t *testing.T) {
	evs, err := readKimiWire(strings.NewReader(kimiWireFixture), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var prompts, injected, answers, ended int
	for _, ev := range evs {
		switch m := ev.(type) {
		case event.Message:
			for _, p := range m.Parts {
				if p.Text == "child output" {
					t.Fatal("subagent leaked into main conversation")
				}
				if m.Role == "user" && p.Kind == event.Text {
					if m.Injected {
						injected++
					} else {
						prompts++
					}
				}
				if p.Text == "answer" {
					answers++
					if m.Model != "kimi-code/k3" {
						t.Fatal("lost model")
					}
				}
				if p.Kind == event.ToolCall && (p.Call.Kind != tool.Read || p.Call.Input.Path != "file.go") {
					t.Fatalf("tool call lost its meaning: %+v", p.Call)
				}
				if p.Kind == event.ToolResult && (p.Output.CallID != "c1" || !p.Output.IsError || p.Output.Text != "file missing") {
					t.Fatalf("tool result: %+v", p.Output)
				}
			}
		case event.TurnEnd:
			ended++
			if ended == 1 && (m.Tokens.Total() != 100 || m.Duration != 700*time.Millisecond || m.Reason != "done") {
				t.Fatalf("turn accounting: %+v", m)
			}
			if ended == 2 && m.Reason != "interrupted" {
				t.Fatalf("interruption: %+v", m)
			}
		}
	}
	if prompts != 2 || injected != 1 || answers != 1 || ended != 2 {
		t.Fatalf("prompts=%d injected=%d answers=%d ended=%d", prompts, injected, answers, ended)
	}
	evs, err = readKimiWire(strings.NewReader(kimiWireFixture), time.UnixMilli(3000))
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok && m.ID == "u2" {
			t.Fatal("cutoff included a newer prompt")
		}
	}
}

func TestKimiCodeDiscoveryAndTail(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "sessions", "wd_project_123", "session_one")
	wire := filepath.Join(root, "agents", "main", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(wire), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"id":"session_one","cwd":"/work/project","title":"Saved Kimi conversation","updatedAt":4000}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wire, []byte(kimiWireFixture), 0600); err != nil {
		t.Fatal(err)
	}
	a := Kimi{Known[0]}
	past := a.Past(agent.Profile{Kind: "kimi", Dir: dir})
	if len(past) != 1 || past[0].ID != "session_one" || past[0].Cwd != "/work/project" || past[0].Name != "Saved Kimi conversation" || !past[0].UpdatedAt.Equal(time.UnixMilli(4000)) {
		t.Fatalf("past: %+v", past)
	}
	evs, err := a.History(past[0], time.UnixMilli(3000))
	if err != nil || len(evs) == 0 {
		t.Fatalf("history: %d %v", len(evs), err)
	}
	evs, cut, err := a.HistoryTail(past[0], 300)
	if err != nil || !cut || len(evs) == 0 {
		t.Fatalf("tail: %d %v %v", len(evs), cut, err)
	}
	if err := os.Remove(wire); err != nil {
		t.Fatal(err)
	}
	if len(a.Past(agent.Profile{Dir: dir})) != 0 {
		t.Fatal("missing transcript must not appear as resumable history")
	}
}

func TestKimiWireMalformedRecord(t *testing.T) {
	if _, err := readKimiWire(strings.NewReader("not-json\n"), time.Time{}); err == nil {
		t.Fatal("malformed history was silently accepted")
	}
}
