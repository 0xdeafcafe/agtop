// Package agtools is agtop's own MCP server, run inside an agtop session's
// host rather than as a process: Claude Code names it in initialize and sends
// every MCP message for it over the session's control channel. Its tools are
// things only agtop can draw, such as a drawing shown in its own frame.
package agtools

import (
	"encoding/json"
	"strings"
)

// Server is the MCP server's name; Claude Code calls its tools mcp__agtop__*.
const Server = "agtop"

// Prefix starts the name Claude Code gives each of these tools.
const Prefix = "mcp__" + Server + "__"

// Show is the show tool as Claude Code names it.
const Show = Prefix + "show"

// ShowInput is what Claude sends to show.
type ShowInput struct {
	Title   string `json:"title"`
	Drawing string `json:"drawing"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

var tools = []tool{{
	Name: "show",
	Description: "Show the user a drawing: an ASCII or box-drawing diagram, a flow or architecture sketch, a layout mockup, " +
		"a tree, or any text whose exact layout matters. agtop draws it in a frame of its own, full width, never wrapped " +
		"or folded away, and the user can copy it whole. Use it instead of a code block whenever the picture is the point. " +
		"The drawing is shown as-is: plain text, no markdown, no fences.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":   map[string]any{"type": "string", "description": "A few words naming what the drawing shows."},
			"drawing": map[string]any{"type": "string", "description": "The drawing, exactly as it should appear, lines separated by \\n."},
		},
		"required":             []string{"drawing"},
		"additionalProperties": false,
	},
	// Loaded with the built-in tools, not deferred behind tool search, so
	// Claude reaches for it without first looking it up.
	Meta: map[string]any{"anthropic/alwaysLoad": true},
}}

// Allowed is what to pass --allowedTools so the tools run without asking:
// they only draw.
func Allowed() []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = Prefix + t.Name
	}
	return out
}

// Handle answers one JSON-RPC message from Claude Code's MCP client. A
// notification gets an empty result, which is what Claude Code expects back.
func Handle(msg json.RawMessage) json.RawMessage {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		return reply(nil, nil, &rpcError{Code: -32700, Message: "parse error"})
	}
	if len(m.ID) == 0 {
		return reply(nil, map[string]any{}, nil)
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return reply(m.ID, map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": Server, "version": "1"},
		}, nil)
	case "tools/list":
		return reply(m.ID, map[string]any{"tools": tools}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return reply(m.ID, call(p.Name, p.Arguments), nil)
	case "ping":
		return reply(m.ID, map[string]any{}, nil)
	}
	return reply(m.ID, nil, &rpcError{Code: -32601, Message: "method not found: " + m.Method})
}

// call runs a tool. The drawing itself is the call's input, which the
// transcript keeps, so showing it needs nothing more than a yes.
func call(name string, args json.RawMessage) map[string]any {
	switch name {
	case "show":
		var in ShowInput
		_ = json.Unmarshal(args, &in)
		if strings.TrimSpace(in.Drawing) == "" {
			return result("Nothing to show: the drawing is empty.", true)
		}
		return result("Shown to the user.", false)
	}
	return result("agtop has no tool named "+name+".", true)
}

func result(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func reply(id json.RawMessage, res any, e *rpcError) json.RawMessage {
	out := map[string]any{"jsonrpc": "2.0"}
	if id != nil {
		out["id"] = id
	}
	if e != nil {
		out["error"] = e
	} else {
		out["result"] = res
	}
	b, _ := json.Marshal(out)
	return b
}
