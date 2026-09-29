package agent

import (
	"errors"
	"os/exec"
)

// What agtop does to a session or an account that runs outside agtop
// mode, through the agent's own program: each an interface an adapter has
// only when its agent can, found with As.

// Dispatcher starts a session in the agent's own background service, and
// returns its id.
type Dispatcher interface {
	Dispatch(p Profile, dir, prompt string, flags ...string) (id string, err error)
}

// Stopper ends a session running outside agtop and keeps its conversation.
// pid, when known, is ended if the agent's own way doesn't.
type Stopper interface {
	Stop(p Profile, id string, pid int) error
}

// Remover deletes a session the agent keeps, and what it made for it.
type Remover interface {
	Remove(p Profile, id string) error
}

// Replier sends a message to a session in the agent's background service.
// A session it has let go of is ErrGone.
type Replier interface {
	Reply(p Profile, id, text string) error
}

// Attacher opens a background session in the agent's own terminal UI.
type Attacher interface {
	Attach(p Profile, id string) *exec.Cmd
}

// Loginer signs p in, in the terminal.
type Loginer interface {
	Login(p Profile) *exec.Cmd
}

// Screener opens one of the agent's own screens in the terminal: a fresh
// program on a slash command, in dir, with hint printed above it.
type Screener interface {
	Screen(p Profile, dir, command, hint string) *exec.Cmd
}

// Mover moves a session running outside agtop to another profile or
// folder, keeping its conversation, and returns its new id.
type Mover interface {
	Move(m *Move) (id string, err error)
}

// Move is where a session goes.
type Move struct {
	From, To Profile
	Job      Job      // the session as listed
	Extra    any      // the adapter's own record of it, from its listing
	Dir      string   // its new folder; empty keeps the old one
	AddDirs  []string // more folders to give it
	Note     string   // the first message once it's moved
}

// ErrGone is a session its agent has let go of: its conversation is kept,
// and carries on in agtop mode.
var ErrGone = errors.New("the agent has let the session go")

// As is agent k's adapter as T, when it's registered and is one.
func As[T any](k Kind) (T, bool) {
	var zero T
	a, ok := Get(k)
	if !ok {
		return zero, false
	}
	t, ok := a.(T)
	return t, ok
}
