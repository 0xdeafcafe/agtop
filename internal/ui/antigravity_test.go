package ui

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"slices"
	"testing"
)

func TestAntigravityReplacesGeminiForNewSessions(t *testing.T) {
	m, _ := benchModel(160, 50)
	m.store.Config.Dispatch.Kind = "gemini"
	if got := m.startKindIn("/tmp"); got != "antigravity" {
		t.Fatalf("new session still uses %q", got)
	}
	hs := catalogHarnesses()
	if !slices.Contains(hs, agent.Kind("antigravity")) || slices.Contains(hs, agent.Kind("gemini")) {
		t.Fatal(hs)
	}
	if _, ok := agent.As[agent.HistoryReader]("gemini"); !ok {
		t.Fatal("legacy history lost")
	}
	if slices.Contains(agent.Providers(), "gemini") {
		t.Fatal("retired provider remains in model catalog")
	}
}
