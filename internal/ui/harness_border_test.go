package ui

import "testing"

func TestHarnessBorderIsQuietAndDistinct(t *testing.T) {
	claude := harnessBorder("claude")
	codex := harnessBorder("codex")
	if claude == codex {
		t.Fatal("Claude and Codex borders have the same tint")
	}
	if claude == lookOf("claude").colour() || codex == lookOf("codex").colour() {
		t.Fatal("a border uses the full-strength harness accent")
	}
}
