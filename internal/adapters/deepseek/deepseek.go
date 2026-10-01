// Package deepseek is DeepSeek's own coding agent, DeepSeek Harness (dsh,
// npm @deepseek-ai/dsh), as a rush adapter. rush runs it over ACP with
// its shipped "acp" profile (dsh --profile acp), and reads the balance of
// the DeepSeek API key it uses. It stays out of sight until dsh is
// installed.
package deepseek

import (
	"context"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/adapters/acp"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// Kind is DeepSeek's.
const Kind agent.Kind = "deepseek"

func init() { agent.Register(Adapter{}) }

// Adapter is DeepSeek Harness.
type Adapter struct{}

// cli is dsh over ACP. SDK and ACP are dsh profiles rather than programs
// of their own; the acp profile sets itself up on first use.
var cli = acp.Agent{ID: Kind, Title: "DeepSeek", Command: "dsh", Args: []string{"--profile", "acp"}, Home: ".dsh"}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "dsh" }
func (Adapter) Maker() string    { return "DeepSeek" }

// KeyEnv is where DeepSeek's API key is read from.
func (Adapter) KeyEnv() string { return "DEEPSEEK_API_KEY" }

// Program is dsh, which npm puts on PATH.
func (Adapter) Program() (string, []string) { return "dsh", []string{".dsh/bin"} }

// features are dsh's over ACP: it resumes without replaying, has no modes
// and asks no questions. Its money left is read, not a window.
var features = map[agent.Feature]agent.Support{
	agent.FeatureRun: agent.Yes, agent.FeatureResume: agent.Yes.With("without replaying the conversation"),
	agent.FeatureInterrupt: agent.Yes, agent.FeatureModel: agent.Yes.With("when dsh offers models"),
	agent.FeatureImages: agent.Yes.With("on models that take them"), agent.FeatureMCP: agent.Yes,
	agent.FeatureHandoffIn: agent.Yes, agent.FeatureQuota: agent.Yes.With("the API key's balance"),
	agent.FeatureBackground: agent.No.With("ACP has no background tasks"),
	agent.FeatureHistory:    agent.No.With("dsh's session logs are compressed and still change"),
}

func (Adapter) Features() map[agent.Feature]agent.Support { return features }
func (Adapter) Level() agent.Level                        { return agent.LevelPreview }

// Profiles is DSH_HOME, or ~/.dsh, when dsh is installed.
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) {
		return nil
	}
	return []agent.Profile{{Kind: Kind, Name: "DeepSeek", Dir: home()}}
}

// home is dsh's home: DSH_HOME, else ~/.dsh.
func home() string {
	if d := os.Getenv("DSH_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".dsh")
}

// Start runs dsh over ACP in the profile's home. dsh has no modes, so a
// mode asked for is left alone rather than failing the start.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	o.Mode = ""
	if o.Profile.Dir != "" && o.Profile.Dir != home() {
		o.Env = append(append([]string(nil), o.Env...), "DSH_HOME="+o.Profile.Dir)
	}
	return cli.Start(ctx, o)
}

var (
	_ agent.Adapter     = Adapter{}
	_ agent.Driver      = Adapter{}
	_ agent.Programmer  = Adapter{}
	_ agent.QuotaSource = Adapter{}
)
