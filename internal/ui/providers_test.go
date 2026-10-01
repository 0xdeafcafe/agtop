package ui

import "testing"

// Claude Code is the harness, Anthropic the provider: a session's header
// doesn't call both "Claude", and together they're Provider (Harness).
func TestProviderNameIsNotHarness(t *testing.T) {
	if got := harnessName(string(loginsKind)); got != "Claude Code" {
		t.Errorf("harness = %q, want Claude Code", got)
	}
	if got := agentName(string(loginsKind)); got != "Anthropic (Claude Code)" {
		t.Errorf("label = %q, want Anthropic (Claude Code)", got)
	}
	if got := providerName(string(loginsKind)); got != "Anthropic" {
		t.Errorf("provider = %q, want Anthropic", got)
	}
}
