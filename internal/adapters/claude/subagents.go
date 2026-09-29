package claude

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
)

// SubagentRuns follows a session's subagent runs from its transcript and
// theirs.
func (Adapter) SubagentRuns() agent.SubagentRuns { return runs{&claude.SubagentRuns{}} }

type runs struct{ *claude.SubagentRuns }

func (r runs) SetGone(gone bool) { r.SubagentRuns.Gone = gone }
func (r runs) Gone() bool        { return r.SubagentRuns.Gone }

func (r runs) Clone() agent.SubagentRuns {
	c := r.SubagentRuns.Clone()
	return runs{&c}
}

var _ agent.RunFollower = Adapter{}
