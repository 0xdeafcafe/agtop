package agent

import "time"

// RunState is how a session's transcripts say a subagent run stands.
type RunState int

const (
	RunUnknown RunState = iota // no word of its call: judge by its writing
	RunRunning
	RunDone
)

// SubagentRun is one subagent run a session started, as its transcripts
// say.
type SubagentRun struct {
	ID, ToolUseID string
	Depth         int       // 1 for one the session started, more for one a run started
	Mod           time.Time // when its transcript was last written
	// Type and Description are the run's own words: which subagent it is,
	// and what it was asked.
	Type, Description string
}

// SubagentRuns follows a session's transcript, and its subagent runs'
// own, reading only what each has gained since.
type SubagentRuns interface {
	// SetGone says the session's own process is known to have exited: a
	// run the transcripts left unfinished ended with it. Gone says so.
	SetGone(gone bool)
	Gone() bool
	// Update reads what the session's transcript at path, and its runs',
	// have gained.
	Update(path string) []SubagentRun
	// Stats counts the session's runs, and those still working.
	Stats(path string, now time.Time) SubagentStats
	// Running are the runs Stats last found still working.
	Running() []SubagentRun
	// State is how the transcripts say run id, of call toolUseID, stands:
	// how it ended, and when, once it has.
	State(id, toolUseID string) (st RunState, status string, at time.Time)
	// Going is whether run id is still working, last written at mod, and
	// how it ended if it has.
	Going(id, toolUseID string, mod, now time.Time) (bool, string)
	// Clone is a copy of what's been read, to read apart from it.
	Clone() SubagentRuns
}

// RunFollower is an agent whose sessions' subagent runs rush follows
// from their transcripts.
type RunFollower interface {
	SubagentRuns() SubagentRuns
}

// FollowRuns is a new follower of a session of agent k's subagent runs;
// false when k can't follow them.
func FollowRuns(k Kind) (SubagentRuns, bool) {
	f, ok := As[RunFollower](k)
	if !ok {
		return nil, false
	}
	return f.SubagentRuns(), true
}
