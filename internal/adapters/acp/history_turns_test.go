package acp_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/acp"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/convo"
)

func TestSavedMessageTurnsRenderSeparatelyAndFinish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.jsonl")
	text := `{"role":"user","content":"first request"}
{"role":"assistant","content":"working","tool_calls":[{"id":"call","function":{"name":"read","arguments":"{}"}}]}
{"role":"tool","tool_call_id":"call","content":"contents"}
{"role":"user","injected":true,"content":"injected context"}
{"role":"assistant","content":"first answer"}
{"role":"user","content":"second request"}
{"role":"assistant","content":"second answer"}
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	events, err := acp.ReadMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	session := convo.New()
	ends := 0
	injected := 0
	for _, ev := range events {
		if _, ok := ev.(event.TurnEnd); ok {
			ends++
		}
		if m, ok := ev.(event.Message); ok && m.Injected {
			injected++
		}
		session.Apply(ev, time.Now())
	}
	if ends != 2 || injected != 1 {
		t.Fatalf("boundaries=%d injected=%d", ends, injected)
	}
	if len(session.Turns) != 2 || session.Turns[0].Prompt != "first request" || session.Turns[1].Prompt != "second request" {
		t.Fatalf("turns: %+v", session.Turns)
	}
	if session.Live() != nil {
		t.Fatal("saved history is still working")
	}
	for _, turn := range session.Turns {
		for _, item := range turn.Items {
			if item.Kind == convo.KInterject {
				t.Fatal("ordinary history prompt rendered as mid-turn")
			}
		}
	}
}
