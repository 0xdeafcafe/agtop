package claude

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
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
	return acct.CopyCheckpoints(sid, newID)
}

// DefaultModel is the Opus a session runs when none is picked.
func (Adapter) DefaultModel() string { return "claude-opus-5-5" }

// ContextWindow is the model's: everything current but Haiku has 1M.
func (Adapter) ContextWindow(model string) int64 { return claude.ContextWindow(model) }

// Reads is what every current Claude model reads: images and PDFs, not
// audio or video.
func (Adapter) Reads(string) (agent.Media, bool) { return agent.MediaImage | agent.MediaPDF, true }

// ModelName is Claude's model id as people say it: "Opus 5.5".
func (Adapter) ModelName(id string) string { return claude.ModelName(id) }

// Stats is the profile's stats-cache.json, what Claude Code's /stats shows.
func (Adapter) Stats(p agent.Profile) (agent.Stats, error) {
	return claude.LoadStats(claude.AccountOf(p))
}

// Preview reads what a session is doing from its transcript's tail.
func (Adapter) Preview(path string, window int64) agent.Preview { return claude.ReadPreview(path, window) }
