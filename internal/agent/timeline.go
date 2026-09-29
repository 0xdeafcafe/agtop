package agent

import "time"

// Timeliner is an agent whose sessions rush can follow as a timeline: what
// a session and its subagents have done, in time order.
type Timeliner interface {
	NewTimeline() Timeline
}

// Timeline follows one session's transcript, reading only what it gains.
type Timeline interface {
	// Update reads what the transcript at path, and its subagents', have
	// gained; subagents last written before since aren't read.
	Update(path string, since time.Time)
	// Events are those since since, oldest first.
	Events(since time.Time) []Happening
}

type EventKind int

const (
	EvPrompt  EventKind = iota // you wrote to it
	EvTurn                     // a turn ended, with what it said
	EvPlan                     // tasks planned; N of them
	EvTick                     // a task ticked off
	EvStart                    // a subagent started
	EvEnd                      // a subagent, or a background command, ended: Status says how
	EvAsk                      // it asked you something
	EvCommit                   // a commit
	EvPR                       // a PR opened
	EvError                    // a turn died on an API error
	EvCompact                  // its context was compacted
)

type Happening struct {
	At     time.Time
	Kind   EventKind
	Run    string // the subagent it's from, by agent id; "" is the session
	Status string // for EvEnd: completed, failed or stopped
	Text   string
	N      int // for EvPlan
}
