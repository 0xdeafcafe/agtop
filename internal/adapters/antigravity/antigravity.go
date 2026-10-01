// Package antigravity runs Google's agy CLI using its persistent NDJSON mode.
package antigravity

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/acp"
	"github.com/0xdeafcafe/rush/internal/agent"
)

const Kind agent.Kind = "antigravity"

type Adapter struct{}

func init()                                 { agent.Register(Adapter{}) }
func (Adapter) Kind() agent.Kind            { return Kind }
func (Adapter) Name() string                { return "Antigravity CLI" }
func (Adapter) Maker() string               { return "Google" }
func (Adapter) Level() agent.Level          { return agent.LevelPreview }
func (Adapter) Program() (string, []string) { return "agy", []string{".local/bin"} }
func (Adapter) Published() agent.Published  { return agent.Published{Update: []string{"update"}} }
func (Adapter) Profiles() []agent.Profile {
	if !agent.Installed(Kind) {
		return nil
	}
	h, _ := os.UserHomeDir()
	return []agent.Profile{{Kind: Kind, Name: "Antigravity", Dir: filepath.Join(h, ".gemini", "antigravity-cli")}}
}
func (Adapter) Features() map[agent.Feature]agent.Support {
	return map[agent.Feature]agent.Support{
		agent.FeatureRun: agent.Yes, agent.FeatureResume: agent.Yes, agent.FeatureInterrupt: agent.Yes,
		agent.FeatureSignIn:    agent.Yes.With("Antigravity's native Google sign-in"),
		agent.FeatureModel:     agent.No.With("choose before starting; streaming sessions cannot switch models"),
		agent.FeatureEffort:    agent.Yes.With("selected at startup"),
		agent.FeatureModes:     agent.Yes.With("selected at startup; headless tools follow native policy"),
		agent.FeaturePlan:      agent.Yes.With("selected at startup"),
		agent.FeatureImages:    agent.No.With("agy's streaming input accepts text only"),
		agent.FeatureQuestions: agent.No.With("native headless policy; no interactive approval protocol"),
		agent.FeatureQuota:     agent.No.With("use /usage in Antigravity; Gemini Code Assist limits do not apply"),
		agent.FeatureHistory:   agent.Yes.With("conversations started in Rush; native imports are not available"),
	}
}
func (Adapter) Choices() agent.Choices {
	return agent.Choices{Efforts: []agent.Choice{{ID: "low"}, {ID: "medium"}, {ID: "high"}, {ID: "max"}}, Modes: []agent.Choice{{ID: "accept-edits", Note: "native edit policy"}, {ID: "plan", Note: "planning mode"}, {ID: "always-proceed", Note: "approve every tool; skips permission prompts"}}}
}
func (Adapter) SpawnCommand() (string, string) { return `agy -p "<task>"`, "--model" }
func (Adapter) ReadOnce(args []string) (agent.Once, bool) {
	return (acp.Agent{Once: `agy -p "<task>"`, Flags: map[string]string{"-p": "prompt", "--print": "prompt", "--prompt": "prompt", "--model": "model", "--output-format": "=text", "--dangerously-skip-permissions": "mode=always-proceed"}}).ReadOnce(args)
}
func (Adapter) SignInCommand(agent.Profile) (*exec.Cmd, error) {
	bin := agent.Path(Kind)
	if bin == "" {
		return nil, errors.New("Antigravity CLI is not installed: install agy from antigravity.google")
	}
	return exec.Command(bin), nil
}

var modelCache struct {
	sync.Mutex
	at   time.Time
	data []byte
	err  error
}

func modelsOutput() ([]byte, error) {
	modelCache.Lock()
	defer modelCache.Unlock()
	if time.Since(modelCache.at) < 2*time.Second {
		return modelCache.data, modelCache.err
	}
	b, err := readModels()
	modelCache.at, modelCache.data, modelCache.err = time.Now(), b, err
	writeRunnerSnapshot(b, err == nil)
	return b, err
}
func readModels() ([]byte, error) {
	bin := agent.Path(Kind)
	if bin == "" {
		return nil, errors.New("Antigravity CLI is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, bin, "models").CombinedOutput()
	if err != nil {
		return nil, errors.New("Antigravity models unavailable; sign in through Settings → Harnesses → Antigravity CLI")
	}
	return b, nil
}
func (Adapter) CheckKey(agent.Profile) error { _, err := modelsOutput(); return err }
func (Adapter) AccountSummary(p agent.Profile) agent.AccountSummary {
	err := (Adapter{}).CheckKey(p)
	if err != nil {
		return agent.AccountSummary{Status: err.Error()}
	}
	return agent.AccountSummary{SignedIn: true, Status: "Antigravity account available", Method: "native sign-in", Note: "Identity and plan are managed by Antigravity"}
}
func (Adapter) ListModels(agent.Profile) []agent.Choice {
	b, err := modelsOutput()
	if err != nil {
		return nil
	}
	return parseModels(string(b))
}
func parseModels(text string) []agent.Choice {
	var out []agent.Choice
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.Contains(f[0], "-") || strings.ContainsAny(f[0], ":│") {
			continue
		}
		out = append(out, agent.Choice{ID: f[0], Note: strings.Join(f[1:], " ")})
	}
	return out
}
