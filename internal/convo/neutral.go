package convo

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// applyNeutral folds in an event from any agent. The model below was made
// for Claude Code and still speaks its shapes: a tool call becomes the
// Claude tool of its kind, with that tool's input and structured result,
// so every agent's steps draw as Claude's do. Tools with no Claude
// counterpart keep their own names and draw by name.
func (s *Session) applyNeutral(ev event.Event, now time.Time) {
	switch e := ev.(type) {
	case event.Init:
		in := headless.Init{SessionID: e.SessionID, Model: e.Model, Cwd: e.Cwd, PermissionMode: e.Mode, Version: e.Version, Tools: e.Tools, SlashCommands: e.Commands}
		for _, m := range e.MCP {
			in.MCPServers = append(in.MCPServers, headless.MCPServer(m))
		}
		s.Apply(in, now)
	case event.MessageStart:
		s.Apply(headless.MessageStart{MessageID: e.ID, Model: e.Model}, now)
	case event.PartStart:
		s.Apply(headless.BlockStart{Index: e.Index, Type: blockType(e.Kind)}, now)
	case event.Delta:
		s.Apply(headless.Delta{Index: e.Index, Thinking: e.Kind == event.Thinking, Input: e.Kind == event.ToolCall, Text: e.Text}, now)
	case event.Message:
		for _, m := range claudeMessages(e) {
			s.Apply(m, now)
		}
	case event.CallUpdated:
		if st := s.byID[e.Call.ID]; st != nil {
			st.Tool, st.Input = claudeTool(e.Call)
			s.touchStep(st)
		}
	case event.Approval:
		s.ensureStep(e.Call, now)
		name, input := claudeTool(e.Call)
		req := headless.PermissionRequest{ID: e.ID, Tool: name, Input: input, ToolUseID: e.Call.ID, Reason: e.Reason, BlockedPath: e.Path}
		for _, o := range e.Options {
			if o.Kind == event.AllowAlways {
				req.Suggestions = json.RawMessage(`[{"agtop":"always"}]`)
			}
		}
		s.Apply(req, now)
	case event.Question:
		call := tool.Call{ID: e.CallID, Name: "AskUserQuestion", Kind: tool.Question}
		if call.ID == "" {
			call.ID = e.ID
		}
		s.ensureStep(call, now)
		s.Apply(headless.PermissionRequest{ID: e.ID, Tool: "AskUserQuestion", Input: questionInput(e), ToolUseID: call.ID}, now)
	case event.ApprovalCancelled:
		s.Apply(headless.PermissionCancelled{ID: e.ID}, now)
	case event.Denied:
		s.Apply(headless.PermissionDenied{Tool: e.Tool, ToolUseID: e.CallID, Reason: e.Reason}, now)
	case event.TurnEnd:
		if e.Reason == "interrupted" {
			s.interrupted(now)
			return
		}
		r := headless.Result{Subtype: "success", CostUSD: e.Cost, DurationMS: int(e.Duration / time.Millisecond), NumTurns: e.Turns, Usage: claudeUsage(e.Tokens)}
		if e.Reason != "done" {
			r.Subtype, r.IsError, r.Text = "error_"+strings.ReplaceAll(e.Reason, " ", "_"), true, e.Err
		}
		s.Apply(r, now)
	case event.Compacted:
		s.Apply(headless.Compact{Trigger: e.Trigger, PreTokens: e.Before, PostTokens: e.After}, now)
	case event.Limited:
		s.Limit = "rejected"
	case event.Context:
		s.Context = e.Tokens
	case event.Plan:
		b, _ := json.Marshal(map[string]any{"todos": claudeTodos(e.Todos)})
		s.tasksFromInput(&Step{Tool: "TodoWrite", Input: b})
	case event.TaskStarted:
		s.Apply(headless.TaskStarted{ID: e.ID, ToolUseID: e.CallID, Type: taskType(e.Kind), Description: e.Label, SubagentType: e.Agent, Backgrounded: e.Background}, now)
	case event.TaskUpdated:
		s.Apply(headless.TaskUpdated{ID: e.ID, Status: e.Status, Description: e.Label, Backgrounded: e.Background, Error: e.Err}, now)
	case event.TaskProgress:
		s.Apply(headless.TaskProgress{ID: e.ID, Summary: e.Summary, LastTool: e.LastTool, Tokens: e.Tokens, ToolUses: e.ToolUses}, now)
	case event.TaskDone:
		s.Apply(headless.TaskDone{ID: e.ID, ToolUseID: e.CallID, Status: e.Status, OutputFile: e.OutputFile, Summary: e.Summary}, now)
	}
}

// ensureStep makes a step of a call an approval or question is about,
// when the agent asks before it says it's making the call.
func (s *Session) ensureStep(c tool.Call, now time.Time) {
	if c.ID == "" || s.byID[c.ID] != nil {
		return
	}
	name, input := claudeTool(c)
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: c.ID, Name: name, Input: input}}}, now)
}

// claudeMessages is a message as Claude Code would send it: one tool
// result to a message, as Claude Code's structured result goes with it.
func claudeMessages(m event.Message) []headless.Message {
	base := headless.Message{Role: m.Role, ID: m.ID, Model: m.Model, ParentToolUseID: m.Parent}
	if m.Tokens != nil {
		u := claudeUsage(*m.Tokens)
		base.Usage = &u
	}
	var out []headless.Message
	cur := base
	for _, p := range m.Parts {
		switch p.Kind {
		case event.Text:
			cur.Blocks = append(cur.Blocks, headless.Block{Type: "text", Text: p.Text})
		case event.Thinking:
			cur.Blocks = append(cur.Blocks, headless.Block{Type: "thinking", Text: p.Text})
		case event.ToolCall:
			if p.Call != nil {
				name, input := claudeTool(*p.Call)
				cur.Blocks = append(cur.Blocks, headless.Block{Type: "tool_use", ID: p.Call.ID, Name: name, Input: input})
			}
		case event.ToolResult:
			if p.Output == nil {
				continue
			}
			o := p.Output
			r := base
			r.Usage = nil
			r.Blocks = []headless.Block{{Type: "tool_result", ToolUseID: o.CallID, Text: resultText(o), IsError: o.IsError}}
			r.ToolResult = claudeResult(o)
			out = append(out, r)
		}
	}
	if len(cur.Blocks) > 0 || len(out) == 0 {
		out = append([]headless.Message{cur}, out...)
	}
	return out
}

// claudeNames are Claude Code's tools for each kind of call.
var claudeNames = map[tool.Kind]string{
	tool.Shell: "Bash", tool.Read: "Read", tool.Edit: "Edit", tool.Write: "Write", tool.Search: "Grep",
	tool.Glob: "Glob", tool.Fetch: "WebFetch", tool.WebSearch: "WebSearch", tool.Subagent: "Agent",
	tool.Todo: "TodoWrite", tool.Question: "AskUserQuestion", tool.PlanMode: "ExitPlanMode", tool.Notebook: "NotebookEdit",
}

// claudeTool is a call as the Claude tool of its kind, with that tool's
// input; a call of no Claude kind keeps its name and input.
func claudeTool(c tool.Call) (string, json.RawMessage) {
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
	b, _ := json.Marshal(v)
	return name, b
}

// claudeResult is an output as Claude Code's structured account of a run.
func claudeResult(o *tool.Output) json.RawMessage {
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
	b, _ := json.Marshal(v)
	return b
}

// resultText is what a call returned, saying how a command exited when it
// failed, as Claude Code's text does.
func resultText(o *tool.Output) string {
	text := o.Text
	if text == "" && (o.Stdout != "" || o.Stderr != "") {
		text = strings.TrimRight(o.Stdout+"\n"+o.Stderr, "\n")
	}
	if o.IsError && o.Exit != nil && *o.Exit > 0 && !exitRe.MatchString(text) {
		text = fmt.Sprintf("Exit code %d\n%s", *o.Exit, text)
	}
	return text
}

func claudeUsage(u usage.TokenUsage) headless.Usage {
	return headless.Usage{InputTokens: int(u.Input), OutputTokens: int(u.Output), CacheReadInputTokens: int(u.CacheRead),
		CacheCreationInputTokens: int(u.CacheWrite5m + u.CacheWrite1h)}
}

func claudeTodos(todos []tool.TodoItem) []map[string]string {
	out := make([]map[string]string, 0, len(todos))
	for _, t := range todos {
		out = append(out, map[string]string{"content": t.Label, "activeForm": t.Active, "status": t.Status})
	}
	return out
}

func questionInput(q event.Question) json.RawMessage {
	var qs []map[string]any
	for _, a := range q.Asks {
		var opts []map[string]string
		for _, o := range a.Options {
			opts = append(opts, map[string]string{"label": o.Label, "description": o.Description, "preview": o.Preview})
		}
		qs = append(qs, map[string]any{"question": a.Text, "header": a.Header, "multiSelect": a.Multi, "options": opts})
	}
	b, _ := json.Marshal(map[string]any{"title": q.Title, "questions": qs})
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
