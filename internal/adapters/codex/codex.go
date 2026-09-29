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

// features are what app-server gives agtop. Codex has no subagents, no
// plan mode among its approval presets, and nothing to rewind to.
var features = map[agent.Feature]agent.Support{
	agent.FeatureRun: agent.Yes, agent.FeatureResume: agent.Yes, agent.FeatureFork: agent.Yes,
	agent.FeatureInterrupt: agent.Yes, agent.FeatureModel: agent.Yes.With("from the next turn"),
	agent.FeatureEffort: agent.Yes, agent.FeatureModes: agent.Yes.With("read-only, auto, full-access"),
	agent.FeatureImages: agent.Yes, agent.FeatureQuestions: agent.Yes, agent.FeatureContext: agent.Yes.With("how full it is, not what fills it"),
	agent.FeatureMCP: agent.Yes, agent.FeatureHandoffIn: agent.Yes,
	agent.FeatureLive: agent.Yes, agent.FeatureHistory: agent.Yes,
	agent.FeatureSwitch: agent.Yes, agent.FeatureSignIn: agent.Yes, agent.FeatureQuota: agent.Yes,
	agent.FeatureCommands: agent.Planned, agent.FeaturePricing: agent.Planned, agent.FeatureEfficiency: agent.Planned,
}

func (Adapter) Features() map[agent.Feature]agent.Support { return features }
func (Adapter) Level() agent.Level                        { return agent.LevelTested }

// Profiles is CODEX_HOME, or ~/.codex, when it exists and codex is
// installed: a folder left behind by an uninstalled codex lists nothing.
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) {
		return nil
	}
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
