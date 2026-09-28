package ollama

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
)

func envOf(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func TestTuneSendsEverythingToTheOneModel(t *testing.T) {
	m := Model{Name: "qwen3.6:35b-a3b-coding", Capabilities: []string{"tools", "thinking"}, Context: 65536}
	env, flags := tune(m, "http://127.0.0.1:11434", nil)
	e := envOf(env)
	for _, a := range aliases {
		if e[a] != m.Name {
			t.Errorf("%s = %q, want the model", a, e[a])
		}
	}
	if e["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:11434" || e["ANTHROPIC_API_KEY"] != "" {
		t.Errorf("not pointed at Ollama alone: %v", e)
	}
	if e["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "65536" || e["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "16384" {
		t.Errorf("window: compact %q, output %q", e["CLAUDE_CODE_AUTO_COMPACT_WINDOW"], e["CLAUDE_CODE_MAX_OUTPUT_TOKENS"])
	}
	if _, ok := e["CLAUDE_CODE_DISABLE_THINKING"]; ok {
		t.Error("thinking turned off for a model that thinks")
	}
	if !slices.Contains(flags, leanTools) || !slices.Contains(flags, "--exclude-dynamic-system-prompt-sections") {
		t.Errorf("flags %v aren't the lean ones", flags)
	}
}

func TestTuneFitsTheModel(t *testing.T) {
	m := Model{Name: "qwen3-coder:30b", Capabilities: []string{"tools"}, MaxContext: 262144}
	env, flags := tune(m, "http://h:1", []string{"--tools", "default"})
	e := envOf(env)
	if e["CLAUDE_CODE_DISABLE_THINKING"] != "1" {
		t.Error("thinking left on for a model that doesn't think")
	}
	if e["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "262144" || e["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "32000" {
		t.Errorf("unloaded model's window: %v", e)
	}
	if slices.Contains(flags, leanTools) {
		t.Errorf("tools asked for were replaced: %v", flags)
	}
}

func TestRankPrefersLoadedThenCoders(t *testing.T) {
	vl := Model{Name: "qwen3-vl:30b", Capabilities: []string{"tools", "thinking", "vision"}}
	coder := Model{Name: "qwen3.6:35b-a3b-coding", Capabilities: []string{"tools", "thinking"}}
	if rank(coder) <= rank(vl) {
		t.Error("a coder should come before a vision model")
	}
	vl.VRAM = 1
	if rank(vl) <= rank(coder) {
		t.Error("a model in memory should come first")
	}
}

func TestServer(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "http://127.0.0.1:11434",
		"0.0.0.0":             "http://127.0.0.1:11434",
		"0.0.0.0:9000":        "http://127.0.0.1:9000",
		"https://gpu.box:80/": "https://gpu.box:80",
		"box":                 "http://box:11434",
	} {
		t.Setenv("OLLAMA_HOST", in)
		if got := server(); got != want {
			t.Errorf("OLLAMA_HOST=%q: %q, want %q", in, got, want)
		}
	}
}

// TestLive runs a turn on the real Ollama: AGTOP_OLLAMA_LIVE=<model>.
func TestLive(t *testing.T) {
	model := os.Getenv("AGTOP_OLLAMA_LIVE")
	if model == "" {
		t.Skip("set AGTOP_OLLAMA_LIVE to a model to run against Ollama")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := Adapter{}.Start(ctx, agent.StartOptions{Dir: t.TempDir(), Model: model, Profile: agent.Profile{Dir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(agent.Input{Text: "Run `echo agtop-ollama` with the Bash tool and say what it printed."}); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	for ev := range conn.Events() {
		switch e := ev.(type) {
		case event.Approval:
			_ = conn.Answer(e.ID, "allow")
		case event.Message:
			for _, p := range e.Parts {
				if e.Role == "assistant" && p.Kind == event.Text {
					said.WriteString(p.Text)
				}
			}
		case event.TurnEnd:
			if !strings.Contains(said.String(), "agtop-ollama") {
				t.Fatalf("the model said %q", said.String())
			}
			return
		}
	}
	t.Fatal("the session ended before its turn did")
}
