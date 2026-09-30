package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// Every unused thing is ticked to drop; unticked, it stays; what the
// session went without already stays gone.
func TestSlimDrops(t *testing.T) {
	m, _ := benchModel(120, 40)
	s := &slimSheet{keep: map[int]bool{}, was: []string{"mcp__old"}, items: []convo.Unused{
		{What: "MCP server", Name: "playwright", Tokens: 1600, Rules: []string{"mcp__playwright"}},
		{What: "skill", Name: "pdf", Tokens: 120, Rules: []string{"Skill(pdf)"}},
	}}
	rules, tokens := s.drops()
	if strings.Join(rules, ",") != "mcp__old,mcp__playwright,Skill(pdf)" || tokens != 1720 {
		t.Errorf("all ticked: %v %d", rules, tokens)
	}
	s.key(m, tea.KeyPressMsg{}, "space") // keep playwright
	if rules, tokens = s.drops(); strings.Join(rules, ",") != "mcp__old,Skill(pdf)" || tokens != 120 {
		t.Errorf("playwright kept: %v %d", rules, tokens)
	}
	s.key(m, tea.KeyPressMsg{}, "a") // one dropped: keep them all
	if _, tokens = s.drops(); tokens != 0 {
		t.Errorf("a keeps all when any was dropped: %d", tokens)
	}
	s.key(m, tea.KeyPressMsg{}, "a")
	if _, tokens = s.drops(); tokens != 1720 {
		t.Errorf("a again drops all: %d", tokens)
	}
}
