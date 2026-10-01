// Package glm is Z.ai's GLM models through ZCode, Z.ai's own coding agent,
// as a rush adapter. ZCode doesn't speak ACP itself: its machine
// interface is its own protocol (zcode app-server --stdio). rush runs it
// through zcode-acp-server (npm zcode-acp), which drives that same ZCode
// runtime and speaks ACP, and reads the GLM Coding Plan's limits as ZCode
// does. It stays out of sight until both are installed.
package glm

import (
	"context"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/adapters/acp"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// Kind is GLM's.
const Kind agent.Kind = "glm"

func init() { agent.Register(Adapter{}) }

// Adapter is GLM through ZCode.
type Adapter struct{}

// cli is ZCode over ACP, through the bridge.
var cli = acp.Agent{ID: Kind, Title: "GLM", Command: "zcode-acp-server",
	More: map[agent.Feature]agent.Support{agent.FeatureQuota: agent.Yes.With("the GLM Coding Plan's")}}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "ZCode" }
func (Adapter) Maker() string    { return "GLM" }

// KeyEnv is where Z.ai's API key is read from.
func (Adapter) KeyEnv() string { return "ZAI_API_KEY" }

// Program is the ACP bridge, what rush runs.
func (Adapter) Program() (string, []string) { return "zcode-acp-server", nil }

// Features are the bridge's over ACP (ZCode's modes: plan, build, edit,
// yolo, auto), and the Coding Plan's limits. It's been tried only against
// recorded fixtures.
func (Adapter) Features() map[agent.Feature]agent.Support { return cli.Features() }
func (Adapter) Level() agent.Level                        { return cli.Level() }

// Profiles is ZCODE_HOME, or ~/.zcode, when the bridge is installed and
// has a ZCode to drive.
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) || !hasZCode() {
		return nil
	}
	return []agent.Profile{{Kind: Kind, Name: "GLM", Dir: home()}}
}

// home is ZCode's data folder: ZCODE_HOME, else ~/.zcode.
func home() string {
	if d := os.Getenv("ZCODE_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".zcode")
}

// appBundle is the CLI inside ZCode's desktop app, which the bridge finds
// on its own.
var appBundle = "/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs"

// hasZCode is whether the bridge has a ZCode to run, where it looks for
// one: ZCODE_BIN, zcode (its installer puts it in ~/.local/bin), or the
// desktop app.
func hasZCode() bool {
	if p := os.Getenv("ZCODE_BIN"); p != "" {
		_, err := os.Stat(p)
		return err == nil
	}
	if _, ok := agent.Find("zcode"); ok {
		return true
	}
	_, err := os.Stat(appBundle)
	return err == nil
}

// Start runs ZCode over ACP through the bridge, in the profile's home.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	if o.Profile.Dir != "" && o.Profile.Dir != home() {
		o.Env = append(append([]string(nil), o.Env...), "ZCODE_HOME="+o.Profile.Dir)
	}
	return cli.Start(ctx, o)
}

var (
	_ agent.Adapter     = Adapter{}
	_ agent.Driver      = Adapter{}
	_ agent.Programmer  = Adapter{}
	_ agent.QuotaSource = Adapter{}
)
