package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama" // Ollama in two harnesses
)

// What alt+m picks is what the next session starts as, and the box says
// so; without it, the folder's agent as Settings has it.
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
	if w := ansi.Strip(m.startWith(dir, true)); !strings.Contains(w, "Codex") || !strings.Contains(w, "high effort") {
		t.Errorf("the box should say what it starts as: %q", w)
	}
	s := &startSheet{kinds: []string{"claude", "codex"}, o: *m.startOver}
	s.key(m, tea.KeyPressMsg{}, "right") // on to the next agent, as Settings starts it
	if s.o.kind != "claude" || s.o.effort != m.startDefaults("claude").effort {
		t.Errorf("another agent starts as its Settings say: %+v", s.o)
	}
}

// Provider and harness are picked apart: Ollama moves from Claude Code to
// Codex on the Harness row and stays Ollama.
func TestStartHarness(t *testing.T) {
	m, _ := benchModel(120, 40)
	s := &startSheet{kinds: []string{"claude", "ollama", "codex", "ollama-codex"}, o: m.startDefaults("ollama"), row: 1}
	if got := s.choices(0); strings.Join(got, ",") != "claude,ollama,codex" {
		t.Errorf("providers: %v", got)
	}
	s.key(m, tea.KeyPressMsg{}, "right")
	if s.o.kind != "ollama-codex" {
		t.Errorf("Ollama in the next harness: %+v", s.o)
	}
	s.row = 0
	s.key(m, tea.KeyPressMsg{}, "right")
	if s.o.kind != "codex" {
		t.Errorf("the next provider, in its own harness: %+v", s.o)
	}
}

// A provider with an API key can be paid for either way; one in another's
// harness only with the key, and only once there is one.
func TestStartBilling(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.store.Config.APIKeys = nil
	t.Setenv("ANTHROPIC_API_KEY", "")
	if got := m.billings("claude"); strings.Join(got, ",") != "" {
		t.Errorf("no key, the subscription alone: %q", got)
	}
	m.store.Config.MarkAPIKey("claude", true)
	if got := m.billings("claude"); len(got) != 2 || got[1] != "key" {
		t.Errorf("with a key, either: %q", got)
	}
	if got := m.billings("ollama"); len(got) != 1 || got[0] != "" {
		t.Errorf("Ollama needs no key: %q", got)
	}
}
