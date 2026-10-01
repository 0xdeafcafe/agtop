package acp

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

func TestKimiConfiguredModelsAndPast(t *testing.T) {
	dir := t.TempDir()
	cfg := `[models."custom.model"]
model = "kimi-k2"
max_context_size = 262144
display_name = "My Kimi"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	a := Kimi{Known[0]}
	models := a.ListModels(agent.Profile{Dir: dir})
	if len(models) != 1 || models[0].ID != "custom.model" || models[0].Context != 262144 {
		t.Fatalf("models: %+v", models)
	}
	cwd := "/work/project"
	root := filepath.Join(dir, "sessions", fmt.Sprintf("%x", md5.Sum([]byte(cwd))), "session-1")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kimi.json"), []byte(`{"work_dirs":[{"path":"/work/project","kaos":"local"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	transcript := `{"role":"_checkpoint","id":0}
{"role":"user","content":[{"type":"text","text":"full pasted text"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}]}
{"role":"assistant","content":"answer","tool_calls":[{"id":"c1","function":{"name":"ReadFile","arguments":"{\"path\":\"a.go\"}"}}]}
{"role":"tool","tool_call_id":"c1","content":"file contents"}
`
	if err := os.WriteFile(filepath.Join(root, "context.jsonl"), []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	past := a.Past(agent.Profile{Dir: dir})
	if len(past) != 1 || past[0].Cwd != cwd || past[0].ID != "session-1" {
		t.Fatalf("past: %+v", past)
	}
	evs, err := ReadMessages(past[0].Transcript)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 4 {
		t.Fatalf("events: %+v", evs)
	}
	user := evs[0].(event.Message)
	if len(user.Parts) != 2 || user.Parts[0].Text != "full pasted text" || string(user.Parts[1].Image.Data) != "hi" {
		t.Fatalf("user: %+v", user)
	}
	if evs[2].(event.Message).Parts[0].Output.CallID != "c1" {
		t.Fatal("lost tool result linkage")
	}
}
func TestVibeHistoryImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"question","images":[{"source":{"kind":"file","path":"attachments/a.png"},"mime_type":"image/png"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	evs, err := ReadMessages(path)
	if err != nil {
		t.Fatal(err)
	}
	m := evs[0].(event.Message)
	if len(m.Parts) != 2 || m.Parts[1].Image.Path != filepath.Join(filepath.Dir(path), "attachments/a.png") {
		t.Fatalf("image: %+v", m)
	}
}

func TestKimiCheckKeyRequiresCredentials(t *testing.T) {
	dir := t.TempDir()
	p := agent.Profile{Dir: dir}
	a := Kimi{Known[0]}
	config := `[models.test]
provider = "kimi"
model = "kimi"
[providers.kimi]
api_key = ""
[providers.kimi.oauth]
key = "oauth/kimi-code"
storage = "file"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKey(p); err == nil {
		t.Fatal("model configuration is not authentication")
	}
	if err := os.MkdirAll(filepath.Join(dir, "credentials"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials", "kimi-code.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKey(p); err == nil {
		t.Fatal("empty token file is not authentication")
	}
	if err := os.WriteFile(path, []byte(`{"refresh_token":"test-refresh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKey(p); err != nil {
		t.Fatal(err)
	}
}

func TestMessageHistoryTailAndCutoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.jsonl")
	text := "{\"timestamp\":\"2026-10-01T00:00:00Z\",\"role\":\"user\",\"content\":\"old\"}\n" + "{\"timestamp\":\"2026-10-01T00:01:00Z\",\"role\":\"assistant\",\"content\":\"recent\"}\n"
	os.WriteFile(path, []byte(text), 0600)
	evs, cut, err := ReadMessagesTail(path, 85)
	if err != nil || !cut {
		t.Fatalf("tail cut %v err %v", cut, err)
	}
	messages := 0
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok {
			messages++
			if m.Parts[0].Text != "recent" {
				t.Fatalf("unexpected tail %+v", m)
			}
		}
	}
	if messages != 1 {
		t.Fatalf("tail messages %d", messages)
	}
	before, _ := time.Parse(time.RFC3339, "2026-10-01T00:01:00Z")
	evs, err = ReadMessagesBefore(path, before)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok && m.Parts[0].Text != "old" {
			t.Fatal("included cutoff message")
		}
	}
}

func BenchmarkMessageHistoryLarge(b *testing.B) {
	path := filepath.Join(b.TempDir(), "messages.jsonl")
	f, _ := os.Create(path)
	for i := 0; i < 4096; i++ {
		fmt.Fprintf(f, "{\"role\":\"user\",\"content\":\"request\"}\n{\"role\":\"assistant\",\"content\":%q}\n", strings.Repeat("response ", 1024))
	}
	f.Close()
	b.Run("Whole", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := ReadMessages(path); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Tail2MiB", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := ReadMessagesTail(path, 2<<20); err != nil {
				b.Fatal(err)
			}
		}
	})
}
