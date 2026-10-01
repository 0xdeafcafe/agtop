package gemini

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryReplacementsRewindImagesAndDiscovery(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tmp", "project", "chats")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), ".project_root"), []byte("/work/project"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "session-test.jsonl")
	lines := `{"sessionId":"s1","projectHash":"hash","startTime":"2026-01-01T00:00:00Z"}
{"id":"u1","type":"user","timestamp":"2026-01-01T00:00:01Z","content":[{"text":"whole prompt"},{"inlineData":{"mimeType":"image/png","data":"aGk="}}]}
{"id":"a1","type":"gemini","content":"partial"}
{"id":"a1","type":"gemini","content":"complete","tokens":{"input":50,"output":20},"model":"pro"}
{"id":"u2","type":"user","content":"discard me"}
{"$rewindTo":"u2"}
{"$set":{"summary":"My session","lastUpdated":"2026-01-01T00:00:10Z"}}
`
	if err := os.WriteFile(path, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	a := Adapter{}
	past := a.Past(agent.Profile{Dir: dir})
	if len(past) != 1 || past[0].Name != "My session" || past[0].Cwd != "/work/project" {
		t.Fatalf("past: %+v", past)
	}
	evs, err := a.History(past[0], time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("events: %+v", evs)
	}
	if user := evs[0].(event.Message); len(user.Parts) != 2 || string(user.Parts[1].Image.Data) != "hi" {
		t.Fatalf("user: %+v", user)
	}
	if msg := evs[1].(event.Message); msg.Parts[0].Text != "complete" || msg.Tokens.Input != 50 {
		t.Fatalf("assistant: %+v", msg)
	}
}
func TestLegacyHistoryAndCompaction(t *testing.T) {
	for _, ext := range []string{"json", "jsonl"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session."+ext)
			text := `{"sessionId":"s","messages":[{"id":"u","type":"user","content":"before"}]}`
			if ext == "jsonl" {
				text += "\n" + `{"$set":{"messages":[{"id":"u2","type":"user","content":"after"}]}}`
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := readConversation(path)
			if err != nil || len(c.Messages) != 1 {
				t.Fatalf("conversation: %+v %v", c, err)
			}
			want := "u"
			if ext == "jsonl" {
				want = "u2"
			}
			if c.Messages[0].ID != want {
				t.Fatalf("message: %+v", c.Messages)
			}
		})
	}
}

func TestCheckKeyNeedsUsableCredentialRecord(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	dir := t.TempDir()
	p := agent.Profile{Dir: dir}
	a := Adapter{}
	if err := os.WriteFile(filepath.Join(dir, credsFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKey(p); err == nil {
		t.Fatal("empty credentials marked signed in")
	}
	if err := os.WriteFile(filepath.Join(dir, credsFile), []byte(`{"refresh_token":"test-refresh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKey(p); err != nil {
		t.Fatal(err)
	}
}
