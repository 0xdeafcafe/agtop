package agent

import (
	"errors"
	"io"
)

// A background session's own screen, where its agent keeps one in a
// service of its own (Claude Code's daemon): rush opens it in its
// terminal, or mirrors it in a pane.

// Terminal is a session's own screen, run in rush's terminal until you
// leave it.
type Terminal interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

// Mirror is a session's screen attached for a pane to draw.
type Mirror struct {
	// Modes are the private modes its screen has on.
	Modes []int
	// Conn takes the answers to its terminal's queries; closing it
	// detaches.
	Conn io.WriteCloser
	// Relay writes what the screen draws to w, until it detaches.
	Relay func(w io.Writer) error
}

// Joiner is an agent whose background sessions' own screens rush opens.
type Joiner interface {
	// Join is session id's screen in rush's terminal. Its Run fails with
	// ErrGone for a session the agent has let go, and ErrElsewhere once
	// it was opened in another window.
	Join(p Profile, id string) Terminal
	// ServiceUp is whether p's background service runs. It asks the
	// service, so never on the UI.
	ServiceUp(p Profile) bool
	// Mirror attaches to session id as viewer, cols by rows.
	Mirror(p Profile, id, viewer string, cols, rows int) (*Mirror, error)
	// Resize resizes viewer's attachment to session id.
	Resize(p Profile, id, viewer string, cols, rows int) error
}

// ErrElsewhere is a session's screen opened in another window, which
// takes it from this one.
var ErrElsewhere = errors.New("the session was opened in another window")
