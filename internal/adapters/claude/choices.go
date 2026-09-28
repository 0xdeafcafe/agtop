package claude

import "github.com/0xdeafcafe/agtop/internal/agent"

// Choices are Claude Code's --model, --effort and --permission-mode.
func (Adapter) Choices() agent.Choices {
	return agent.Choices{
		Models: []agent.Choice{
			{ID: "opus", Note: "the most capable Opus, for hard, long-running work."},
			{ID: "opus[1m]", Note: "Opus with the 1M-token context window, for very large tasks."},
			{ID: "sonnet", Note: "faster and cheaper, good for routine work."},
			{ID: "haiku", Note: "fastest and cheapest, for small tasks."},
			{ID: "fable", Note: "Anthropic's most capable model, at a higher price."},
		},
		Efforts: []agent.Choice{
			{ID: "low", Note: "quick and cheap; fine for simple, well-specified tasks."},
			{ID: "medium", Note: "a balance of speed and care."},
			{ID: "high", Note: "careful; the usual choice for real engineering work."},
			{ID: "xhigh", Note: "very careful; for tricky, long-horizon tasks."},
			{ID: "max", Note: "as thorough as possible, whatever it costs."},
		},
		Modes: []agent.Choice{
			{ID: "default", Note: "asks before edits and commands it isn't sure about."},
			{ID: "acceptEdits", Note: "edits files without asking; still asks before other commands."},
			{ID: "plan", Note: "plans first and changes nothing until you approve."},
			{ID: "auto", Note: "a classifier approves safe actions and asks about risky ones."},
			{ID: "bypassPermissions", Note: "never asks. Only for sandboxed or throwaway work."},
		},
	}
}
