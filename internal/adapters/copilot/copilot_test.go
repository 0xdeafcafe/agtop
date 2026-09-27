package copilot

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

// A log as the coding agent writes it, trimmed.
const log = `data: {"id":"clone-repo","created":1776968719202,"object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"clone-repo","function":{"name":"run_setup","arguments":"{\"name\":\"Clone repository o/r\"}"}}]}}]}

data: {"id":"clone-repo","created":1776968724426,"object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"content":"Cloned o/r","role":"assistant","tool_calls":[{"index":0,"id":"clone-repo","function":{"name":"run_setup","arguments":"{\"name\":\"Clone repository o/r\"}"}}]}}]}

data: {"id":"mcp","created":"1776968730","object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"content":"MCP server started","role":"assistant","tool_calls":[{"index":0,"id":"mcp","function":{"name":"run_setup","arguments":"{\"name\":\"Start MCP\"}"}}]}}]}

data: {"id":"msg_1","created":1776968740000,"object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"content":"Let me look.","role":"assistant"}}]}

data: {"id":"msg_1","created":1776968741000,"object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"t1","function":{"name":"bash","arguments":"{\"command\":\"cd /home/runner/work/r/r && go test ./...\",\"description\":\"Run tests\"}"}}]}}]}

data: {"id":"msg_1","created":1776968745000,"object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"content":"ok","role":"assistant","tool_calls":[{"index":0,"id":"t1","function":{"name":"bash","arguments":"{}"}}]}}]}

data: {"id":"msg_2","created":1776968750000,"object":"chat.completion.chunk","choices":[{"finish_reason":"stop","index":0,"delta":{"content":"Done: tests pass.","role":"assistant"}}]}
`

func TestReadLog(t *testing.T) {
	evs := readLog(strings.NewReader(log), time.Time{})
	var calls, results, texts []string
	for _, ev := range evs {
		m := ev.(event.Message)
		for _, p := range m.Parts {
			switch p.Kind {
			case event.ToolCall:
				calls = append(calls, p.Call.ID)
			case event.ToolResult:
				results = append(results, p.Output.CallID+"="+p.Output.Text)
			case event.Text:
				texts = append(texts, p.Text)
			}
		}
	}
	if strings.Join(calls, ",") != "clone-repo,mcp,t1" {
		t.Errorf("calls = %v", calls)
	}
	if strings.Join(results, ",") != "clone-repo=Cloned o/r,mcp=MCP server started,t1=ok" {
		t.Errorf("results = %v", results)
	}
	if strings.Join(texts, "|") != "Let me look.|Done: tests pass." {
		t.Errorf("texts = %v", texts)
	}
	// Up to a time: the run that finished at 1776968745 isn't in yet.
	early := readLog(strings.NewReader(log), time.UnixMilli(1776968742000))
	if n := len(early); n >= len(evs) || n == 0 {
		t.Errorf("before cut %d of %d events", n, len(evs))
	}
}

func TestCallOf(t *testing.T) {
	c := callOf("t1", "bash", `{"command":"cd /home/runner/work/r/r && go test ./...","description":"Run tests"}`)
	if c.Kind != tool.Shell || c.Input.Command != "go test ./..." || c.Input.Description != "Run tests" {
		t.Errorf("bash = %+v", c.Input)
	}
	c = callOf("t2", "edit", `{"path":"/home/runner/work/r/r/app/x.ts","old_str":"a","new_str":"b"}`)
	if c.Kind != tool.Edit || c.Input.Path != "app/x.ts" || c.Input.Edits[0].New != "b" {
		t.Errorf("edit = %+v", c.Input)
	}
	c = callOf("t3", "github-mcp-server-get_job_logs", `{}`)
	if c.Kind != tool.MCP || c.Input.Server != "github-mcp" || c.Input.Tool != "get_job_logs" {
		t.Errorf("mcp = %+v", c.Input)
	}
}

func TestQuotaOf(t *testing.T) {
	var u user
	u.Plan, u.ResetsAt = "individual", "2026-10-01T00:00:00.000Z"
	u.Quotas = map[string]struct {
		Entitlement float64 `json:"entitlement"`
		Remaining   float64 `json:"remaining"`
		Percent     float64 `json:"percent_remaining"`
		Unlimited   bool    `json:"unlimited"`
		Overage     bool    `json:"overage_permitted"`
	}{
		"chat":                 {Unlimited: true, Percent: 100},
		"premium_interactions": {Entitlement: 1500, Remaining: 70, Percent: 4.6},
	}
	q := quotaOf(u, time.Now())
	if len(q.Windows) != 1 {
		t.Fatalf("windows = %+v", q.Windows)
	}
	w := q.Windows[0]
	if w.Used != 1430 || w.Limit != 1500 || w.Percent < 95 || w.ResetsAt.Month() != time.October {
		t.Errorf("premium requests = %+v", w)
	}
}
