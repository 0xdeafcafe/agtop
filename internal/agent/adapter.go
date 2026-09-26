package agent

import (
	"context"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// Kind names an agent: "claude", "codex", "copilot", "kimi".
type Kind string

// Profile is one config home of one agent: where its settings,
// transcripts and sessions live.
type Profile struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// Account is who pays for a session and whose limits it spends: a sign-in
// with a plan, or an API key. Any number of profiles and agents can use
// one.
type Account struct {
	Kind  Kind   // the adapter that knows how to sign it in
	Key   string // unique across adapters, and where its Quota is kept: "claude:login:<uuid>"
	Name  string
	Email string
	Org   string
	Plan  string
}

// Caps is what an agent can do beyond talking and running tools. The
// interface hides what an agent can't do rather than faking it.
type Caps uint64

const (
	CapResume Caps = 1 << iota
	CapFork
	CapRewind
	CapImages
	CapEffort
	CapModes // permission modes, such as plan and accept-edits
	CapSubagents
	CapBackground // background tasks it can list and stop
	CapQuestions  // structured questions with choices
	CapContext    // a breakdown of what fills the context
	CapCompact
	CapMCP
	CapHooks
	CapPlugins
	CapStatusLine // runs agtop as its statusline
	CapScreen     // has its own TUI to show beside agtop's
)

// Has is whether c has every capability in want.
func (c Caps) Has(want Caps) bool { return c&want == want }

// Adapter is one agent as agtop knows it. The optional parts below are
// found with a type assertion; an adapter has only those it can do.
type Adapter interface {
	Kind() Kind
	Name() string // "Claude Code"
	Caps() Caps
	// Profiles are its config homes on this machine.
	Profiles() []Profile
}

// Discoverer finds an agent's sessions outside agtop: those running in a
// terminal or its own daemon, and past ones with a transcript.
type Discoverer interface {
	Live(p Profile) []Session
	Past(p Profile) []Session
}

// Driver runs a session headless, for agtop to draw.
type Driver interface {
	Start(ctx context.Context, o StartOptions) (Conn, error)
}

// StartOptions is how a Driver starts or resumes a session.
type StartOptions struct {
	Profile   Profile
	Dir       string
	SessionID string // resumed when Resume is set, else the new session's id if the agent takes one
	Resume    bool
	Fork      bool
	Model     string
	Effort    string
	Mode      string
	Env       []string
	Flags     []string // passed to the agent as they are
	Binary    string   // the agent's program, when it isn't on PATH by its usual name
}

// Conn is a running session.
type Conn interface {
	Events() <-chan event.Event
	Send(Input) error
	Answer(approvalID, optionID string) error
	Interrupt() error
	SetModel(model string) error
	SetMode(mode string) error
	Close() error
}

// Answerer is a Conn that takes answers to an event.Question: each Ask's
// chosen labels, keyed by the Ask's ID, or its Text when it has none.
type Answerer interface {
	AnswerQuestion(id string, answers map[string][]string) error
}

// Input is a message to a session.
type Input struct {
	Text   string
	Images []string // paths
}

// HistoryReader reads a session's transcript back as the events a live
// session sends.
type HistoryReader interface {
	History(s Session, before time.Time) ([]event.Event, error)
}

// QuotaSource reads an account's limits. p is the profile to read them
// through, for agents whose sign-in lives in the profile.
type QuotaSource interface {
	Quota(ctx context.Context, p Profile, a Account) (usage.Quota, error)
}

// Accounts signs in to, lists and switches an agent's accounts.
type Accounts interface {
	Accounts() []Account
	Current(p Profile) (Account, error)
	Switch(p Profile, a Account) error
	SignIn(p Profile) *exec.Cmd
}

// Pricer prices a model's tokens.
type Pricer interface {
	Cost(model string, u usage.TokenUsage) (float64, bool)
}

// Commander lists the slash commands and skills a session can run.
type Commander interface {
	Commands(p Profile, cwd string) []Command
}

// Session is one conversation, running or past.
type Session struct {
	Kind       Kind
	Profile    Profile
	ID         string
	Name       string
	State      string // working, blocked, idle, done, stopped
	Detail     string // what it's doing, in words
	Needs      string // what it's waiting on you for
	Cwd        string
	Model      string
	Transcript string
	PID        int
	Todos      []Todo
	Running    []Task
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// Extra is the adapter's own, for its own use: Claude's job file.
	Extra any
}

var (
	mu       sync.RWMutex
	adapters = map[Kind]Adapter{}
)

// Register makes an adapter known. Adapters call it from init.
func Register(a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	adapters[a.Kind()] = a
}

// Get is the adapter for kind, if one is registered.
func Get(k Kind) (Adapter, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := adapters[k]
	return a, ok
}

// All is every registered adapter, by kind.
func All() []Adapter {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Adapter, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind() < out[j].Kind() })
	return out
}
