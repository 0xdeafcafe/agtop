package ollama

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	vibead "github.com/0xdeafcafe/rush/internal/adapters/vibe"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

func TestVibeConfigPointsAtOllama(t *testing.T) {
	got := vibeConfig([]Model{
		{Name: "qwen3.8:27b-mlx", Capabilities: []string{"tools", "vision"}, Context: 65536},
		{Name: "qwen2.5:1.5b", Capabilities: []string{"tools"}},
	}, "http://127.0.0.1:11434")
	for _, want := range []string{
		`active_model = "qwen3.8:27b-mlx"` + "\n" + `allowed_models = ["qwen3.8:27b-mlx", "qwen2.5:1.5b"]`,
		"[[providers]]\nname = \"ollama\"\napi_base = \"http://127.0.0.1:11434/v1\"\napi_style = \"openai\"",
		"name = \"qwen3.8:27b-mlx\"\nprovider = \"ollama\"\nalias = \"qwen3.8:27b-mlx\"",
		"supports_images = true\nauto_compact_threshold = 49152\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if strings.Count(got, "supports_images") != 1 || strings.Count(got, "auto_compact_threshold") != 1 {
		t.Errorf("the small model takes images or has a window:\n%s", got)
	}
	if agent.HarnessOf(VibeKind) != vibead.Kind || agent.ProviderOf(VibeKind) != string(Kind) {
		t.Errorf("harness %s, provider %s", agent.HarnessOf(VibeKind), agent.ProviderOf(VibeKind))
	}
}

// TestVibeLive runs a turn of Vibe on the real Ollama:
// RUSH_OLLAMA_VIBE_LIVE=<model>.
func TestVibeLive(t *testing.T) {
	model := os.Getenv("RUSH_OLLAMA_VIBE_LIVE")
	if model == "" {
		t.Skip("set RUSH_OLLAMA_VIBE_LIVE to a model to run Vibe against Ollama")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, err := VibeAdapter{}.Start(ctx, agent.StartOptions{Model: model, Dir: t.TempDir(), Profile: agent.Profile{Dir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send(agent.Input{Text: "Reply with the one word: pong"}); err != nil {
		t.Fatal(err)
	}
	var said string
	for {
		select {
		case e := <-c.Events():
			switch e := e.(type) {
			case event.Message:
				for _, p := range e.Parts {
					if e.Role == "assistant" && p.Kind == event.Text {
						said += p.Text
					}
				}
			case event.TurnEnd:
				if e.Reason == "error" || said == "" {
					t.Fatalf("turn %+v, said %q", e, said)
				}
				t.Logf("%s said %q", model, said)
				return
			}
		case <-ctx.Done():
			t.Fatal("no answer")
		}
	}
}
