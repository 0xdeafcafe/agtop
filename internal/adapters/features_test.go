// Package adapters_test checks every adapter together.
package adapters_test

import (
	"testing"

	_ "github.com/0xdeafcafe/agtop/internal/adapters/acp"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/codex"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/glm"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/ollama"
	"github.com/0xdeafcafe/agtop/internal/agent"
)

// implements are the features that are an interface: an adapter has the
// feature exactly when it has the interface.
var implements = map[agent.Feature]func(agent.Adapter) bool{
	agent.FeatureRun:      func(a agent.Adapter) bool { _, ok := a.(agent.Driver); return ok },
	agent.FeatureLive:     func(a agent.Adapter) bool { _, ok := a.(agent.Discoverer); return ok },
	agent.FeatureHistory:  func(a agent.Adapter) bool { _, ok := a.(agent.HistoryReader); return ok },
	agent.FeatureQuota:    func(a agent.Adapter) bool { _, ok := a.(agent.QuotaSource); return ok },
	agent.FeatureSwitch:   func(a agent.Adapter) bool { _, ok := a.(agent.Accounts); return ok },
	agent.FeatureSignIn:   func(a agent.Adapter) bool { _, ok := a.(agent.Accounts); return ok },
	agent.FeaturePricing:  func(a agent.Adapter) bool { _, ok := a.(agent.Pricer); return ok },
	agent.FeatureCommands: func(a agent.Adapter) bool { _, ok := a.(agent.Commander); return ok },
}

// A feature an adapter declares Yes has its interface, and an interface
// it has is declared Yes.
func TestFeaturesMatchInterfaces(t *testing.T) {
	if len(agent.All()) < 9 {
		t.Fatalf("only %d adapters registered", len(agent.All()))
	}
	for _, a := range agent.All() {
		for f, has := range implements {
			yes := agent.Supports(a.Kind(), f)
			if yes != has(a) {
				t.Errorf("%s: %s declared %v, but its interface implemented is %v", a.Kind(), f, agent.FeatureOf(a.Kind(), f).Is, has(a))
			}
		}
	}
}

// Every feature an adapter declares is one agtop knows, and has a label.
func TestFeaturesKnown(t *testing.T) {
	known := map[agent.Feature]bool{}
	for _, i := range agent.AllFeatures() {
		if i.Label == "" || known[i.Feature] {
			t.Errorf("%s: no label, or listed twice", i.Feature)
		}
		known[i.Feature] = true
	}
	for _, a := range agent.All() {
		for f := range a.Features() {
			if !known[f] {
				t.Errorf("%s declares %q, which isn't in AllFeatures", a.Kind(), f)
			}
		}
	}
}

// Each agent's level is as far as it has been tried.
func TestLevels(t *testing.T) {
	want := map[agent.Kind]agent.Level{
		"claude": agent.LevelFull, "codex": agent.LevelTested, "copilot": agent.LevelTested,
		"deepseek": agent.LevelPreview, "glm": agent.LevelPreview, "kimi": agent.LevelPreview,
		"vibe": agent.LevelPreview, "gemini": agent.LevelPreview, "opencode": agent.LevelPreview,
	}
	for k, l := range want {
		if got := agent.LevelOf(k); got != l {
			t.Errorf("%s is %s, want %s", k, got, l)
		}
	}
}

// Claude Code is the built-in agent: what an empty kind means, and whose
// program fleet looks for.
func TestBuiltin(t *testing.T) {
	if k := agent.KindOf(""); k != "claude" || !agent.IsBuiltin(k) || !agent.IsBuiltin("") {
		t.Errorf("an empty kind is %q", k)
	}
	if agent.IsBuiltin("codex") || agent.KindOf("codex") != "codex" {
		t.Error("Codex is taken as built in")
	}
	if !agent.IsProgram("/opt/homebrew/bin/claude") || !agent.IsProgram("codex") || agent.IsProgram("zsh") {
		t.Error("agents' programs aren't told from others")
	}
}
