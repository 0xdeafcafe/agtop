// Package codex is OpenAI's Codex CLI as an agtop adapter. It drives
// `codex app-server`, Codex's own JSON-RPC protocol, rather than ACP,
// because only it reports the account's rate-limit windows.
package codex

import (
	"context"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// Kind is Codex's.
const Kind agent.Kind = "codex"

func init() { agent.Register(Adapter{}) }

// Adapter is the Codex CLI.
type Adapter struct{}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "Codex" }

// Program is codex.
func (Adapter) Program() (string, []string) { return "codex", []string{".codex/bin"} }

func (Adapter) Caps() agent.Caps {
	return agent.CapResume | agent.CapFork | agent.CapImages | agent.CapEffort | agent.CapModes |
		agent.CapQuestions | agent.CapContext | agent.CapMCP
}

// Profiles is CODEX_HOME, or ~/.codex, when it exists.
func (Adapter) Profiles() []agent.Profile {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".codex")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil
	}
	return []agent.Profile{{Kind: Kind, Name: filepath.Base(dir), Dir: dir}}
}

// Quota reads the limits of the account p is signed in to.
func (Adapter) Quota(ctx context.Context, p agent.Profile, _ agent.Account) (usage.Quota, error) {
	return Quota(ctx, p, "")
}

var _ agent.QuotaSource = Adapter{}

// Start runs a thread in its own app-server.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	c, err := Start(ctx, o)
	if err != nil {
		return nil, err
	}
	return c, nil
}
