package agent

import (
	"path/filepath"
	"time"
)

// LegacyKind is the agent an empty kind meant: rush ran only Claude Code
// before it ran others, and wrote no kind for it.
const LegacyKind Kind = "claude" // migration: what older state meant by no kind

// Migrated is a kind read from state, a host's info or config an older
// rush wrote, as it's kept now: an empty one is LegacyKind. Call it only
// where such a kind is read in; nothing past there sees an empty kind.
func Migrated(kind string) Kind {
	if kind == "" {
		return LegacyKind // migration: older rushes wrote no kind for Claude Code
	}
	return Kind(kind)
}

// ClaudeTranscripts is an adapter whose sessions write Claude Code's
// transcripts, which the conversation pane reads as they grow and which
// sit where a Claude Code account keeps them. Another agent's are read
// through its HistoryReader.
type ClaudeTranscripts interface {
	ClaudeTranscripts()
}

// ReadsAsClaude is whether agent k's transcripts are Claude Code's.
func ReadsAsClaude(k Kind) bool {
	a, ok := Get(k)
	if !ok {
		return false
	}
	_, ok = a.(ClaudeTranscripts)
	return ok
}

// SpawnFinder is an adapter that finds a session one of its programs
// began from another's shell itself, faster than listing every session:
// one begun in dir after start that fits, looked for in profiles.
type SpawnFinder interface {
	FindSpawn(profiles []Profile, dir string, start time.Time, fits func(Session) bool) (Session, bool)
}

// ProgramOf is what agent k's program is called; empty when it has none,
// isn't registered, or rides another's.
func ProgramOf(k Kind) string {
	a, ok := Get(k)
	if !ok {
		return ""
	}
	if _, ok := a.(Rider); ok {
		return ""
	}
	p, ok := a.(Programmer)
	if !ok {
		return ""
	}
	name, _ := p.Program()
	return name
}

// IsProgram is whether a process called comm (a name, or a path) is one
// of the registered agents' programs.
func IsProgram(comm string) bool {
	if comm = filepath.Base(comm); comm == "" || comm == "." || comm == "/" {
		return false
	}
	for _, a := range All() {
		if ProgramOf(a.Kind()) == comm {
			return true
		}
	}
	return false
}

// Spawnable is an adapter whose program a session's shell can run as a
// run rush hosts (see host's stand-ins): how the run is written, with a
// "<task>" for its prompt, and the flag that picks its model.
type Spawnable interface {
	SpawnCommand() (cmd, modelFlag string)
}
