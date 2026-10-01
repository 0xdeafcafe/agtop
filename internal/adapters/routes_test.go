package adapters_test

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// The matrix in docs/providers-harnesses.md: which provider runs in which
// harness, by what agent, and what's said when it can't or should warn.
func TestCompat(t *testing.T) {
	for _, c := range []struct {
		id      string
		harness agent.Kind
		kind    agent.Kind
		warn    bool
		why     string // a word the reason says, when it can't
	}{
		{id: "claude", harness: "claude", kind: "claude"},
		{id: "claude", harness: "pi", kind: "anthropic-plan-pi", warn: true},
		{id: "claude", harness: "codex", why: "plan runs in Claude Code or Pi"},
		{id: "claude-key", harness: "claude", kind: "claude"},
		{id: "claude-key", harness: "pi", kind: "anthropic-pi"},
		{id: "claude-key", harness: "codex", why: "Responses API"},
		{id: "codex", harness: "codex", kind: "codex"},
		{id: "codex", harness: "pi", kind: "openai-plan-pi", warn: true},
		{id: "codex-key", harness: "pi", kind: "openai-pi"},
		{id: "codex-key", harness: "claude", why: "Anthropic's API"},
		{id: "ollama", harness: "claude", kind: "ollama"},
		{id: "ollama", harness: "codex", kind: "ollama-codex"},
		{id: "ollama", harness: "pi", kind: "ollama-pi"},
		{id: "deepseek", harness: "claude", kind: "deepseek-claude"},
		{id: "glm", harness: "pi", kind: "glm-pi"},
	} {
		k, warn, why := agent.Compat(c.id, c.harness)
		if k != c.kind || (warn != "") != c.warn {
			t.Errorf("%s in %s: %q warn %q, want %q warn %v", c.id, c.harness, k, warn, c.kind, c.warn)
		}
		if c.why != "" && !strings.Contains(why, c.why) {
			t.Errorf("%s in %s: why %q, want it to say %q", c.id, c.harness, why, c.why)
		}
	}
}

// Words go both ways, and old and short ones are understood.
func TestRouteWords(t *testing.T) {
	for id, w := range map[string]string{"claude": "anthropic-sub", "claude-key": "anthropic-api", "codex": "openai-sub", "codex-key": "openai-api", "ollama": "ollama"} {
		if got := agent.ProviderWord(id); got != w {
			t.Errorf("ProviderWord(%s) = %s, want %s", id, got, w)
		}
		if got := agent.ProviderByWord(w); got != id {
			t.Errorf("ProviderByWord(%s) = %s, want %s", w, got, id)
		}
	}
	for w, h := range map[string]agent.Kind{"cc": "claude", "claude-code": "claude", "codex": "codex", "pi": "pi", "dsh": "deepseek"} {
		if got := agent.HarnessByWord(w); got != h {
			t.Errorf("HarnessByWord(%s) = %s, want %s", w, got, h)
		}
	}
	// A key pays: Pi on Anthropic's key is the key's, on its plan the plan's.
	if id, h := agent.RouteOf("anthropic-pi", false); id != "claude-key" || h != "pi" {
		t.Errorf("anthropic-pi is %s/%s", id, h)
	}
	if id, h := agent.RouteOf("anthropic-plan-pi", false); id != "claude" || h != "pi" {
		t.Errorf("anthropic-plan-pi is %s/%s", id, h)
	}
}
