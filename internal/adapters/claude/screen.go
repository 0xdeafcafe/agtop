package claude

import (
	"fmt"
	"io"
	"os/exec"
	"slices"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
	"github.com/0xdeafcafe/rush/internal/daemon"
)

// Claude Code's daemon keeps its background sessions' screens: rush opens
// them full screen, mirrors them in the preview, and keeps the daemon's
// pins.

func client(p agent.Profile) daemon.Client { return daemon.Client{Account: claude.AccountOf(p)} }

// Join attaches to session short's screen in rush's terminal.
func (Adapter) Join(p agent.Profile, short string) agent.Terminal {
	return joined{&daemon.Session{Client: client(p), Short: short}}
}

type joined struct{ *daemon.Session }

// Run says why the daemon refused, in rush's words.
func (j joined) Run() error {
	err := j.Session.Run()
	switch {
	case daemon.IsRefusal(err, "EKICKED"):
		return fmt.Errorf("%w: %w", agent.ErrElsewhere, err)
	case daemon.IsRefusal(err, "ENOJOB"):
		return fmt.Errorf("%w: %w", agent.ErrGone, err)
	}
	return err
}

func (Adapter) ServiceUp(p agent.Profile) bool { return client(p).Running() }

func (Adapter) Mirror(p agent.Profile, short, viewer string, cols, rows int) (*agent.Mirror, error) {
	conn, r, info, err := client(p).Attach(short, viewer, cols, rows)
	if err != nil {
		return nil, err
	}
	return &agent.Mirror{Modes: info.DecModes, Conn: conn, Relay: func(w io.Writer) error { return daemon.Relay(r, w) }}, nil
}

func (Adapter) Resize(p agent.Profile, short, viewer string, cols, rows int) error {
	return client(p).Resize(short, viewer, cols, rows)
}

// TogglePin pins session short in the daemon's list, or unpins it.
func (Adapter) TogglePin(p agent.Profile, short string) error {
	acct := claude.AccountOf(p)
	pins, err := claude.LoadPins(acct)
	if err != nil {
		return err
	}
	out := slices.DeleteFunc(slices.Clone(pins), func(id string) bool { return id == short })
	if len(out) == len(pins) {
		out = append(out, short)
	}
	return claude.WritePins(acct, out)
}

// SessionsView is claude agents, Claude Code's own list of its sessions.
func (Adapter) SessionsView(p agent.Profile) *exec.Cmd {
	return claudeCmd(claude.AccountOf(p), "", "agents")
}

var (
	_ agent.Joiner         = Adapter{}
	_ agent.Pinner         = Adapter{}
	_ agent.SessionsViewer = Adapter{}
)
