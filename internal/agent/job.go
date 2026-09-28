package agent

import "time"

// Job is a session as a list row shows it: what it's called, what it's
// doing and what it's waiting on, whichever agent runs it. The agent's own
// record of it (Claude Code's job file) is the adapter's.
type Job struct {
	ID             string
	Account        string
	Name           string
	NameSource     string
	State          string // working, blocked, done, stopped
	Detail         string
	Tempo          string
	Needs          string
	Intent         string
	Cwd            string
	SessionID      string
	TranscriptPath string
	WorktreePath   string
	WorktreeBranch string
	Children       int
	InFlight       int      // background tasks running or queued
	Background     []string // what they are: shell commands, subagent names
	Subagents      int      // subagents still running
	Running        []Task   // subagents, shells and monitors not yet finished
	TodosDone      int
	Todos          int
	TodoItems      []Todo
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ModTime        time.Time
}

// Live is a session working, or waiting on you.
func (j Job) Live() bool { return j.State == "working" || j.State == "blocked" }

// Busy is a finished turn whose background work is still running.
func (j Job) Busy() bool { return !j.Live() && j.InFlight > 0 && len(j.Background) > 0 }

// Open is true for anything with a live process, including idle terminals.
func (j Job) Open() bool { return j.Live() || j.State == "idle" }
