package headless

import (
	"encoding/json"
	"errors"
	"time"
)

// ContextUsage is what fills the context window, by category, as Claude
// Code's /context counts it (the get_context_usage control request).
type ContextUsage struct {
	Categories []ContextCategory `json:"categories"`
	Total      int               `json:"totalTokens"`
	Max        int               `json:"maxTokens"`
	Percentage float64           `json:"percentage"`
	Model      string            `json:"model"`
	// AutoCompactAt is the fill at which Claude Code summarises the
	// conversation, when AutoCompact is on.
	AutoCompactAt int  `json:"autoCompactThreshold"`
	AutoCompact   bool `json:"isAutoCompactEnabled"`
	MemoryFiles   []struct {
		Path   string `json:"path"`
		Type   string `json:"type"`
		Tokens int    `json:"tokens"`
	} `json:"memoryFiles"`
	MCPTools []struct {
		Name     string `json:"name"`
		Server   string `json:"serverName"`
		Tokens   int    `json:"tokens"`
		IsLoaded bool   `json:"isLoaded"`
	} `json:"mcpTools"`
	Agents []struct {
		Type   string `json:"agentType"`
		Source string `json:"source"`
		Tokens int    `json:"tokens"`
	} `json:"agents"`
	Skills struct {
		Total    int `json:"totalSkills"`
		Included int `json:"includedSkills"`
		Tokens   int `json:"tokens"`
		Each     []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
			Tokens int    `json:"tokens"`
		} `json:"skillFrontmatter"`
	} `json:"skills"`
	Messages struct {
		ToolCalls   int `json:"toolCallTokens"`
		ToolResults int `json:"toolResultTokens"`
		Attachments int `json:"attachmentTokens"`
		Assistant   int `json:"assistantMessageTokens"`
		User        int `json:"userMessageTokens"`
		ByTool      []struct {
			Name    string `json:"name"`
			Calls   int    `json:"callTokens"`
			Results int    `json:"resultTokens"`
		} `json:"toolCallsByType"`
	} `json:"messageBreakdown"`
	// At is when it was counted (set by agtop, not Claude Code).
	At time.Time `json:"at"`
}

// ContextCategory is one part of the window: kind is used, deferred (tools
// loaded on demand, not in the window), buffer (kept free for
// auto-compact) or free.
type ContextCategory struct {
	Name     string `json:"name"`
	Tokens   int    `json:"tokens"`
	Kind     string `json:"kind"`
	Deferred bool   `json:"isDeferred"`
}

// AskContextUsage asks Claude Code what fills the context window; the
// answer is a ControlReply for the returned id (read it with
// ParseContextUsage). It counts from the last response and local estimates,
// so it costs nothing.
func (s *Session) AskContextUsage() (string, error) {
	return s.control(map[string]any{"subtype": "get_context_usage", "detail": "summary"})
}

// ParseContextUsage reads a reply to AskContextUsage.
func ParseContextUsage(reply ControlReply) (ContextUsage, error) {
	var u ContextUsage
	err := json.Unmarshal(reply.Body, &u)
	return u, err
}

// Ask sends any control request (req carries its subtype) and returns its
// id; the answer is a ControlReply for it.
func (s *Session) Ask(req json.RawMessage) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(req, &m); err != nil {
		return "", err
	}
	if _, ok := m["subtype"].(string); !ok {
		return "", errors.New("a control request needs a subtype")
	}
	return s.control(m)
}
