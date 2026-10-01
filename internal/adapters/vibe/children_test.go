package vibe

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// A task's child session sits in its parent's folder, linked from the
// parent's meta.json by the call; the call names it, and it reads as a
// conversation of its own.
func TestFindChild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VIBE_CONFIG_PATH", "")
	write := func(rel, body string) {
		path := filepath.Join(dir, "logs", "session", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	parent := "session_1_aaaa/"
	write(parent+"meta.json", `{"session_id":"P","start_time":"2026-01-01T00:00:00Z","child_sessions":[
{"session_id":"C0","tool_call_id":"t0","agent":"explore","relative_path":"agents/explore_0_c0"},
{"session_id":"C1","tool_call_id":"t1","agent":"explore","relative_path":"agents/explore_1_c1"}]}`)
	write(parent+"messages.jsonl", `{"role":"user","content":"look"}
{"role":"assistant","content":"","tool_calls":[{"id":"t1","function":{"name":"task","arguments":"{\"task\":\"find it\",\"agent\":\"explore\"}"}}]}
`)
	write(parent+"agents/explore_0_c0/messages.jsonl", `{"role":"user","content":"decoy"}`+"\n")
	write(parent+"agents/explore_1_c1/meta.json", `{"session_id":"C1","parent_session_id":"P","start_time":"2026-01-01T00:00:05Z"}`)
	write(parent+"agents/explore_1_c1/messages.jsonl", `{"role":"user","content":"find it"}
{"role":"assistant","content":"found"}
`)
	write("session_2_bbbb/meta.json", `{"session_id":"Q","start_time":"2026-01-01T00:00:00Z","child_sessions":[{"session_id":"X","tool_call_id":"t1","agent":"explore","relative_path":"agents/x"}]}`)
	write("session_2_bbbb/messages.jsonl", `{"role":"user","content":"hi"}`+"\n")

	a := Adapter{}
	p := agent.Profile{Dir: dir}
	got, ok := a.FindChild(p, "P", "t1", time.Time{})
	if !ok || got.ID != "C1" || got.Transcript != filepath.Join(dir, "logs", "session", parent, "agents/explore_1_c1/messages.jsonl") || got.CreatedAt.Second() != 5 {
		t.Fatalf("child: %+v %v", got, ok)
	}
	if _, ok := a.FindChild(p, "P", "t9", time.Time{}); ok {
		t.Fatal("found a child no call started")
	}

	evs, err := a.History(got, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := evs[1].(event.Message); !ok || m.Role != "assistant" || m.Parts[0].Text != "found" {
		t.Fatalf("child conversation: %+v", evs)
	}

	evs, err = a.History(agent.Session{Transcript: filepath.Join(dir, "logs", "session", parent, "messages.jsonl")}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := evs[1].(event.Message); !ok || m.Parts[0].Call.Input.Child != "t1" {
		t.Fatalf("call: %+v", evs[1])
	}
}
