package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

func TestMention(t *testing.T) {
	fix := &fleet.Agent{Key: "a", DisplayName: "fix login bug", Branch: "fix/login"}
	docs := &fleet.Agent{Key: "b", DisplayName: "docs"}
	term := &fleet.Agent{Key: "c", DisplayName: "login page", Interactive: true}
	m := &Model{order: []*fleet.Agent{docs, fix, term}, groupOf: map[string]string{"a": "Working", "b": "Done"}}

	got := m.mentionMatches([]rune("@LOG"), 0)
	if len(got) != 1 || got[0].Name != "fix-login-bug" || got[0].Description != "Working · fix/login" {
		t.Fatalf("search by name, skipping what can't be messaged: %+v", got)
	}
	if got := m.mentionMatches([]rune("@"), 0); len(got) != 2 || got[0].Name != "docs" {
		t.Fatalf("@ alone offers every agent in the list's order: %+v", got)
	}
	if m.mentionMatches([]rune("@docs hi"), 0) != nil {
		t.Fatal("the picker closes once the message is being typed")
	}
	if a, rest := m.mentioned("@Fix-Login-Bug  try again\nplease"); a != fix || rest != "try again\nplease" {
		t.Fatalf("got %v %q", a, rest)
	}
	if a, _ := m.mentioned("@src/main.go explain"); a != nil {
		t.Fatal("an @ that tags no agent is left for the agent")
	}
}
