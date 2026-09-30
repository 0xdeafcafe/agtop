// Package ollama is models served by Ollama on this machine, as a rush
// adapter. There's no agent of Ollama's own to run: rush runs Claude Code
// on Ollama's Anthropic-compatible API, fitted to the model it runs (see
// tune), in a Claude config folder of its own, so that a local model
// reads none of the user's plugins, skills and MCP servers before every
// answer. It stays out of sight until both ollama and claude are installed.
package ollama

import (
	"context"
	"path/filepath"

	claudead "github.com/0xdeafcafe/rush/internal/adapters/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Kind is Ollama's.
const Kind agent.Kind = "ollama"

func init() { agent.Register(Adapter{}) }

// Adapter is Claude Code on Ollama.
type Adapter struct{}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "Ollama" }

// Program is ollama, which it needs beside Claude Code.
func (Adapter) Program() (string, []string) {
	return "ollama", []string{"/Applications/Ollama.app/Contents/Resources"}
}

// features are Claude Code's, less what a local model can't do or its
// separate folder hides: its sessions aren't in ~/.claude for fleet to
// find, and there are no limits or accounts, only this machine.
var features = map[agent.Feature]agent.Support{
	agent.FeatureRun: agent.Yes, agent.FeatureResume: agent.Yes, agent.FeatureFork: agent.Yes,
	agent.FeatureInterrupt: agent.Yes, agent.FeatureGuide: agent.Yes.With("at its next step"), agent.FeatureModel: agent.Yes.With("any model Ollama has that calls tools"),
	agent.FeatureModes: agent.Yes, agent.FeatureCompact: agent.Yes.With("at the window the model was loaded with"),
	agent.FeatureImages:    agent.Yes.With("on models that take them"),
	agent.FeatureHandoffIn: agent.Yes, agent.FeaturePricing: agent.Yes.With("free: it runs on this machine"),
	agent.FeatureEffort:    agent.No.With("a model thinks or doesn't, as it was made"),
	agent.FeatureSubagents: agent.No.With("left out to keep the prompt small"),
	agent.FeatureQuota:     agent.No.With("no limits but this machine's"),
}

func (Adapter) Features() map[agent.Feature]agent.Support { return features }
func (Adapter) Level() agent.Level                        { return agent.LevelTested }

// Rides Claude Code: its sessions are claude's.
func (Adapter) Rides() agent.Kind { return claudead.Kind }

// Profiles is rush's own Claude folder for Ollama, when ollama and
// Claude Code are both here.
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) {
		return nil
	}
	return []agent.Profile{{Kind: Kind, Name: "Ollama", Dir: home()}}
}

// home is the Claude config folder sessions on Ollama keep to.
func home() string { return filepath.Join(state.Dir(), "ollama") }

// Cost is nothing: the model runs here.
func (Adapter) Cost(string, usage.TokenUsage) (float64, bool) { return 0, true }

// Start picks and loads the model, then runs Claude Code on it.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	m, err := pick(ctx, o.Model)
	if err != nil {
		return nil, err
	}
	env, flags := tune(m, server(), o.Flags)
	o.Env = append(append([]string(nil), o.Env...), env...)
	o.Flags, o.Model, o.Effort = flags, m.Name, ""
	if o.Profile.Dir == "" {
		o.Profile.Dir = home()
	}
	// The host hands over the adapter's own program, which is ollama's,
	// unless it was told of a claude to run.
	if o.Binary == "" || filepath.Base(o.Binary) == "ollama" {
		o.Binary = agent.Path(claudead.Kind)
	}
	return claudead.Adapter{}.Start(ctx, o)
}

var (
	_ agent.Adapter    = Adapter{}
	_ agent.Driver     = Adapter{}
	_ agent.Programmer = Adapter{}
	_ agent.Pricer     = Adapter{}
	_ agent.Rider      = Adapter{}
)
