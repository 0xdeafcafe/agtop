package agent

// Rewinder is an agent that can take a conversation back: put back the
// files it changed, and first say what it learned. Copying the
// conversation to go back to is Brancher's.
type Rewinder interface {
	// RewindFiles puts the files session sid (p's, in cwd, on model)
	// changed back as they were just before its message msgID; dryRun only
	// says what that would change.
	RewindFiles(p Profile, cwd, sid, model, msgID string, dryRun bool) (FileRewind, error)
	// Recap asks session sid what it learned since its message from.
	Recap(p Profile, cwd, sid, model, from string) (string, error)
}

// FileRewind is what putting a conversation's files back did, or would do.
type FileRewind struct {
	CanRewind  bool
	Error      string // why it can't
	Files      []string
	Insertions int
	Deletions  int
}
