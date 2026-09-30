package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	_ "github.com/0xdeafcafe/rush/internal/adapters/cross"  // providers in Pi, by key
	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama" // Ollama in several harnesses
	"github.com/0xdeafcafe/rush/internal/agent"
)

// What the sheet picks is what the next session starts as, and the box
// says so; without it, the folder's agent as Settings has it.
func TestStartOver(t *testing.T) {
	m, _ := benchModel(120, 40)
	dir := m.startDir()
	if got := m.nextStart(dir); got.kind != m.startKindIn(dir) {
		t.Fatalf("with nothing picked, the folder's agent: %+v", got)
	}
	m.startOver = &startOver{kind: "codex", model: "gpt-5.5", effort: "high"}
	if got := m.nextStart(dir); got.kind != "codex" || got.model != "gpt-5.5" || got.effort != "high" {
		t.Fatalf("picked: %+v", got)
	}
	if w := ansi.Strip(m.startWith(dir, true)); !strings.HasPrefix(w, "codex") || !strings.Contains(w, "high effort") {
		t.Errorf("the box should say what it starts as: %q", w)
	}
}

// Only valid combinations: a subscription in its own harness alone, an
// API key in any harness that speaks its API once the key is kept, and a
// local provider in all of its harnesses.
func TestRoutes(t *testing.T) {
	kinds := []string{"claude", "codex", "pi", "anthropic-pi", "openai-pi", "ollama", "ollama-pi"}
	say := func(rs []route) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.id+"="+strings.Join(r.kinds, "+"))
		}
		return strings.Join(out, " ")
	}
	none := func(string) bool { return false }
	if got, want := say(routes(kinds, none)), "claude=claude codex=codex pi=pi ollama=ollama+ollama-pi"; got != want {
		t.Errorf("no keys:\n got %s\nwant %s", got, want)
	}
	claudeKey := func(p string) bool { return p == "claude" }
	if got, want := say(routes(kinds, claudeKey)), "claude=claude claude-key=claude+anthropic-pi codex=codex pi=pi ollama=ollama+ollama-pi"; got != want {
		t.Errorf("Anthropic's key:\n got %s\nwant %s", got, want)
	}
}

// Provider and harness are picked apart: Ollama moves from Claude Code to
// Pi on the Harness row and stays Ollama; another provider starts in its
// own harness.
func TestStartHarness(t *testing.T) {
	m, _ := benchModel(120, 40)
	s := &startSheet{routes: routes([]string{"claude", "ollama", "codex", "ollama-pi"}, func(string) bool { return false }), o: m.startDefaults("ollama"), row: 2}
	if got := s.choices(m, 1); strings.Join(got, ",") != "claude,ollama,codex" {
		t.Errorf("providers: %v", got)
	}
	s.key(m, tea.KeyPressMsg{}, "right")
	if s.o.kind != "ollama-pi" {
		t.Errorf("Ollama in the next harness: %+v", s.o)
	}
	s.row = 1
	s.key(m, tea.KeyPressMsg{}, "right")
	if s.o.kind != "codex" {
		t.Errorf("the next provider, in its own harness: %+v", s.o)
	}
}

// One name for a setup everywhere: <harness>:<account>, the provider
// standing in for the account in another's harness.
func TestSetupName(t *testing.T) {
	for _, c := range []struct {
		k                agent.Kind
		billing, account string
		want             string
	}{
		{"claude", "", "Alex Work", "claudecode:alex-work"},
		{"claude", "key", "", "claudecode:key"},
		{"codex", "", "alex", "codex:alex"},
		{"codex", "", "", "codex"},
		{"ollama", "", "", "claudecode:ollama"},
		{"ollama-pi", "", "", "pi:ollama"},
		{"anthropic-pi", "key", "", "pi:claude-key"},
	} {
		if got := setupName(c.k, c.billing, c.account); got != c.want {
			t.Errorf("%s %q %q: got %s, want %s", c.k, c.billing, c.account, got, c.want)
		}
	}
}

// /agent takes a setup's name or another name for it, then an effort of
// its agent's if you like.
func TestPickSetup(t *testing.T) {
	ss := []setup{
		{"codex", []string{"codex"}, startOver{kind: "codex"}},
		{"codex:alex", []string{"codex:alex"}, startOver{kind: "codex", account: "alex"}},
		{"claudecode:alex", []string{"claude:alex"}, startOver{kind: "claude", account: "alex"}},
	}
	efforts := func(string) []agent.Choice { return []agent.Choice{{ID: "low"}, {ID: "high"}} }
	for arg, want := range map[string]startOver{
		"codex:alex":      {kind: "codex", account: "alex"},
		"Claude:Alex":     {kind: "claude", account: "alex"},
		"codex:alex:high": {kind: "codex", account: "alex", effort: "high"},
		"codex:low":       {kind: "codex", effort: "low"},
	} {
		if got, err := pickSetup(ss, arg, efforts); err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", arg, got, err, want)
		}
	}
	for _, arg := range []string{"nope", "codex:alex:ultra", "pi", ":high"} {
		if _, err := pickSetup(ss, arg, efforts); err == nil {
			t.Errorf("%s should say there's no such setup or effort", arg)
		}
	}
}
