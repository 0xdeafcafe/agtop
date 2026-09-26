// Package tool is a tool call as agtop draws it, whichever agent made it.
// Each adapter reads its own tool names and input shapes into a Call once,
// so the renderer switches on Kind and reads Input's fields, not on
// "Bash" or "shell" or "execute".
package tool

import "encoding/json"

// Kind is what a tool call does.
type Kind int

const (
	Other     Kind = iota // anything else: drawn by its Name
	Shell                 // runs a command
	Read                  // reads a file
	Edit                  // changes part of a file
	Write                 // writes a whole file
	Delete                // deletes a file
	Move                  // moves or renames a file
	Search                // searches file contents (grep)
	Glob                  // finds files by name
	Fetch                 // reads a web page
	WebSearch             // searches the web
	Subagent              // hands work to a subagent
	Todo                  // changes the agent's todo list or plan
	Question              // asks the user something, with choices
	PlanMode              // leaves or enters plan mode with a plan
	MCP                   // an MCP server's tool
	Notebook              // edits a notebook cell
	Think                 // thinks aloud, runs nothing
)

var kindNames = [...]string{"other", "shell", "read", "edit", "write", "delete", "move", "search", "glob", "fetch", "websearch", "subagent", "todo", "question", "planmode", "mcp", "notebook", "think"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "other"
}

// Changes is whether a call of this kind changes files.
func (k Kind) Changes() bool {
	switch k {
	case Edit, Write, Delete, Move, Notebook:
		return true
	}
	return false
}

// Call is one tool call.
type Call struct {
	ID    string
	Name  string // the agent's own name for the tool: "Bash", "shell", "mcp__linear__get_issue"
	Kind  Kind
	Title string // what the agent says the call is, when it says: ACP's title
	Input Input
	Raw   json.RawMessage // the input as the agent sent it
}

// Input is what a call works on, as far as agtop reads it. An adapter
// fills in the fields its tool has, and leaves the rest empty.
type Input struct {
	Command     string     // Shell
	Cwd         string     // Shell, when it isn't the session's
	Background  bool       // Shell: started to run beside the turn
	Timeout     int        // Shell, milliseconds
	Path        string     // Read, Edit, Write, Delete, Notebook; Search and Glob: where
	To          string     // Move
	Pattern     string     // Search, Glob
	Query       string     // WebSearch
	URL         string     // Fetch
	Description string     // what the call is for, in the agent's words
	Prompt      string     // Subagent; Fetch: what to look for
	Agent       string     // Subagent: its type
	Content     string     // Write: the whole file
	Edits       []Replace  // Edit
	Offset      int        // Read: first line, 1-based; 0 is the start
	Limit       int        // Read: how many lines; 0 is all
	Server      string     // MCP
	Tool        string     // MCP
	Todos       []TodoItem // Todo
}

// Replace is one replacement in a file.
type Replace struct {
	Path     string // when a call edits several files; else Input.Path
	Old, New string
	All      bool // every occurrence
}

// TodoItem is one item of a todo list or plan as a Todo call sets it.
type TodoItem struct {
	Label  string // what it is
	Active string // what it says while it's being done: "Running the tests"
	Status string // pending, in_progress, completed
}

// Output is what a call returned.
type Output struct {
	CallID  string
	Text    string // flattened to text, as the agent would read it
	IsError bool
	Stdout  string // Shell, when the agent keeps the streams apart
	Stderr  string
	Exit    *int    // Shell, when known
	Patches []Patch // Edit and Write: what changed
	Lines   *Span   // Read: which lines it read
	Created bool    // Write: the file didn't exist before
	Raw     json.RawMessage
}

// Patch is one hunk of a change to a file.
type Patch struct {
	Path     string   `json:"path,omitempty"`
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"` // each prefixed ' ', '-' or '+'
}

// Span is a run of lines out of a file's Total.
type Span struct{ Start, Count, Total int }
