package headless

import (
	"encoding/json/jsontext"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// FromNeutral is any agent's event as Claude Code would have said it:
// what's built on Claude Code's events (the host's bookkeeping, convo)
// takes other agents' this way. A tool call becomes the Claude tool of its
// kind, with that tool's input and structured result; a tool with no
// Claude counterpart keeps its own name. Events Claude Code has no word
// for (plans, context, a call's input filled in) are left to the caller.
func FromNeutral(ev event.Event) []Event {
	switch e := ev.(type) {
	case event.Init:
		in := Init{SessionID: e.SessionID, Model: e.Model, Cwd: e.Cwd, PermissionMode: e.Mode, Version: e.Version, Tools: e.Tools, SlashCommands: e.Commands}
		for _, m := range e.MCP {
			in.MCPServers = append(in.MCPServers, MCPServer(m))
		}
		return []Event{in}
	case event.MessageStart:
		return []Event{MessageStart{MessageID: e.ID, Model: e.Model}}
	case event.PartStart:
		return []Event{BlockStart{Index: e.Index, Type: blockType(e.Kind)}}
	case event.Delta:
		return []Event{Delta{Index: e.Index, Thinking: e.Kind == event.Thinking, Input: e.Kind == event.ToolCall, Text: e.Text}}
	case event.Message:
		var out []Event
		for _, m := range claudeMessages(e) {
			out = append(out, m)
		}
		return out
	case event.Approval:
		name, input := ClaudeTool(e.Call)
		req := PermissionRequest{ID: e.ID, Tool: name, Input: input, ToolUseID: e.Call.ID, Reason: e.Reason, BlockedPath: e.Path}
		for _, o := range e.Options {
			if o.Kind == event.AllowAlways {
				req.Suggestions = jsontext.Value(`[{"agtop":"always"}]`)
			}
		}
		return []Event{req}
	case event.Question:
		return []Event{PermissionRequest{ID: e.ID, Tool: "AskUserQuestion", Input: questionInput(e), ToolUseID: QuestionCall(e)}}
	case event.ApprovalCancelled:
		return []Event{PermissionCancelled{ID: e.ID}}
	case event.Denied:
		return []Event{PermissionDenied{Tool: e.Tool, ToolUseID: e.CallID, Reason: e.Reason}}
	case event.TurnEnd:
		r := Result{Subtype: "success", CostUSD: e.Cost, DurationMS: int(e.Duration / time.Millisecond), NumTurns: e.Turns, Usage: claudeUsage(e.Tokens)}
		if e.Reason != "done" && e.Reason != "interrupted" {
			r.Subtype, r.IsError, r.Text = "error_"+strings.ReplaceAll(e.Reason, " ", "_"), true, e.Err
		}
		return []Event{r}
	case event.Compacted:
		return []Event{Compact{Trigger: e.Trigger, PreTokens: e.Before, PostTokens: e.After}}
	case event.Limited:
		raw, _ := jsonx.Marshal(map[string]any{"status": "rejected", "rateLimitType": e.Window, "resetsAt": e.ResetsAt.Unix()})
		return []Event{RateLimit{Status: "rejected", Raw: raw}}
	case event.TaskStarted:
		return []Event{TaskStarted{ID: e.ID, ToolUseID: e.CallID, Type: taskType(e.Kind), Description: e.Label, SubagentType: e.Agent, Backgrounded: e.Background}}
	case event.TaskUpdated:
		return []Event{TaskUpdated{ID: e.ID, Status: e.Status, Description: e.Label, Backgrounded: e.Background, Error: e.Err}}
	case event.TaskProgress:
		return []Event{TaskProgress{ID: e.ID, Summary: e.Summary, LastTool: e.LastTool, Tokens: e.Tokens, ToolUses: e.ToolUses}}
	case event.TaskDone:
		return []Event{TaskDone{ID: e.ID, ToolUseID: e.CallID, Status: e.Status, OutputFile: e.OutputFile, Summary: e.Summary}}
	}
	return nil
}

// QuestionCall is the tool call a question is about: its own id when the
// agent gave none.
func QuestionCall(q event.Question) string {
	if q.CallID != "" {
		return q.CallID
	}
	return q.ID
}

var exitCodeRe = regexp.MustCompile(`(?m)^(?:Error: )?Exit code (\d+)`)

// ClaudeTodos is a plan as TodoWrite's todos.
func ClaudeTodos(todos []tool.TodoItem) []map[string]string {
	return claudeTodos(todos)
}

// claudeMessages is a message as Claude Code would send it: one tool
// result to a message, as Claude Code's structured result goes with it.
func claudeMessages(m event.Message) []Message {
	base := Message{Role: m.Role, ID: m.ID, Model: m.Model, ParentToolUseID: m.Parent}
	if m.Tokens != nil {
		u := claudeUsage(*m.Tokens)
		base.Usage = &u
	}
	var out []Message
	cur := base
	for _, p := range m.Parts {
		switch p.Kind {
		case event.Text:
			cur.Blocks = append(cur.Blocks, Block{Type: "text", Text: p.Text})
		case event.Thinking:
			cur.Blocks = append(cur.Blocks, Block{Type: "thinking", Text: p.Text})
		case event.ToolCall:
			if p.Call != nil {
				name, input := ClaudeTool(*p.Call)
				cur.Blocks = append(cur.Blocks, Block{Type: "tool_use", ID: p.Call.ID, Name: name, Input: input})
			}
		case event.ToolResult:
			if p.Output == nil {
				continue
			}
			o := p.Output
			r := base
			r.Usage = nil
			r.Blocks = []Block{{Type: "tool_result", ToolUseID: o.CallID, Text: resultText(o), IsError: o.IsError}}
			r.ToolResult = claudeResult(o)
			out = append(out, r)
		}
	}
	if len(cur.Blocks) > 0 || len(out) == 0 {
		out = append([]Message{cur}, out...)
	}
	return out
}

// claudeNames are Claude Code's tools for each kind of call.
var claudeNames = map[tool.Kind]string{
	tool.Shell: "Bash", tool.Read: "Read", tool.Edit: "Edit", tool.Write: "Write", tool.Search: "Grep",
	tool.Glob: "Glob", tool.Fetch: "WebFetch", tool.WebSearch: "WebSearch", tool.Subagent: "Agent",
	tool.Todo: "TodoWrite", tool.Question: "AskUserQuestion", tool.PlanMode: "ExitPlanMode", tool.Notebook: "NotebookEdit",
}

// ClaudeTool is a call as the Claude tool of its kind, with that tool's
// input; a call of no Claude kind keeps its name and input.
func ClaudeTool(c tool.Call) (string, jsontext.Value) {
	in := c.Input
	name, ok := claudeNames[c.Kind]
	var v map[string]any
	switch {
	case c.Kind == tool.MCP:
		name = "mcp__" + in.Server + "__" + in.Tool
	case !ok:
		name = c.Name
		if name == "" {
			name = c.Kind.String()
		}
	}
	switch c.Kind {
	case tool.Shell:
		v = map[string]any{"command": in.Command, "description": in.Description, "run_in_background": in.Background}
	case tool.Read:
		v = map[string]any{"file_path": in.Path}
		if in.Offset > 0 {
			v["offset"] = in.Offset
		}
		if in.Limit > 0 {
			v["limit"] = in.Limit
		}
	case tool.Edit:
		v = map[string]any{"file_path": in.Path}
		switch len(in.Edits) {
		case 0:
		case 1:
			v["old_string"], v["new_string"], v["replace_all"] = in.Edits[0].Old, in.Edits[0].New, in.Edits[0].All
		default:
			name = "MultiEdit"
			var edits []map[string]any
			for _, e := range in.Edits {
				edits = append(edits, map[string]any{"old_string": e.Old, "new_string": e.New, "replace_all": e.All})
			}
			v["edits"] = edits
		}
	case tool.Write:
		v = map[string]any{"file_path": in.Path, "content": in.Content}
	case tool.Notebook:
		v = map[string]any{"notebook_path": in.Path}
	case tool.Search, tool.Glob:
		v = map[string]any{"pattern": in.Pattern, "path": in.Path}
	case tool.Fetch:
		v = map[string]any{"url": in.URL, "prompt": in.Prompt}
	case tool.WebSearch:
		v = map[string]any{"query": in.Query}
	case tool.Subagent:
		v = map[string]any{"description": in.Description, "prompt": in.Prompt, "subagent_type": in.Agent}
	case tool.Todo:
		v = map[string]any{"todos": claudeTodos(in.Todos)}
	case tool.Question:
		v = map[string]any{"questions": []map[string]any{{"question": c.Title}}}
	default:
		if len(c.Raw) > 0 && c.Raw[0] == '{' {
			return name, c.Raw
		}
		v = map[string]any{}
		for k, x := range map[string]string{"description": in.Description, "command": in.Command, "file_path": in.Path,
			"pattern": in.Pattern, "url": in.URL, "query": in.Query, "prompt": in.Prompt} {
			if x != "" {
				v[k] = x
			}
		}
		if len(v) == 0 && c.Title != "" {
			v["description"] = c.Title
		}
	}
	b, _ := jsonx.Marshal(v)
	return name, b
}

// claudeResult is an output as Claude Code's structured account of a run.
func claudeResult(o *tool.Output) jsontext.Value {
	v := map[string]any{}
	if o.Stdout != "" || o.Stderr != "" {
		v["stdout"], v["stderr"] = o.Stdout, o.Stderr
	}
	if len(o.Patches) > 0 {
		v["structuredPatch"] = o.Patches
	}
	if o.Created {
		v["type"] = "create"
	}
	if l := o.Lines; l != nil {
		v["file"] = map[string]int{"numLines": l.Count, "startLine": l.Start, "totalLines": l.Total}
	}
	if len(v) == 0 {
		return nil
	}
	b, _ := jsonx.Marshal(v)
	return b
}

// resultText is what a call returned, saying how a command exited when it
// failed, as Claude Code's text does.
func resultText(o *tool.Output) string {
	text := o.Text
	if text == "" && (o.Stdout != "" || o.Stderr != "") {
		text = strings.TrimRight(o.Stdout+"\n"+o.Stderr, "\n")
	}
	if o.IsError && o.Exit != nil && *o.Exit > 0 && !exitCodeRe.MatchString(text) {
		text = fmt.Sprintf("Exit code %d\n%s", *o.Exit, text)
	}
	return text
}

func claudeUsage(u usage.TokenUsage) Usage {
	return Usage{InputTokens: int(u.Input), OutputTokens: int(u.Output), CacheReadInputTokens: int(u.CacheRead),
		CacheCreationInputTokens: int(u.CacheWrite5m + u.CacheWrite1h)}
}

func claudeTodos(todos []tool.TodoItem) []map[string]string {
	out := make([]map[string]string, 0, len(todos))
	for _, t := range todos {
		out = append(out, map[string]string{"content": t.Label, "activeForm": t.Active, "status": t.Status})
	}
	return out
}

func questionInput(q event.Question) jsontext.Value {
	var qs []map[string]any
	for _, a := range q.Asks {
		var opts []map[string]string
		for _, o := range a.Options {
			opts = append(opts, map[string]string{"label": o.Label, "description": o.Description, "preview": o.Preview})
		}
		qs = append(qs, map[string]any{"question": a.Text, "header": a.Header, "multiSelect": a.Multi, "options": opts})
	}
	b, _ := jsonx.Marshal(map[string]any{"title": q.Title, "questions": qs})
	return b
}

func blockType(k event.PartKind) string {
	switch k {
	case event.Thinking:
		return "thinking"
	case event.ToolCall:
		return "tool_use"
	}
	return "text"
}

func taskType(k event.TaskKind) string {
	switch k {
	case event.ShellTask:
		return "local_bash"
	case event.SubagentTask:
		return "local_agent"
	case event.MonitorTask:
		return "monitor_mcp"
	case event.WorkflowTask:
		return "local_workflow"
	}
	return "task"
}
