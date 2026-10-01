// Package pi is Pi, Mario Zechner's coding agent, as a rush adapter. It
// drives `pi --mode rpc`, Pi's own protocol of JSON lines over stdin and
// stdout, and reads Pi's session files back for past sessions.
package pi

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Kind is Pi's.
const Kind agent.Kind = "pi"

func init() { agent.Register(Adapter{}) }

// Adapter is Pi.
type Adapter struct{}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "Pi" }

// Program is pi, as npm, bun or pnpm put it.
func (Adapter) Program() (string, []string) {
	return "pi", []string{"Library/pnpm", ".local/share/pnpm"}
}

// Published is Pi's package on npm.
func (Adapter) Published() agent.Published {
	return agent.Published{NPM: "@earendil-works/pi-coding-agent"}
}

// features are what RPC mode gives rush. Pi runs every tool without
// asking and keeps no modes, plans, subagents or MCP by design; those come
// as extensions, of which rush knows the subagent one's tool.
var features = map[agent.Feature]agent.Support{
	agent.FeatureRun: agent.Yes, agent.FeaturePrompt: agent.Yes, agent.FeatureResume: agent.Yes, agent.FeatureFork: agent.Yes.With("the whole session, not from a message"),
	agent.FeatureInterrupt: agent.Yes, agent.FeatureModel: agent.Yes.With("any model Pi has, at once"),
	agent.FeatureEffort: agent.Yes.With("its thinking level, from the start"), agent.FeatureImages: agent.Yes,
	agent.FeatureCompact: agent.Yes, agent.FeatureHandoffIn: agent.Yes,
	agent.FeatureQuestions: agent.Yes.With("an extension's select and confirm"),
	agent.FeatureContext:   agent.Yes.With("how full it is, not what fills it"),
	agent.FeatureLive:      agent.Yes.With("sessions written to in the last two minutes"), agent.FeatureHistory: agent.Yes,
	agent.FeatureModes: agent.No.With("Pi asks before nothing: every tool runs"),
	agent.FeaturePlan:  agent.No.With("only as an extension"), agent.FeatureSubagents: agent.Yes.With("the subagent extension's, when it's installed"),
	agent.FeatureMCP:      agent.No.With("only as an extension"),
	agent.FeatureRewind:   agent.No.With("its /tree is its own screen's"),
	agent.FeatureCommands: agent.Yes.With("prompt templates and skills; not packages' or extensions'"),
	agent.FeaturePricing:  agent.No.With("Pi prices each turn itself"),
	agent.FeatureSignIn:   agent.Yes.With("its own /login; no account swapping"),
	agent.FeatureQuota:    agent.No.With("each provider keeps its own limits"),
	agent.FeatureMemory:   agent.Yes.With("its AGENTS.md files"), agent.FeatureSettings: agent.Yes.With("settings.json, yours and the project's"),
}

func (Adapter) Features() map[agent.Feature]agent.Support { return features }
func (Adapter) Level() agent.Level                        { return agent.LevelTested }

// Home is Pi's config folder: PI_CODING_AGENT_DIR, else ~/.pi/agent.
func Home() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

// Profiles is Pi's config folder, when it exists and pi is installed: a
// folder left behind by an uninstalled pi lists nothing.
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) {
		return nil
	}
	dir := Home()
	if fi, err := os.Stat(dir); dir == "" || err != nil || !fi.IsDir() {
		return nil
	}
	return []agent.Profile{{Kind: Kind, Name: "Pi", Dir: dir}}
}

// Start runs a session in its own pi.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) { //nolint:gocritic // agent.Driver's signature
	c, err := Start(ctx, &o)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// RunsSession is whether a command line is a pi in RPC mode on session
// sessionID: one rush started, resumed or forked it with.
func (Adapter) RunsSession(args []string, sessionID string) bool {
	if sessionID == "" || !slices.Contains(args, "rpc") {
		return false
	}
	for i, a := range args[:max(len(args)-1, 0)] {
		switch a {
		case "--session", "--session-id", "--fork":
			if v := args[i+1]; v == sessionID || idOfFile(v) == sessionID {
				return true
			}
		}
	}
	return false
}

var (
	_ agent.Adapter    = Adapter{}
	_ agent.Driver     = Adapter{}
	_ agent.Programmer = Adapter{}
	_ agent.Orphans    = Adapter{}
)
