package claude

import (
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
)

var _ agent.Timeliner = Adapter{}

// NewTimeline follows a Claude Code transcript and its subagents'.
func (Adapter) NewTimeline() agent.Timeline { return timeline{&claude.Timeline{}} }

type timeline struct{ *claude.Timeline }

func (t timeline) Events(since time.Time) []agent.Happening { return t.View(since).Events }
