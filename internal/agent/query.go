package agent

// Querier is an agent that answers one prompt headless, read-only and in a
// JSON shape, keeping no session: what to run for q, and how to read what
// it printed. The caller runs the program (Path) and owns its process.
type Querier interface {
	QueryCommand(p Profile, q Query) (args, env []string)
	// QueryAnswer reads what it printed; ok is false when that can't be
	// read at all, so what it cost isn't known.
	QueryAnswer(stdout []byte) (a Answer, ok bool)
	// QueryModels are its quick, cheap model and its careful one.
	QueryModels() (quick, careful string)
}

// Query is one question for a Querier.
type Query struct {
	Model  string
	System string
	Prompt string // given on its stdin
	Schema string // the JSON schema its answer must fit
	// Dirs are the folders it may read, and nothing else.
	Dirs   []string
	Budget float64 // the most it may spend, in dollars
}

// Answer is what a Querier gave back.
type Answer struct {
	Out  []byte  // the answer, in Query.Schema's shape
	Cost float64 // what it cost, in dollars
	Err  error   // why it failed, when it did
}
