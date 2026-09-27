// Package copilot is GitHub Copilot as an agtop adapter: the Copilot CLI,
// run over ACP in agtop mode, and Copilot's coding agent, whose sessions on
// GitHub agtop lists and reads. Both are reached with your GitHub sign-in:
// gh's, when the CLI has none of its own.
package copilot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/adapters/acp"
	"github.com/0xdeafcafe/agtop/internal/agent"
)

// Kind is Copilot's.
const Kind agent.Kind = "copilot"

func init() { agent.Register(Adapter{}) }

// Adapter is GitHub Copilot.
type Adapter struct{}

// cli is the Copilot CLI over ACP.
var cli = acp.Agent{ID: Kind, Title: "Copilot", Command: "copilot", Args: []string{"--acp"}}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "Copilot" }
func (Adapter) Caps() agent.Caps { return cli.Caps() }

// Profiles is COPILOT_HOME, or ~/.copilot, when the CLI is installed or gh
// is: the coding agent needs only a GitHub sign-in.
func (Adapter) Profiles() []agent.Profile {
	_, errCLI := exec.LookPath("copilot")
	_, errGH := exec.LookPath("gh")
	if errCLI != nil && errGH != nil {
		return nil
	}
	dir := os.Getenv("COPILOT_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".copilot")
	}
	return []agent.Profile{{Kind: Kind, Name: "Copilot", Dir: dir}}
}

// Start runs the Copilot CLI over ACP. Without a sign-in of its own it gets
// gh's, which it takes as GH_TOKEN.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	if !signedIn(o.Profile.Dir) && !hasToken(o.Env) {
		if tok, err := token(); err == nil {
			o.Env = append(append([]string(nil), o.Env...), "GH_TOKEN="+tok)
		}
	}
	if o.Profile.Dir != "" {
		o.Env = append(o.Env, "COPILOT_HOME="+o.Profile.Dir)
	}
	return cli.Start(ctx, o)
}

// signedIn is whether the CLI has a sign-in of its own in dir.
func signedIn(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	return err == nil && strings.Contains(string(b), "logged_in_users")
}

func hasToken(env []string) bool {
	for _, kv := range env {
		for _, k := range []string{"COPILOT_GITHUB_TOKEN=", "GH_TOKEN=", "GITHUB_TOKEN="} {
			if strings.HasPrefix(kv, k) {
				return true
			}
		}
	}
	return false
}

var (
	_ agent.Adapter       = Adapter{}
	_ agent.Driver        = Adapter{}
	_ agent.QuotaSource   = Adapter{}
	_ agent.Discoverer    = Adapter{}
	_ agent.HistoryReader = Adapter{}
)
