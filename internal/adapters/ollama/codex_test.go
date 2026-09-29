package ollama

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	codexad "github.com/0xdeafcafe/agtop/internal/adapters/codex"
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
)

func TestCodexFlagsPointAtOllama(t *testing.T) {
	m := Model{Name: "qwen2.5:1.5b", Capabilities: []string{"tools"}, Context: 32768}
	flags := codexFlags(m, "http://127.0.0.1:11434", []string{"-c", "model=mine"})
	for _, want := range []string{
		`model_provider="agtop-ollama"`,
		`model_providers.agtop-ollama={name="Ollama",base_url="http://127.0.0.1:11434/v1",wire_api="responses"}`,
		`model="qwen2.5:1.5b"`,
		`model_context_window=32768`,
	} {
		if !slices.Contains(flags, want) {
			t.Errorf("no -c %s in %v", want, flags)
		}
	}
	// The caller's own come last, so theirs win.
	if flags[len(flags)-1] != "model=mine" {
		t.Errorf("caller's flags not last: %v", flags)
	}
	if f := codexFlags(Model{Name: "x"}, "http://h:1", nil); slices.ContainsFunc(f, func(s string) bool {
		return strings.HasPrefix(s, "model_context_window")
	}) {
		t.Errorf("a window given when none is known: %v", f)
	}
}

func TestCodexIsOllamaInCodex(t *testing.T) {
	if agent.HarnessOf(CodexKind) != codexad.Kind || agent.ProviderOf(CodexKind) != string(Kind) {
		t.Errorf("harness %s, provider %s", agent.HarnessOf(CodexKind), agent.ProviderOf(CodexKind))
	}
	if !slices.Contains(agent.Harnesses(string(Kind)), CodexKind) {
		t.Error("not among Ollama's harnesses")
	}
	ss := ours([]agent.Session{{ID: "t", Kind: codexad.Kind, Profile: agent.Profile{Kind: codexad.Kind}}})
	if ss[0].Kind != CodexKind || ss[0].Profile.Kind != CodexKind {
		t.Errorf("codex's session not ours: %+v", ss[0])
	}
}

// TestCodexLive runs a turn of Codex on the real Ollama:
// AGTOP_OLLAMA_CODEX_LIVE=<model>.
func TestCodexLive(t *testing.T) {
	model := os.Getenv("AGTOP_OLLAMA_CODEX_LIVE")
	if model == "" {
		t.Skip("set AGTOP_OLLAMA_CODEX_LIVE to a model to run Codex against Ollama")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	home := t.TempDir()
	conn, err := CodexAdapter{}.Start(ctx, agent.StartOptions{Dir: t.TempDir(), Model: model, Profile: agent.Profile{Dir: home}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(agent.Input{Text: "say hi"}); err != nil {
		t.Fatal(err)
	}
	var said strings.Builder
	for ev := range conn.Events() {
		t.Logf("%T %+v", ev, ev)
		switch e := ev.(type) {
		case event.Init:
			if e.Model != model {
				t.Errorf("thread runs %q, not %q", e.Model, model)
			}
		case event.Approval:
			_ = conn.Answer(e.ID, "allow")
		case event.Message:
			for _, p := range e.Parts {
				if e.Role == "assistant" && p.Kind == event.Text {
					said.WriteString(p.Text)
				}
			}
		case event.TurnEnd:
			if strings.TrimSpace(said.String()) == "" {
				t.Fatal("the model said nothing")
			}
			t.Logf("said %q", said.String())
			past := CodexAdapter{}.Past(agent.Profile{Kind: CodexKind, Dir: home})
			if len(past) != 1 || past[0].Kind != CodexKind {
				t.Fatalf("past: %+v", past)
			}
			if h, err := (CodexAdapter{}).History(past[0], time.Time{}); err != nil || len(h) == 0 {
				t.Fatalf("history: %d events, %v", len(h), err)
			}
			return
		}
	}
	t.Fatal("the session ended before its turn did")
}
