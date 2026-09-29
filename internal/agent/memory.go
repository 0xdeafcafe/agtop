package agent

import "time"

// Memory is what an agent reads for a session, beyond the conversation:
// its instruction files (CLAUDE.md, AGENTS.md), its own memory, settings
// and the like, and what's untidy about them.
type Memory struct {
	// Files are grouped, in Groups' order.
	Files  []MemoryFile
	Groups []string
	// When is when each group's files reach the model.
	When     map[string]string
	Problems []MemoryProblem
}

// MemoryFile is one file an agent reads, or one it would read if it
// existed.
type MemoryFile struct {
	Group   string
	Path    string
	Name    string
	About   string
	Kind    string // a memory note's type: user, feedback, project, reference
	Missing bool
	Size    int64
	Mod     time.Time
	// Up is the tokens it costs every session, roughly; Later, the tokens
	// it costs when it's loaded, which When says.
	Up    int64
	Later int64
	When  string
	Warn  string // what's untidy about it
	Sub   bool   // imported by the file above it
}

// MemoryProblem is something untidy, and what to do about it.
type MemoryProblem struct {
	Kind  string
	Title string
	Fix   string
	Path  string   // the file to open for it
	Files []string // the files it's about
}

// MemoryReader is an agent that says what it reads for a session of p's
// in cwd, whose transcript (if it has one yet) is at transcript.
type MemoryReader interface {
	Memory(p Profile, cwd, transcript string) Memory
	// ForgetFile deletes one of its files, and whatever points to it.
	ForgetFile(path string) error
}
