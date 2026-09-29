package efficiency

import (
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/settingsfile"
)

// Source is an agent whose transcripts efficiency reads: it lists them,
// and reads each line of one into a File through File's Saw, Request,
// Call, Result and the rest. Efficiency reads Agent's.
type Source interface {
	// Transcripts lists p's transcripts: its sessions' and their
	// subagents', whose paths have a "subagents" folder in them.
	Transcripts(p agent.Profile) []string
	// TranscriptsDir is the folder all of p's transcripts are under.
	TranscriptsDir(p agent.Profile) string
	// ReadLine reads one whole line of a transcript into f.
	ReadLine(f *File, line []byte)
	// Setup is what's set up in p's home, for savers to be found in.
	Setup(p agent.Profile) Setup
}

// Setup is an agent's setup in one home, as far as savers show in it.
type Setup struct {
	Settings *settingsfile.File // its own settings
	Hooks    []string           // every hook's command
	Enabled  map[string]bool    // plugins turned on, by id
	Plugins  map[string]time.Time
	MCP      map[string]bool // MCP servers, by name
	// Backup are the files an install may change, copied first.
	Backup []string
	// Env is the environment its CLI runs in for that home.
	Env []string
}

// source is Agent's Source, when it has one.
func source() (Source, bool) { return agent.As[Source](Agent) }

// Transcripts lists p's transcripts, as Agent's Source finds them.
func Transcripts(p agent.Profile) []string {
	if src, ok := source(); ok {
		return src.Transcripts(p)
	}
	return nil
}

// TranscriptsDir is the folder all of p's transcripts are under, as
// Agent's Source says; "" when it has none.
func TranscriptsDir(p agent.Profile) string {
	if src, ok := source(); ok {
		return src.TranscriptsDir(p)
	}
	return ""
}
