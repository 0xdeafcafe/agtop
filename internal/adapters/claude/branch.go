package claude

import (
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
)

// TranscriptPath is the session's file in p's projects folder.
func (Adapter) TranscriptPath(p agent.Profile, cwd, sid string) string {
	return claude.AccountOf(p).TranscriptPath(cwd, sid)
}

// Branch copies the transcript with the new session's id in it, then its
// file checkpoints, so the copy can rewind; without them it still runs.
func (Adapter) Branch(p agent.Profile, src, cwd, sid, newID string, upTo int64) error {
	acct := claude.AccountOf(p)
	if err := claude.CopyTranscript(src, acct.TranscriptPath(cwd, newID), sid, newID, upTo); err != nil {
		return err
	}
	_ = acct.CopyCheckpoints(sid, newID)
	return nil
}

// DefaultModel is the Opus a session runs when none is picked.
func (Adapter) DefaultModel() string { return "claude-opus-5-5" }

// ModelName is Claude's model id as people say it: "Opus 5.5".
func (Adapter) ModelName(id string) string { return claude.ModelName(id) }

// Stats is the profile's stats-cache.json, what Claude Code's /stats shows.
func (Adapter) Stats(p agent.Profile) (agent.Stats, error) {
	return claude.LoadStats(claude.AccountOf(p))
}
