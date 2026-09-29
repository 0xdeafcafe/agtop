package claude

import "testing"

func TestModelName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5[1m]":       "Opus 5.5",
		"claude-sonnet-5":           "Sonnet 5",
		"opus":                      "Opus",
		"opus[1m]":                  "Opus",
		"Opus 4.1 (1M context)":     "Opus 4.1",
		"gpt-5-codex":               "gpt-5-codex",
		"claude-haiku-4-5-20251001": "Haiku 4.5",
	} {
		if got := ModelName(in); got != want {
			t.Errorf("ModelName(%q) = %q, want %q", in, got, want)
		}
	}
}
