package ui

import "testing"

// Claude Code is the harness, Anthropic the provider: a session's header
// doesn't call both "Claude".
func TestProviderNameIsNotHarness(t *testing.T) {
	if got := agentName(string(loginsKind)); got != "Claude Code" {
		t.Errorf("harness = %q, want Claude Code", got)
	}
	if got := providerName(string(loginsKind)); got != "Anthropic" {
		t.Errorf("provider = %q, want Anthropic", got)
	}
}
