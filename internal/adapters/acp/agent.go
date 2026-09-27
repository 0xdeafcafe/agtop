package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// Agent is an agent agtop knows only through ACP: how to start it, and
// where it keeps its things. The agents below need nothing more yet.
type Agent struct {
	ID      agent.Kind
	Title   string
	Command string   // its program
	Args    []string // what makes it speak ACP
	Home    string   // its config folder, under the home folder: ".kimi-code"
}

// Known are the ACP agents agtop runs. An agent that grows more than ACP
// gives (history, limits) moves to a package of its own, as Copilot has.
var Known = []Agent{
	{ID: "gemini", Title: "Gemini", Command: "gemini", Args: []string{"--experimental-acp"}, Home: ".gemini"},
	{ID: "kimi", Title: "Kimi", Command: "kimi", Args: []string{"acp"}, Home: ".kimi-code"},
	{ID: "opencode", Title: "OpenCode", Command: "opencode", Args: []string{"acp"}, Home: ".config/opencode"},
	{ID: "vibe", Title: "Mistral Vibe", Command: "vibe-acp", Home: ".vibe"},
}

func init() {
	for _, a := range Known {
		agent.Register(a)
	}
}

func (a Agent) Kind() agent.Kind { return a.ID }
func (a Agent) Name() string     { return a.Title }

func (Agent) Caps() agent.Caps {
	return agent.CapResume | agent.CapImages | agent.CapModes | agent.CapQuestions | agent.CapMCP
}

// Profiles is its config folder, when it's installed.
func (a Agent) Profiles() []agent.Profile {
	if _, err := exec.LookPath(a.Command); err != nil {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []agent.Profile{{Kind: a.ID, Name: a.Title, Dir: filepath.Join(home, a.Home)}}
}

// ErrNoFork is a fork asked of an agent ACP can't fork.
var ErrNoFork = errors.New("acp: this agent can't fork a session")

// Start runs it over ACP, then puts it in the mode and model asked for.
func (a Agent) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	if o.Fork {
		return nil, ErrNoFork
	}
	cmd := a.Command
	if o.Binary != "" {
		cmd = o.Binary
	}
	opts := Options{Command: cmd, Args: append(append([]string(nil), a.Args...), o.Flags...), Env: o.Env, Dir: o.Dir, Adapter: string(a.ID)}
	if o.Resume {
		opts.Resume = o.SessionID
	}
	s, err := Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	if o.Mode != "" {
		if err := s.SetMode(o.Mode); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	if o.Model != "" {
		if err := s.SetModel(o.Model); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	return s, nil
}

var (
	_ agent.Adapter  = Agent{}
	_ agent.Driver   = Agent{}
	_ agent.Conn     = (*Session)(nil)
	_ agent.Answerer = (*Session)(nil)
)
