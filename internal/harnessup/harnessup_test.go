package harnessup

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"2.1.284 (Claude Code)":       "2.1.284",
		"codex-cli 0.155.1\n":         "0.155.1",
		"GitHub Copilot CLI 1.0.3.":   "1.0.3",
		"v1.0.3-beta.2":               "1.0.3-beta.2",
		"ollama version is 0.12.3":    "0.12.3",
		"no version here":             "",
		"node v22 needed, have 0.3.4": "0.3.4",
	} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, installed string
		want              bool
	}{
		{"2.1.285", "2.1.284", true},
		{"0.10.0", "0.9.9", true}, // by value, not as text
		{"1.0", "1.0.0", false},
		{"1.0.0", "1.0.0-beta.1", true},
		{"1.0.0-beta.1", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"", "1.0.0", false},
		{"1.0.0", "", false},
	} {
		if got := Newer(c.latest, c.installed); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.installed, got, c.want)
		}
	}
}

func TestPlan(t *testing.T) {
	claude := agent.Published{NPM: "@anthropic-ai/claude-code", Update: []string{"update"}}
	for _, c := range []struct {
		pub              agent.Published
		path, real, want string
	}{
		{claude, "/opt/homebrew/bin/claude", "/opt/homebrew/Caskroom/claude-code@latest/2.1.284/claude", "brew upgrade --cask claude-code@latest"},
		{claude, "/Users/x/.local/bin/claude", "/Users/x/.local/share/claude/versions/2.1.284", "claude update"},
		{agent.Published{}, "/opt/homebrew/bin/gemini", "/opt/homebrew/lib/node_modules/@google/gemini-cli/bundle/gemini.js", "npm install -g @google/gemini-cli@latest"},
		{agent.Published{}, "/x/pnpm/pi", "/x/pnpm/global/5/node_modules/@earendil-works/pi-coding-agent/cli.js", "pnpm add -g @earendil-works/pi-coding-agent@latest"},
		{agent.Published{}, "/opt/homebrew/bin/vibe", "/opt/homebrew/Cellar/mistral-vibe/2.25.0/libexec/bin/vibe", "brew upgrade mistral-vibe"},
		{agent.Published{}, "/Users/x/.kimi-code/bin/kimi", "/Users/x/.kimi-code/bin/kimi", ""},
	} {
		if got := planFor(c.pub, c.path, sourceOf(c.real)).Label; got != c.want {
			t.Errorf("plan at %s = %q, want %q", c.real, got, c.want)
		}
	}
}
