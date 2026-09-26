package acp

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

// call is a tool call as the updates so far describe it.
type call struct {
	c       tool.Call
	acpKind string
	content []toolContent
	locs    []location
	sent    bool // its ToolCall message has gone out
	done    bool // and its ToolResult
}

// notified takes the agent's notifications.
func (s *Session) notified(method string, params json.RawMessage) {
	switch method {
	case "session/update":
		var n struct {
			Update json.RawMessage `json:"update"`
		}
		if json.Unmarshal(params, &n) == nil {
			s.update(n.Update)
		}
	case "$/cancel_request":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) == nil {
			s.withdraw(p.RequestID)
		}
	}
}

// update turns one session/update into events.
func (s *Session) update(raw json.RawMessage) {
	var u struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch u.SessionUpdate {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk":
		var c chunk
		if json.Unmarshal(raw, &c) != nil {
			return
		}
		role, kind := "assistant", event.Text
		switch u.SessionUpdate {
		case "agent_thought_chunk":
			kind = event.Thinking
		case "user_message_chunk":
			role = "user"
		}
		s.chunk(role, kind, c.MessageID, c.Content)
	case "tool_call", "tool_call_update":
		var tc toolCall
		if json.Unmarshal(raw, &tc) == nil {
			s.track(tc)
		}
	case "plan":
		var p struct {
			Entries []planEntry `json:"entries"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		todos := make([]tool.TodoItem, len(p.Entries))
		for i, e := range p.Entries {
			todos[i] = tool.TodoItem{Label: e.Content, Status: e.Status}
		}
		s.emit(event.Plan{Todos: todos})
	case "available_commands_update":
		var p struct {
			AvailableCommands []struct {
				Name string `json:"name"`
			} `json:"availableCommands"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		s.commands = s.commands[:0]
		for _, c := range p.AvailableCommands {
			s.commands = append(s.commands, c.Name)
		}
		s.emitInit()
	case "current_mode_update":
		var p struct {
			CurrentModeID string `json:"currentModeId"`
		}
		if json.Unmarshal(raw, &p) == nil && p.CurrentModeID != s.mode {
			s.mode = p.CurrentModeID
			s.emitInit()
		}
	case "config_option_update":
		var p struct {
			ConfigOptions []configOption `json:"configOptions"`
		}
		if json.Unmarshal(raw, &p) == nil {
			s.setConfig(p.ConfigOptions)
			s.emitInit()
		}
	case "usage_update":
		var p struct {
			Used, Size int
			Cost       *struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"cost"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		if p.Cost != nil && (p.Cost.Currency == "" || strings.EqualFold(p.Cost.Currency, "USD")) {
			s.cost = p.Cost.Amount
		}
		s.emit(event.Context{Tokens: p.Used, Window: p.Size})
	default:
		s.emit(event.Other{Adapter: s.o.Adapter, Type: u.SessionUpdate, Raw: append(json.RawMessage(nil), raw...)})
	}
}

// chunk adds a piece of a message. ACP streams messages and never sends
// them whole, so a message is kept open and goes out whole, as a Message,
// when another begins or a tool call or the turn ends. An assistant's is
// streamed as it comes as well.
func (s *Session) chunk(role string, kind event.PartKind, id string, b contentBlock) {
	if m := s.open; m != nil && (m.Role != role || (id != "" && m.ID != "" && id != m.ID)) {
		s.flush()
	}
	stream := role == "assistant"
	if s.open == nil {
		s.open = &event.Message{Role: role, ID: id, Model: s.model()}
		if stream {
			s.emit(event.MessageStart{ID: id, Model: s.open.Model})
		}
	}
	m := s.open
	switch b.Type {
	case "text":
		n := len(m.Parts)
		if n == 0 || m.Parts[n-1].Kind != kind {
			m.Parts = append(m.Parts, event.Part{Kind: kind})
			if stream {
				s.emit(event.PartStart{Index: n, Kind: kind})
			}
			n++
		}
		m.Parts[n-1].Text += b.Text
		if stream {
			s.emit(event.Delta{Index: n - 1, Kind: kind, Text: b.Text})
		}
	case "image":
		data, _ := base64.StdEncoding.DecodeString(b.Data)
		m.Parts = append(m.Parts, event.Part{Kind: event.Image, Image: &event.ImageData{MediaType: b.MimeType, Data: data}})
	case "resource_link":
		m.Parts = append(m.Parts, event.Part{Kind: kind, Text: b.URI})
	}
}

// flush sends the open message whole.
func (s *Session) flush() {
	if m := s.open; m != nil {
		s.open = nil
		if len(m.Parts) > 0 {
			s.emit(*m)
		}
	}
}

// track folds a tool call or an update to one into what's known of it,
// and says so: the call once, and its result once it has finished.
func (s *Session) track(tc toolCall) *call {
	c := s.calls[tc.ToolCallID]
	if c == nil {
		c = &call{c: tool.Call{ID: tc.ToolCallID}}
		s.calls[tc.ToolCallID] = c
	}
	if tc.Title != nil {
		c.c.Title = *tc.Title
	}
	if tc.Name != nil {
		c.c.Name = *tc.Name
	}
	if tc.Kind != nil {
		c.acpKind = *tc.Kind
		c.c.Kind = kindOf(c.acpKind)
	}
	if raw := tc.RawInput; len(raw) > 0 && string(raw) != "null" {
		c.c.Raw = append(json.RawMessage(nil), raw...)
	}
	if tc.Content != nil {
		c.content = tc.Content
	}
	if tc.Locations != nil {
		c.locs = tc.Locations
	}
	if c.c.Name == "" {
		c.c.Name = c.acpKind
	}
	c.c.Input = readInput(c.c.Kind, c.c.Raw, c.locs, c.content)
	if c.sent && (tc.Title != nil || tc.Kind != nil || len(tc.RawInput) > 0 || tc.Content != nil || tc.Locations != nil) {
		s.emit(event.CallUpdated{Call: c.c})
	}
	if !c.sent {
		c.sent = true
		s.flush()
		cc := c.c
		s.emit(event.Message{Role: "assistant", Model: s.model(), Parts: []event.Part{{Kind: event.ToolCall, Call: &cc}}})
	}
	if st := deref(tc.Status); !c.done && (st == "completed" || st == "failed") {
		c.done = true
		out := readOutput(c.c.ID, st == "failed", c.content, tc.RawOutput)
		s.emit(event.Message{Role: "user", Parts: []event.Part{{Kind: event.ToolResult, Output: out}}})
	}
	return c
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// kindOf is ACP's ToolKind as agtop's.
func kindOf(k string) tool.Kind {
	switch k {
	case "read":
		return tool.Read
	case "edit":
		return tool.Edit
	case "delete":
		return tool.Delete
	case "move":
		return tool.Move
	case "search":
		return tool.Search
	case "execute":
		return tool.Shell
	case "think":
		return tool.Think
	case "fetch":
		return tool.Fetch
	case "switch_mode":
		return tool.PlanMode
	}
	return tool.Other
}

// readInput reads what it can of a call's input. ACP leaves rawInput to
// each agent, so it looks for the names agents commonly use; locations and
// diffs, which ACP does define, fill in the rest.
func readInput(k tool.Kind, raw json.RawMessage, locs []location, content []toolContent) tool.Input {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	str := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := m[key].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	in := tool.Input{
		Path:        str("path", "file_path", "filePath", "absolute_path", "filename", "file"),
		Pattern:     str("pattern", "regex", "glob"),
		URL:         str("url"),
		Description: str("description"),
		Cwd:         str("cwd", "workdir"),
		Prompt:      str("prompt"),
	}
	switch v := m["command"].(type) {
	case string:
		in.Command = v
	case []any:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		in.Command = strings.Join(parts, " ")
	}
	if in.Command == "" {
		in.Command = str("cmd")
	}
	switch k {
	case tool.Search:
		if in.Pattern == "" {
			in.Pattern = str("query")
		}
	case tool.Move:
		in.To = str("new_path", "newPath", "destination", "to")
		if in.Path == "" {
			in.Path = str("old_path", "oldPath", "source", "from")
		}
	case tool.Edit:
		if old, nw := str("old_string", "oldString", "old_str"), str("new_string", "newString", "new_str"); old != "" || nw != "" {
			all, _ := m["replace_all"].(bool)
			in.Edits = []tool.Replace{{Old: old, New: nw, All: all}}
		}
		in.Content = str("content")
	}
	if in.Path == "" && len(locs) > 0 {
		in.Path = locs[0].Path
	}
	var diffs []tool.Replace
	for _, c := range content {
		if c.Type != "diff" {
			continue
		}
		if in.Path == "" {
			in.Path = c.Path
		}
		r := tool.Replace{Old: deref(c.OldText), New: c.NewText}
		if c.Path != in.Path {
			r.Path = c.Path
		}
		diffs = append(diffs, r)
	}
	if diffs != nil {
		in.Edits = diffs
	}
	return in
}

// readOutput reads a finished call's result.
func readOutput(id string, failed bool, content []toolContent, raw json.RawMessage) *tool.Output {
	out := &tool.Output{CallID: id, IsError: failed}
	if len(raw) > 0 && string(raw) != "null" {
		out.Raw = append(json.RawMessage(nil), raw...)
	}
	var texts []string
	for _, c := range content {
		switch c.Type {
		case "content":
			if c.Content.Type == "text" {
				texts = append(texts, c.Content.Text)
			}
		case "diff":
			out.Patches = append(out.Patches, patch(c))
			out.Created = out.Created || c.OldText == nil
		}
	}
	out.Text = strings.Join(texts, "\n")
	if out.Text != "" || out.Raw == nil {
		return out
	}
	var v any
	_ = json.Unmarshal(out.Raw, &v)
	switch v := v.(type) {
	case string:
		out.Text = v
	case map[string]any:
		get := func(keys ...string) string {
			for _, k := range keys {
				if s, ok := v[k].(string); ok {
					return s
				}
			}
			return ""
		}
		out.Stdout, out.Stderr = get("stdout"), get("stderr")
		out.Text = get("output", "content", "result", "error")
		if out.Text == "" {
			out.Text = strings.TrimSpace(out.Stdout + "\n" + out.Stderr)
		}
		for _, k := range []string{"exit_code", "exitCode", "code"} {
			if n, ok := v[k].(float64); ok {
				e := int(n)
				out.Exit = &e
				break
			}
		}
	}
	return out
}

// patch is a diff as one hunk: the lines that changed, without the ones
// both sides share at either end.
func patch(d toolContent) tool.Patch {
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	old, nw := split(deref(d.OldText)), split(d.NewText)
	pre := 0
	for pre < len(old) && pre < len(nw) && old[pre] == nw[pre] {
		pre++
	}
	suf := 0
	for suf < len(old)-pre && suf < len(nw)-pre && old[len(old)-1-suf] == nw[len(nw)-1-suf] {
		suf++
	}
	p := tool.Patch{Path: d.Path, OldLines: len(old) - pre - suf, NewLines: len(nw) - pre - suf}
	if p.OldLines > 0 {
		p.OldStart = pre + 1
	} else {
		p.OldStart = pre
	}
	if p.NewLines > 0 {
		p.NewStart = pre + 1
	} else {
		p.NewStart = pre
	}
	for _, l := range old[pre : len(old)-suf] {
		p.Lines = append(p.Lines, "-"+l)
	}
	for _, l := range nw[pre : len(nw)-suf] {
		p.Lines = append(p.Lines, "+"+l)
	}
	return p
}
