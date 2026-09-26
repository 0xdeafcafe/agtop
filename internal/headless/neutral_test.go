package headless

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

func neutral(t *testing.T, n *Neutral, line string) []event.Event {
	t.Helper()
	return n.Event(decode(t, line))
}

func TestNeutralFollowsATurn(t *testing.T) {
	var n Neutral
	init := neutral(t, &n, lineInit)[0].(event.Init)
	if init.SessionID != "s-1" || init.Mode != "default" || init.Version != "2.1.280" || len(init.Commands) != 1 {
		t.Errorf("Init = %+v", init)
	}
	if d := neutral(t, &n, lineThink)[0].(event.Delta); d.Kind != event.Thinking || d.Text != "hmm" {
		t.Errorf("thinking Delta = %+v", d)
	}
	m := neutral(t, &n, lineToolUse)[0].(event.Message)
	call := m.Parts[0].Call
	if call == nil || call.Kind != tool.Shell || call.Input.Command != "echo hi" || m.Tokens.CacheRead != 13689 {
		t.Fatalf("tool use = %+v", m)
	}
	a := neutral(t, &n, lineAsk)[0].(event.Approval)
	if a.ID != "req-1" || a.Call.Kind != tool.Shell || len(a.Options) != 3 || a.Options[1].Kind != event.AllowAlways {
		t.Errorf("Approval = %+v", a)
	}
	r := neutral(t, &n, lineToolResult)[0].(event.Message)
	out := r.Parts[0].Output
	if out == nil || out.CallID != "toolu_1" || out.Exit == nil || *out.Exit != 0 {
		t.Errorf("tool result = %+v", r.Parts[0])
	}
	if c := neutral(t, &n, lineCancel)[0].(event.ApprovalCancelled); c.ID != "req-1" {
		t.Errorf("cancel = %+v", c)
	}
	end := neutral(t, &n, lineResult)[0].(event.TurnEnd)
	if end.Reason != "done" || end.Cost != 0.0247 || end.Duration != 6900*time.Millisecond || end.Tokens.Output != 289 {
		t.Errorf("TurnEnd = %+v", end)
	}
	if o := neutral(t, &n, lineHook)[0].(event.Other); o.Adapter != "claude" {
		t.Errorf("hook = %+v", o)
	}
}

func TestNeutralQuestionsAndLimits(t *testing.T) {
	var n Neutral
	ask := `{"type":"control_request","request_id":"req-2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which?","header":"Pick","multiSelect":false,"options":[{"label":"A","description":"a"},{"label":"B","description":"b"}]}]},"tool_use_id":"toolu_9"}}`
	q := neutral(t, &n, ask)[0].(event.Question)
	if q.ID != "req-2" || q.CallID != "toolu_9" || len(q.Asks) != 1 || q.Asks[0].Text != "Which?" || len(q.Asks[0].Options) != 2 {
		t.Errorf("Question = %+v", q)
	}
	limit := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","resetsAt":1790000000,"unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":1790000000}}}}`
	evs := neutral(t, &n, limit)
	if len(evs) != 2 {
		t.Fatalf("rate limit = %+v", evs)
	}
	if q := evs[0].(event.Quota); q.Used("") != 100 {
		t.Errorf("Quota = %+v", q)
	}
	if l := evs[1].(event.Limited); l.Window != "five_hour" || l.ResetsAt.Unix() != 1790000000 {
		t.Errorf("Limited = %+v", l)
	}
}
