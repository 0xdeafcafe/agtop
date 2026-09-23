// Package headless runs Claude Code as a bare agent loop: `claude -p` speaking
// stream-json on stdin and stdout. Claude Code keeps the model, tools, hooks,
// MCP and compaction; agtop draws the session and answers its permission
// prompts, so no terminal UI, PTY host or daemon runs for it.
package headless

import (
	"encoding/json"
	"strings"
)

// Event is one decoded line of Claude Code's output.
type Event interface{ event() }

// Init arrives once, when the session is ready.
type Init struct {
	SessionID      string
	Model          string
	Cwd            string
	PermissionMode string
	Version        string
	Tools          []string
	SlashCommands  []string
}

// MessageStart opens an assistant message that Deltas then fill in.
type MessageStart struct {
	MessageID string
	Model     string
}

// Delta is a piece of text or thinking streamed while a message is written.
// Index is the content block it belongs to.
type Delta struct {
	Index    int
	Thinking bool
	Input    bool // a tool call's input, streamed as JSON
	Text     string
}

// BlockStart opens a content block: "thinking", "text" or "tool_use".
// Thinking often streams no text at all (only a signature), so this is
// how to know it's happening.
type BlockStart struct {
	Index int
	Type  string
}

// Message is a whole assistant or user turn. Tool results arrive as user
// messages. ParentToolUseID is set when a subagent wrote it.
type Message struct {
	Role            string
	ID              string
	Model           string // assistant messages: the model that wrote it
	UUID            string
	ParentToolUseID string
	Blocks          []Block
	Usage           *Usage
	// ToolResult is Claude Code's structured account of a tool run, sent with
	// the tool_result message: an Edit's structuredPatch, a Read's file, a
	// Bash run's stdout and stderr.
	ToolResult json.RawMessage
}

// Block is one content block of a message.
type Block struct {
	Type      string // text, thinking, tool_use, tool_result
	Text      string // text, thinking, or a tool result flattened to text
	ID        string // tool_use
	Name      string // tool_use
	Input     json.RawMessage
	ToolUseID string // tool_result
	IsError   bool   // tool_result
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// PermissionRequest asks the host whether a tool may run. Answer it with
// Session.Allow or Session.Deny.
type PermissionRequest struct {
	ID          string
	Tool        string
	Input       json.RawMessage
	Description string
	Reason      string
	ReasonType  string
	ToolUseID   string
	BlockedPath string
	Suggestions json.RawMessage
}

// PermissionCancelled withdraws a PermissionRequest, e.g. after an interrupt.
type PermissionCancelled struct{ ID string }

// PermissionDenied reports a tool call refused without asking the host.
type PermissionDenied struct {
	Tool      string
	ToolUseID string
	Reason    string
}

// Status is the session's own busy marker, e.g. "requesting".
type Status struct{ Status string }

// Result ends a turn.
type Result struct {
	Subtype    string // success, error_max_turns, error_during_execution, ...
	IsError    bool
	Text       string
	StopReason string
	SessionID  string
	CostUSD    float64
	DurationMS int
	NumTurns   int
	Usage      Usage
}

// RateLimit carries the account's usage windows.
type RateLimit struct {
	Status string
	Raw    json.RawMessage
}

// ControlReply answers a control request the host sent.
type ControlReply struct {
	ID    string
	Error string
	Body  json.RawMessage
}

// Other is anything not decoded above, kept whole so nothing is lost.
type Other struct {
	Type, Subtype string
	Raw           json.RawMessage
}

func (Init) event()                {}
func (MessageStart) event()        {}
func (Delta) event()               {}
func (BlockStart) event()          {}
func (Message) event()             {}
func (PermissionRequest) event()   {}
func (PermissionCancelled) event() {}
func (PermissionDenied) event()    {}
func (Status) event()              {}
func (Result) event()              {}
func (RateLimit) event()           {}
func (ControlReply) event()        {}
func (Other) event()               {}

type envelope struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	UUID            string          `json:"uuid"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	RequestID       string          `json:"request_id"`
	Request         json.RawMessage `json:"request"`
	Response        json.RawMessage `json:"response"`
	Message         json.RawMessage `json:"message"`
	Event           json.RawMessage `json:"event"`
	ToolUseResult   json.RawMessage `json:"tool_use_result"`
}

// Decode turns one output line into an Event.
func Decode(line []byte) (Event, error) {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, err
	}
	other := Other{Type: e.Type, Subtype: e.Subtype, Raw: append(json.RawMessage(nil), line...)}
	switch e.Type {
	case "system":
		return decodeSystem(e.Subtype, line, other)
	case "stream_event":
		return decodeStream(e.Event, other)
	case "assistant", "user":
		return decodeMessage(e)
	case "control_request":
		return decodeControl(e, other)
	case "control_cancel_request":
		return PermissionCancelled{ID: e.RequestID}, nil
	case "control_response":
		var r struct {
			RequestID string          `json:"request_id"`
			Error     string          `json:"error"`
			Response  json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(e.Response, &r); err != nil {
			return nil, err
		}
		return ControlReply{ID: r.RequestID, Error: r.Error, Body: r.Response}, nil
	case "result":
		var r struct {
			Subtype    string  `json:"subtype"`
			IsError    bool    `json:"is_error"`
			Result     string  `json:"result"`
			StopReason string  `json:"stop_reason"`
			SessionID  string  `json:"session_id"`
			CostUSD    float64 `json:"total_cost_usd"`
			DurationMS int     `json:"duration_ms"`
			NumTurns   int     `json:"num_turns"`
			Usage      Usage   `json:"usage"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		return Result{Subtype: r.Subtype, IsError: r.IsError, Text: r.Result, StopReason: r.StopReason,
			SessionID: r.SessionID, CostUSD: r.CostUSD, DurationMS: r.DurationMS, NumTurns: r.NumTurns, Usage: r.Usage}, nil
	case "rate_limit_event":
		var r struct {
			Info json.RawMessage `json:"rate_limit_info"`
		}
		_ = json.Unmarshal(line, &r)
		var s struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(r.Info, &s)
		return RateLimit{Status: s.Status, Raw: r.Info}, nil
	}
	return other, nil
}

func decodeSystem(subtype string, line []byte, other Other) (Event, error) {
	switch subtype {
	case "init":
		var r struct {
			SessionID      string   `json:"session_id"`
			Model          string   `json:"model"`
			Cwd            string   `json:"cwd"`
			PermissionMode string   `json:"permissionMode"`
			Version        string   `json:"claude_code_version"`
			Tools          []string `json:"tools"`
			SlashCommands  []string `json:"slash_commands"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		return Init(r), nil
	case "status":
		var r struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(line, &r)
		return Status(r), nil
	case "permission_denied":
		var r struct {
			Tool      string `json:"tool_name"`
			ToolUseID string `json:"tool_use_id"`
			Reason    string `json:"decision_reason"`
		}
		_ = json.Unmarshal(line, &r)
		return PermissionDenied(r), nil
	}
	return other, nil
}

func decodeStream(raw json.RawMessage, other Other) (Event, error) {
	var ev struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"message"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	switch ev.Type {
	case "message_start":
		return MessageStart{MessageID: ev.Message.ID, Model: ev.Message.Model}, nil
	case "content_block_start":
		return BlockStart{Index: ev.Index, Type: ev.ContentBlock.Type}, nil
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			return Delta{Index: ev.Index, Text: ev.Delta.Text}, nil
		case "thinking_delta":
			return Delta{Index: ev.Index, Thinking: true, Text: ev.Delta.Thinking}, nil
		case "input_json_delta":
			// A tool call being written: counted, not shown.
			return Delta{Index: ev.Index, Input: true, Text: ev.Delta.PartialJSON}, nil
		}
	}
	return other, nil
}

func decodeMessage(e envelope) (Event, error) {
	var m struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *Usage          `json:"usage"`
	}
	if err := json.Unmarshal(e.Message, &m); err != nil {
		return nil, err
	}
	out := Message{Role: m.Role, ID: m.ID, Model: m.Model, UUID: e.UUID, ParentToolUseID: e.ParentToolUseID, Usage: m.Usage, ToolResult: e.ToolUseResult}
	if out.Role == "" {
		out.Role = e.Type
	}
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		out.Blocks = []Block{{Type: "text", Text: text}}
		return out, nil
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil, err
	}
	for _, b := range blocks {
		blk := Block{Type: b.Type, Text: b.Text, ID: b.ID, Name: b.Name, Input: b.Input, ToolUseID: b.ToolUseID, IsError: b.IsError}
		switch b.Type {
		case "thinking":
			blk.Text = b.Thinking
		case "tool_result":
			blk.Text = flatten(b.Content)
		}
		out.Blocks = append(out.Blocks, blk)
	}
	return out, nil
}

// flatten reads a tool result's content, a string or a list of blocks, as text.
func flatten(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, p.Text)
		case "image":
			out = append(out, "[image]")
		}
	}
	return strings.Join(out, "\n")
}

func decodeControl(e envelope, other Other) (Event, error) {
	var r struct {
		Subtype     string          `json:"subtype"`
		Tool        string          `json:"tool_name"`
		Input       json.RawMessage `json:"input"`
		Description string          `json:"description"`
		Reason      string          `json:"decision_reason"`
		ReasonType  string          `json:"decision_reason_type"`
		ToolUseID   string          `json:"tool_use_id"`
		BlockedPath string          `json:"blocked_path"`
		Suggestions json.RawMessage `json:"permission_suggestions"`
	}
	if err := json.Unmarshal(e.Request, &r); err != nil {
		return nil, err
	}
	if r.Subtype != "can_use_tool" {
		return other, nil
	}
	return PermissionRequest{ID: e.RequestID, Tool: r.Tool, Input: r.Input, Description: r.Description,
		Reason: r.Reason, ReasonType: r.ReasonType, ToolUseID: r.ToolUseID, BlockedPath: r.BlockedPath,
		Suggestions: r.Suggestions}, nil
}

// Command is a slash command the session accepts.
type Command struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases"`
}

// Commands reads the slash commands out of the reply to Initialize.
func Commands(reply ControlReply) []Command {
	var r struct {
		Commands []Command `json:"commands"`
	}
	_ = json.Unmarshal(reply.Body, &r)
	return r.Commands
}

// Patch is one hunk of an edit, as Claude Code reports it in an Edit or
// Write result's structuredPatch.
type Patch struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"` // each prefixed ' ', '-' or '+'
}

// Patches reads the hunks from a tool result, if it has any.
func Patches(toolResult json.RawMessage) []Patch {
	var r struct {
		StructuredPatch []Patch `json:"structuredPatch"`
	}
	_ = json.Unmarshal(toolResult, &r)
	return r.StructuredPatch
}
