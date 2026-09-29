package agent

// What an agent keeps about its sessions beyond each one's own record,
// for the list's rows.

// JobKeeper is an agent whose background jobs a service of its own runs
// (Claude Code's daemon), and that keeps more of them than each job's own
// record. Each is read again only when its file changes.
type JobKeeper interface {
	// Workers are the processes running p's jobs, by job id.
	Workers(p Profile) map[string]int
	// Pins are the jobs pinned in the agent's own list, by job id, each
	// its place from 1.
	Pins(p Profile) map[string]int
	// LinkedPRs are the pull requests the agent linked to p's sessions,
	// by URL.
	LinkedPRs(p Profile) map[string]PR
	// Watched are the files and folders p's sessions are found in, and
	// those of each of jobs.
	Watched(p Profile, jobs []string) []string
}

// Transcripts is an agent that keeps each session's conversation in a
// file under a folder of the profile's.
type Transcripts interface {
	// FindTranscript is where session sid, started in cwd, keeps its
	// conversation now: entering a worktree can move it.
	FindTranscript(p Profile, cwd, sid string) string
	// TranscriptsDir is the folder they're all under.
	TranscriptsDir(p Profile) string
}

// TempDir is a folder of an agent's scratch work, and whether cleaning it
// empties it (a folder the agent expects to find) or removes it.
type TempDir struct {
	Path string
	Keep bool
}

// Scratcher is an agent that leaves scratch work of its own behind: what
// its sessions cloned, built or downloaded.
type Scratcher interface {
	// Scratch are session sid's folders, for one run in cwd, and job's
	// own when it runs as the background job with that id.
	Scratch(p Profile, job, sid, cwd string) []TempDir
	// ScratchRoot is the folder a session's scratch folder is two below.
	ScratchRoot() string
}
