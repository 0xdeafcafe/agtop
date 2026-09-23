package ui

import "testing"

func TestReadScreen(t *testing.T) {
	screen := []string{
		"⏺ Reading the host code",
		"",
		"✻ Cogitating… (2m 3s · ↓ 3.4k tokens · esc to interrupt)",
		"  ⎿  ☒ Fix the host",
		"     ☐ Slash picker",
		"",
		"────────────────────────────────",
		"❯ ",
		"────────────────────────────────",
		"  ⏵⏵ accept edits on (shift+tab to cycle)",
		"  Opus 5.5 · ctx 30%",
	}
	si := readScreen(screen)
	if si.working != "✻ Cogitating… (2m 3s · ↓ 3.4k tokens · esc to interrupt)" {
		t.Fatalf("working = %q", si.working)
	}
	if len(si.todos) != 2 || si.todos[0] != "☒ Fix the host" {
		t.Fatalf("todos = %q", si.todos)
	}
	if len(si.status) != 2 || si.status[1] != "Opus 5.5 · ctx 30%" {
		t.Fatalf("status = %q", si.status)
	}
	if readScreen([]string{"nothing here"}).working != "" {
		t.Fatal("no prompt, nothing read")
	}
}
