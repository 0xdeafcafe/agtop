package ollama

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	codexad "github.com/0xdeafcafe/rush/internal/adapters/codex"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/state"
)

// CodexKind is Ollama in Codex's.
const CodexKind agent.Kind = "ollama-codex"

func init() { agent.Register(CodexAdapter{}) }

// CodexAdapter is Codex on Ollama: `codex app-server`, as the codex
// adapter drives it, with a model provider of rush's own pointed at
// Ollama's OpenAI-compatible Responses API, in a CODEX_HOME of its own so
// its threads never mix with the user's ~/.codex, nor reach their OpenAI
// account.
type CodexAdapter struct{}

func (CodexAdapter) Kind() agent.Kind { return CodexKind }
func (CodexAdapter) Name() string     { return "Ollama in Codex" }

// Program is ollama, which it needs beside Codex.
func (CodexAdapter) Program() (string, []string) { return Adapter{}.Program() }

// Rides Codex: its sessions are codex's threads.
func (CodexAdapter) Rides() agent.Kind { return codexad.Kind }

// Provider is Ollama, whichever harness it runs in.
func (CodexAdapter) Provider() string { return string(Kind) }

// codexFeatures are Codex's, less what a local model or rush's own
// folder hasn't: no account, no limits, and nothing to sign in to.
var codexFeatures = map[agent.Feature]agent.Support{
	agent.FeatureRun: agent.Yes, agent.FeaturePrompt: agent.Yes, agent.FeaturePort: agent.Yes, agent.FeatureResume: agent.Yes, agent.FeatureFork: agent.Yes,
	agent.FeatureInterrupt: agent.Yes, agent.FeatureModel: agent.Yes.With("any model Ollama has that calls tools"),
	agent.FeatureModes:  agent.Yes.With("read-only, auto, full-access"),
	agent.FeatureImages: agent.Yes.With("on models that take them"), agent.FeatureQuestions: agent.Yes,
	agent.FeatureContext: agent.Yes.With("how full it is, not what fills it"), agent.FeatureCompact: agent.Yes,
	agent.FeatureHandoffIn: agent.Yes, agent.FeatureLive: agent.Yes, agent.FeatureHistory: agent.Yes,
	agent.FeaturePricing: agent.Yes.With("free: it runs on this machine"),
	agent.FeatureEffort:  agent.No.With("a model thinks or doesn't, as it was made"),
	agent.FeatureQuota:   agent.No.With("no limits but this machine's"),
	agent.FeatureSwitch:  agent.No.With("no accounts, only this machine"),
	agent.FeatureSignIn:  agent.No.With("nothing to sign in to"),
}

func (CodexAdapter) Features() map[agent.Feature]agent.Support { return codexFeatures }
func (CodexAdapter) Level() agent.Level                        { return agent.LevelTested }

// Profiles is rush's own CODEX_HOME for Ollama, when ollama and codex
// are both here.
func (CodexAdapter) Profiles() []agent.Profile {
	if !agent.Installed(CodexKind) {
		return nil
	}
	return []agent.Profile{{Kind: CodexKind, Name: "Ollama in Codex", Dir: codexHome()}}
}

// codexHome is the CODEX_HOME threads on Ollama keep to.
func codexHome() string { return filepath.Join(state.Dir(), "ollama-codex") }

// Cost is nothing: the model runs here.
func (CodexAdapter) Cost(string, usage.TokenUsage) (float64, bool) { return 0, true }

// codexProvider is the id of the model provider rush gives Codex. It's
// rush's own rather than Codex's built-in "ollama", so its base URL can
// follow OLLAMA_HOST.
const codexProvider = "rush-ollama"

// codexFlags are app-server's config overrides that run it on model m,
// served at base, ahead of flags so the caller's own win.
func codexFlags(m Model, base string, flags []string) []string {
	out := []string{
		"-c", "model_provider=" + strconv.Quote(codexProvider),
		"-c", "model_providers." + codexProvider + `={name="Ollama",base_url=` + strconv.Quote(base+"/v1") + `,wire_api="responses"}`,
		"-c", "model=" + strconv.Quote(m.Name),
	}
	if n := window(m); n > 0 {
		out = append(out, "-c", "model_context_window="+strconv.Itoa(n))
	}
	return append(out, flags...)
}

// Start picks and loads the model, then runs Codex on it.
func (CodexAdapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) { //nolint:gocritic // agent.Driver's signature
	m, err := pick(ctx, o.Model)
	if err != nil {
		return nil, err
	}
	o.Flags, o.Model, o.Effort = codexFlags(m, server(), o.Flags), m.Name, ""
	if o.Profile.Dir == "" {
		o.Profile.Dir = codexHome()
	}
	// Codex refuses a CODEX_HOME that isn't there, and nothing else makes it.
	if err := os.MkdirAll(o.Profile.Dir, 0o700); err != nil {
		return nil, err
	}
	// The host hands over the adapter's own program, which is ollama's,
	// unless it was told of a codex to run.
	if o.Binary == "" || filepath.Base(o.Binary) == "ollama" {
		o.Binary = agent.Path(codexad.Kind)
	}
	return codexad.Adapter{}.Start(ctx, o)
}

// Live are the threads Codex wrote to lately in rush's folder.
func (CodexAdapter) Live(p agent.Profile) []agent.Session {
	return ours(codexad.Adapter{}.Live(p))
}

// Past is every thread in rush's folder.
func (CodexAdapter) Past(p agent.Profile) []agent.Session {
	return ours(codexad.Adapter{}.Past(p))
}

// History reads a thread's rollout back, as Codex's own are.
func (CodexAdapter) History(s agent.Session, before time.Time) ([]event.Event, error) { //nolint:gocritic // HistoryReader takes the session by value
	return codexad.Adapter{}.History(s, before)
}

func (CodexAdapter) HistoryTail(s agent.Session, most int64) ([]event.Event, bool, error) { //nolint:gocritic // as History
	return codexad.Adapter{}.HistoryTail(s, most)
}

func (CodexAdapter) FollowHistory(s agent.Session, stop *atomic.Bool) func() ([]event.Event, error) { //nolint:gocritic // as History
	return codexad.Adapter{}.FollowHistory(s, stop)
}

// ours are codex's sessions as Ollama in Codex's.
func ours(ss []agent.Session) []agent.Session {
	for i := range ss {
		ss[i].Kind = CodexKind
		ss[i].Profile.Kind = CodexKind
	}
	return ss
}

var (
	_ agent.Adapter         = CodexAdapter{}
	_ agent.Driver          = CodexAdapter{}
	_ agent.Programmer      = CodexAdapter{}
	_ agent.Pricer          = CodexAdapter{}
	_ agent.Rider           = CodexAdapter{}
	_ agent.Provided        = CodexAdapter{}
	_ agent.Discoverer      = CodexAdapter{}
	_ agent.HistoryReader   = CodexAdapter{}
	_ agent.TailReader      = CodexAdapter{}
	_ agent.HistoryFollower = CodexAdapter{}
)
