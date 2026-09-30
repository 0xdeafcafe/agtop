package convo

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A turn a background task woke reads as Claude Code says it.
func TestWokeReadsAsClaudeCode(t *testing.T) {
	noun, how, ok := woke(&Turn{From: "background shell · completed", Cause: "Format and lint"})
	if !ok || noun != "Background command" || ansi.Strip(how) != " completed" {
		t.Fatalf("got %q %q %v", noun, how, ok)
	}
	if _, _, ok := woke(&Turn{From: "session abc", Cause: ""}); ok {
		t.Fatal("another session's message isn't a task waking it")
	}
}

// rush carrying a session on after an account switch reads as rush's.
func TestSwitchedReadsAsRush(t *testing.T) {
	if !switched(&Turn{Prompt: "continue: you're on another account now, with room, so the usage limit no longer applies."}) {
		t.Fatal("rush's continue isn't read as a switch")
	}
	if switched(&Turn{Prompt: "continue"}) {
		t.Fatal("your own continue is read as rush's")
	}
}
