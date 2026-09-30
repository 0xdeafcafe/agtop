package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/host"
)

// What the context holds and the session never called is offered to drop,
// the costliest first; what it did use, and rush's own server, aren't.
func TestUnused(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "go"}, at(0))
	s.Apply(event.Message{Role: "assistant", ID: "m1", Parts: []event.Part{
		{Kind: event.ToolCall, Call: &tool.Call{ID: "a", Name: "mcp__linear__list_issues", Kind: tool.MCP, Input: tool.Input{Server: "linear", Tool: "list_issues"}}},
		{Kind: event.ToolCall, Call: &tool.Call{ID: "b", Name: "Task", Kind: tool.Subagent, Input: tool.Input{Agent: "Explore"}}},
	}}, at(1))
	s.Usage = &usage.Context{
		MCPTools: []usage.ContextMCPTool{
			{Name: "list_issues", Server: "linear", Tokens: 500, IsLoaded: true},
			{Name: "browser_click", Server: "playwright", Tokens: 900, IsLoaded: true},
			{Name: "browser_type", Server: "playwright", Tokens: 700, IsLoaded: true},
			{Name: "show", Server: "rush", Tokens: 50, IsLoaded: true},
		},
		Agents: []usage.ContextAgent{{Type: "Explore", Tokens: 300}, {Type: "Plan", Tokens: 200}},
		Skills: usage.ContextSkills{Each: []usage.ContextSkill{{Name: "pdf", Tokens: 120}}},
	}
	got := s.Unused()
	names := make([]string, 0, len(got))
	for _, u := range got {
		names = append(names, u.What+":"+u.Name+":"+strings.Join(u.Rules, ","))
	}
	want := "MCP server:playwright:mcp__playwright|subagent:Plan:Task(Plan),Agent(Plan)|skill:pdf:Skill(pdf)"
	if strings.Join(names, "|") != want {
		t.Errorf("unused:\n got %s\nwant %s", strings.Join(names, "|"), want)
	}
	if got[0].Tokens != 1600 {
		t.Errorf("a server costs all its loaded tools: %d", got[0].Tokens)
	}
}
