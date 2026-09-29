package usage

import "time"

// Context is what fills a session's context window, part by part, as its
// agent counts it. Its JSON is what clients have always read: the names
// are Claude Code's /context answer's, the first agent that gave one.
type Context struct {
	Parts   []ContextPart `json:"categories"`
	Total   int           `json:"totalTokens"`
	Max     int           `json:"maxTokens"`
	Percent float64       `json:"percentage"`
	Model   string        `json:"model"`
	// AutoCompactAt is the fill at which the agent summarises the
	// conversation, when AutoCompact is on.
	AutoCompactAt int              `json:"autoCompactThreshold"`
	AutoCompact   bool             `json:"isAutoCompactEnabled"`
	MemoryFiles   []ContextFile    `json:"memoryFiles"`
	MCPTools      []ContextMCPTool `json:"mcpTools"`
	Agents        []ContextAgent   `json:"agents"`
	Skills        ContextSkills    `json:"skills"`
	Messages      ContextMessages  `json:"messageBreakdown"`
	At            time.Time        `json:"at"` // when it was counted
}

// ContextPart is one part of the window. Kind is used, deferred (tools
// loaded on demand, not in the window), buffer (kept free for
// auto-compact) or free.
type ContextPart struct {
	Name     string `json:"name"`
	Tokens   int    `json:"tokens"`
	Kind     string `json:"kind"`
	Deferred bool   `json:"isDeferred"`
}

// ContextFile is a memory file the agent loaded.
type ContextFile struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Tokens int    `json:"tokens"`
}

// ContextMCPTool is one MCP server's tool.
type ContextMCPTool struct {
	Name     string `json:"name"`
	Server   string `json:"serverName"`
	Tokens   int    `json:"tokens"`
	IsLoaded bool   `json:"isLoaded"`
}

// ContextAgent is a subagent the model is told it can start.
type ContextAgent struct {
	Type   string `json:"agentType"`
	Source string `json:"source"`
	Tokens int    `json:"tokens"`
}

// ContextSkills are the skills the model is told of.
type ContextSkills struct {
	Total    int            `json:"totalSkills"`
	Included int            `json:"includedSkills"`
	Tokens   int            `json:"tokens"`
	Each     []ContextSkill `json:"skillFrontmatter"`
}

// ContextSkill is one of them.
type ContextSkill struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Tokens int    `json:"tokens"`
}

// ContextMessages is the conversation's share, by what it is.
type ContextMessages struct {
	ToolCalls   int               `json:"toolCallTokens"`
	ToolResults int               `json:"toolResultTokens"`
	Attachments int               `json:"attachmentTokens"`
	Assistant   int               `json:"assistantMessageTokens"`
	User        int               `json:"userMessageTokens"`
	ByTool      []ContextToolUses `json:"toolCallsByType"`
}

// ContextToolUses is one tool's calls and results.
type ContextToolUses struct {
	Name    string `json:"name"`
	Calls   int    `json:"callTokens"`
	Results int    `json:"resultTokens"`
}
