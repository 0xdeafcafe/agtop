// Package gemini is Google's Gemini CLI as a rush adapter: gemini over
// ACP, in ~/.gemini. Signed in with Google it runs on Code Assist, free or
// paid, whose daily allowance rush reads as Gemini itself does; on an API
// key it's paid by use, with no allowance to read.
package gemini

import (
	"github.com/0xdeafcafe/rush/internal/adapters/acp"
	_ "github.com/0xdeafcafe/rush/internal/adapters/antigravity"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// Kind is Gemini's.
const Kind agent.Kind = "gemini"

func init() {
	agent.Register(Adapter{acp.Agent{
		ID: Kind, Title: "Gemini CLI", Company: "Google", Command: "gemini", Args: []string{"--experimental-acp"}, Home: ".gemini",
		Pub:   agent.Published{NPM: "@google/gemini-cli"},
		Creds: []string{credsFile}, Keys: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
		SignIn: "run gemini once and pick Sign in with Google (free), or set GEMINI_API_KEY",
		More:   map[agent.Feature]agent.Support{agent.FeatureQuota: agent.Yes.With("legacy Code Assist quota")},
		Once:   `gemini -p "<task>"`, Model: "-m", Flags: map[string]string{"-p": "prompt", "--prompt": "prompt",
			"-m": "model", "--model": "model", "-o": "=text", "--output-format": "=text"},
	}})
}

// Adapter is Gemini: what ACP gives it, and its allowance.
type Adapter struct{ acp.Agent }

var (
	_ agent.Driver      = Adapter{}
	_ agent.OnceReader  = Adapter{}
	_ agent.KeyChecker  = Adapter{}
	_ agent.QuotaSource = Adapter{}
)

func (Adapter) Replacement() agent.Kind { return "antigravity" }
