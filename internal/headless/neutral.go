package headless

import (
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// Neutral turns Claude Code's events into agtop's own, which any agent's
// session sends. It remembers each tool call, so the result that comes
// back later reads by the call's kind. One Neutral follows one session.
type Neutral struct {
	calls map[string]tool.Call
}

// Event is ev as agtop's own events: none for the traffic only the host
// sees (control replies, MCP messages), one for most, and more than one
// where Claude says two things at once.
func (n *Neutral) Event(ev Event) []event.Event {
	if n.calls == nil {
		n.calls = map[string]tool.Call{}
	}
	switch e := ev.(type) {
	case Init:
		out := event.Init{SessionID: e.SessionID, Model: e.Model, Cwd: e.Cwd, Mode: e.PermissionMode, Version: e.Version, Tools: e.Tools, Commands: e.SlashCommands}
		for _, s := range e.MCPServers {
			out.MCP = append(out.MCP, event.MCPServer(s))
		}
		return []event.Event{out}
	case MessageStart:
		return []event.Event{event.MessageStart{ID: e.MessageID, Model: e.Model}}
	case BlockStart:
		return []event.Event{event.PartStart{Index: e.Index, Kind: partKind(e.Type)}}
	case Delta:
		kind := event.Text
		switch {
		case e.Thinking:
			kind = event.Thinking
		case e.Input:
			kind = event.ToolCall
		}
		return []event.Event{event.Delta{Index: e.Index, Kind: kind, Text: e.Text}}
	case Message:
		return []event.Event{n.message(e)}
	case PermissionRequest:
		call := claude.Call(e.ToolUseID, e.Tool, e.Input)
		n.calls[e.ToolUseID] = call
		if call.Kind == tool.Question {
			return []event.Event{question(e)}
		}
		out := event.Approval{ID: e.ID, Call: call, Reason: firstOf(e.Reason, e.Description), Path: e.BlockedPath,
			Options: []event.Option{{ID: "allow", Label: "Allow", Kind: event.AllowOnce}}}
		if len(e.Suggestions) > 0 && string(e.Suggestions) != "null" {
			out.Options = append(out.Options, event.Option{ID: "always", Label: "Always allow", Kind: event.AllowAlways})
		}
		out.Options = append(out.Options, event.Option{ID: "deny", Label: "Deny", Kind: event.RejectOnce})
		return []event.Event{out}
	case PermissionCancelled:
		return []event.Event{event.ApprovalCancelled{ID: e.ID}}
	case PermissionDenied:
		return []event.Event{event.Denied{CallID: e.ToolUseID, Tool: e.Tool, Reason: e.Reason}}
	case Status:
		return []event.Event{event.Status{Busy: e.Status != "", Text: e.Status}}
	case Compact:
		return []event.Event{event.Compacted{Trigger: e.Trigger, Before: e.PreTokens, After: e.PostTokens}}
	case Result:
		end := event.TurnEnd{Reason: turnReason(e.Subtype), Cost: e.CostUSD, Tokens: tokens(e.Usage),
			Duration: time.Duration(e.DurationMS) * time.Millisecond, Turns: e.NumTurns}
		if e.IsError {
			end.Err = e.Text
		} else {
			end.Text = e.Text
		}
		return []event.Event{end}
	case TaskStarted:
		return []event.Event{event.TaskStarted{ID: e.ID, CallID: e.ToolUseID, Kind: taskKind(e.Type), Label: firstOf(e.Description, e.Workflow),
			Agent: e.SubagentType, Background: e.Backgrounded}}
	case TaskUpdated:
		return []event.Event{event.TaskUpdated{ID: e.ID, Status: e.Status, Label: e.Description, Background: e.Backgrounded, Err: e.Error}}
	case TaskProgress:
		return []event.Event{event.TaskProgress{ID: e.ID, Summary: firstOf(e.Summary, e.Description), LastTool: e.LastTool, Tokens: e.Tokens, ToolUses: e.ToolUses}}
	case TaskDone:
		return []event.Event{event.TaskDone{ID: e.ID, CallID: e.ToolUseID, Status: e.Status, OutputFile: e.OutputFile, Summary: e.Summary}}
	case BackgroundTasks:
		out := event.Background{Tasks: []event.BackgroundTask{}}
		for _, t := range e.Tasks {
			out.Tasks = append(out.Tasks, event.BackgroundTask{ID: t.ID, Kind: taskKind(t.Type), Type: t.Type, Label: t.Description})
		}
		return []event.Event{out}
	case RateLimit:
		var out []event.Event
		if u, ok := claude.LiveUsage(e.Raw, time.Now()); ok {
			q := u.Quota("")
			q.Source = usage.Live
			out = append(out, event.Quota{Quota: q})
		}
		if e.Status == "rejected" {
			var r struct {
				Type     string `json:"rateLimitType"`
				ResetsAt int64  `json:"resetsAt"`
			}
			_ = jsonx.Unmarshal(e.Raw, &r)
			l := event.Limited{Window: r.Type}
			if r.ResetsAt > 0 {
				l.ResetsAt = time.Unix(r.ResetsAt, 0)
			}
			out = append(out, l)
		}
		return out
	case ControlReply, MCPRequest:
		return nil
	case Other:
		return []event.Event{event.Other{Adapter: "claude", Type: e.Type + "/" + e.Subtype, Raw: e.Raw}}
	}
	return nil
}

func (n *Neutral) message(m Message) event.Message {
	out := event.Message{Role: m.Role, ID: m.ID, Model: m.Model, Parent: m.ParentToolUseID}
	if m.Usage != nil {
		t := tokens(*m.Usage)
		out.Tokens = &t
	}
	for _, b := range m.Blocks {
		switch b.Type {
		case "text":
			out.Parts = append(out.Parts, event.Part{Kind: event.Text, Text: b.Text})
		case "thinking", "redacted_thinking":
			out.Parts = append(out.Parts, event.Part{Kind: event.Thinking, Text: b.Text})
		case "tool_use":
			c := claude.Call(b.ID, b.Name, b.Input)
			n.calls[b.ID] = c
			out.Parts = append(out.Parts, event.Part{Kind: event.ToolCall, Call: &c})
		case "tool_result":
			c, ok := n.calls[b.ToolUseID]
			if !ok {
				c = tool.Call{ID: b.ToolUseID}
			}
			o := claude.Output(c, b.Text, b.IsError, m.ToolResult)
			out.Parts = append(out.Parts, event.Part{Kind: event.ToolResult, Output: &o})
		}
	}
	return out
}

// question is an AskUserQuestion request as the question it asks.
func question(r PermissionRequest) event.Question {
	title, qs := r.Questions()
	out := event.Question{ID: r.ID, CallID: r.ToolUseID, Title: title}
	for _, q := range qs {
		a := event.Ask{Header: q.Header, Text: q.Question, Multi: q.MultiSelect}
		for _, o := range q.Options {
			a.Options = append(a.Options, event.Choice{Label: o.Label, Description: o.Description, Preview: o.Preview})
		}
		out.Asks = append(out.Asks, a)
	}
	return out
}

// taskKind is what a task of Claude Code's type is.
func taskKind(t string) event.TaskKind {
	switch t {
	case "local_bash":
		return event.ShellTask
	case "local_agent":
		return event.SubagentTask
	case "monitor_mcp":
		return event.MonitorTask
	case "local_workflow":
		return event.WorkflowTask
	}
	return event.OtherTask
}

func partKind(t string) event.PartKind {
	switch t {
	case "thinking", "redacted_thinking":
		return event.Thinking
	case "tool_use":
		return event.ToolCall
	}
	return event.Text
}

func turnReason(subtype string) string {
	switch subtype {
	case "success":
		return "done"
	case "error_max_turns":
		return "max_turns"
	}
	return "error"
}

// tokens is a request's usage. The stream doesn't split cache writes by
// lifetime; they count as the hour Claude Code asks for.
func tokens(u Usage) usage.TokenUsage {
	return usage.TokenUsage{Input: int64(u.InputTokens), Output: int64(u.OutputTokens),
		CacheRead: int64(u.CacheReadInputTokens), CacheWrite1h: int64(u.CacheCreationInputTokens)}
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
