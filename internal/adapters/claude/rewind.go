package claude

import (
	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/agent"
)

var _ agent.Rewinder = Adapter{}

func rewindOpts(p agent.Profile, cwd, sid, model string) headless.Options {
	return headless.Options{Account: Account(p), Dir: cwd, Resume: sid, Model: model}
}

// RewindFiles is Claude Code's own rewind of a conversation's files.
func (Adapter) RewindFiles(p agent.Profile, cwd, sid, model, msgID string, dryRun bool) (agent.FileRewind, error) {
	r, err := headless.RewindFiles(rewindOpts(p, cwd, sid, model), msgID, dryRun)
	return agent.FileRewind(r), err
}

// Recap asks a fork of the conversation what it learned since from.
func (Adapter) Recap(p agent.Profile, cwd, sid, model, from string) (string, error) {
	return headless.Recap(rewindOpts(p, cwd, sid, model), from)
}
