package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// TestLive runs a real pi on a model of the Ollama on this machine: a
// turn that reads a file, then the session resumed and read back.
// RUSH_PI_LIVE=1 runs it; RUSH_PI_MODEL is the model, qwen2.5:1.5b if
// unset.
func TestLive(t *testing.T) {
	if os.Getenv("RUSH_PI_LIVE") != "1" {
		t.Skip("RUSH_PI_LIVE=1 runs it")
	}
	model := os.Getenv("RUSH_PI_MODEL")
	if model == "" {
		model = "qwen2.5:1.5b"
	}
	home, work := t.TempDir(), t.TempDir()
	models := `{"providers":{"ollama":{"baseUrl":"http://127.0.0.1:11434/v1","api":"openai-completions","apiKey":"ollama",
		"compat":{"supportsDeveloperRole":false,"supportsReasoningEffort":false},"models":[{"id":"` + model + `"}]}}}`
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello from rush\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := agent.Profile{Kind: Kind, Name: "Pi", Dir: home}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	o := agent.StartOptions{Profile: p, Dir: work, Model: "ollama/" + model, Lean: true}
	c, err := Start(ctx, &o)
	if err != nil {
		t.Fatal(err)
	}
	init := liveNext[event.Init](t, c)
	t.Logf("init %+v", init)
	if init.SessionID == "" || init.Model != model {
		t.Fatalf("init: %+v", init)
	}
	if err := c.Send(agent.Input{Text: "Use the read tool to read a.txt, then tell me what it says."}); err != nil {
		t.Fatal(err)
	}
	var read bool
	var end event.TurnEnd
	for ev := range c.Events() {
		t.Logf("%T %+v", ev, ev)
		if m, ok := ev.(event.Message); ok {
			for _, part := range m.Parts {
				if part.Call != nil && part.Call.Kind == tool.Read {
					read = true
				}
			}
		}
		if e, ok := ev.(event.TurnEnd); ok {
			end = e
			break
		}
	}
	_ = c.Close()
	if end.Reason != "done" || !read {
		t.Fatalf("turn ended %+v, read a file: %v", end, read)
	}

	past := Adapter{}.Past(p)
	if len(past) != 1 || past[0].ID != init.SessionID || past[0].Model != model {
		t.Fatalf("past: %+v", past)
	}
	evs, err := Adapter{}.History(past[0], time.Time{})
	if err != nil || len(evs) < 4 {
		t.Fatalf("history: %v, %d events", err, len(evs))
	}

	o.Resume, o.SessionID = true, init.SessionID
	c, err = Start(ctx, &o)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if again := liveNext[event.Init](t, c); again.SessionID != init.SessionID {
		t.Fatalf("resumed %s, want %s", again.SessionID, init.SessionID)
	}
}

func liveNext[T event.Event](t *testing.T, c *Conn) T {
	t.Helper()
	for ev := range c.Events() {
		if e, ok := ev.(T); ok {
			return e
		}
	}
	t.Fatal("events ended")
	var zero T
	return zero
}
