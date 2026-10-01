// Package adapters_test checks every adapter together.
package adapters_test

import (
	_ "github.com/0xdeafcafe/rush/internal/adapters/antigravity"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/0xdeafcafe/rush/internal/adapters/acp"
	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	_ "github.com/0xdeafcafe/rush/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/rush/internal/adapters/cross"
	_ "github.com/0xdeafcafe/rush/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/rush/internal/adapters/gemini"
	_ "github.com/0xdeafcafe/rush/internal/adapters/glm"
	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama"
	_ "github.com/0xdeafcafe/rush/internal/adapters/pi"
	_ "github.com/0xdeafcafe/rush/internal/adapters/vibe"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// implements are the features that are an interface: an adapter has the
// feature exactly when it has the interface.
var implements = map[agent.Feature]func(agent.Adapter) bool{
	agent.FeatureRun:     func(a agent.Adapter) bool { _, ok := a.(agent.Driver); return ok },
	agent.FeatureLive:    func(a agent.Adapter) bool { _, ok := a.(agent.Discoverer); return ok },
	agent.FeatureHistory: func(a agent.Adapter) bool { _, ok := a.(agent.HistoryReader); return ok },
	agent.FeatureQuota:   func(a agent.Adapter) bool { _, ok := a.(agent.QuotaSource); return ok },
	agent.FeatureSwitch:  func(a agent.Adapter) bool { _, ok := a.(agent.Accounts); return ok },
	agent.FeatureSignIn: func(a agent.Adapter) bool {
		_, accounts := a.(agent.Accounts)
		_, native := a.(agent.Authenticator)
		return accounts || native
	},
	agent.FeaturePricing:  func(a agent.Adapter) bool { _, ok := a.(agent.Pricer); return ok },
	agent.FeatureCommands: func(a agent.Adapter) bool { _, ok := a.(agent.Commander); return ok },
}

// A feature an adapter declares Yes has its interface, and an interface
// it has is declared Yes.
func TestFeaturesMatchInterfaces(t *testing.T) {
	if len(agent.All()) < 9 {
		t.Fatalf("only %d adapters registered", len(agent.All()))
	}
	for _, a := range agent.All() {
		for f, has := range implements {
			yes := agent.Supports(a.Kind(), f)
			// Discoverer also lists saved transcripts; implementing Past need
			// not claim that external live processes can be tracked.
			if f == agent.FeatureLive && !yes && agent.Supports(a.Kind(), agent.FeatureHistory) {
				continue
			}
			if yes != has(a) {
				t.Errorf("%s: %s declared %v, but its interface implemented is %v", a.Kind(), f, agent.FeatureOf(a.Kind(), f).Is, has(a))
			}
		}
	}
}

// Every feature an adapter declares is one rush knows, and has a label.
func TestFeaturesKnown(t *testing.T) {
	known := map[agent.Feature]bool{}
	for _, i := range agent.AllFeatures() {
		if i.Label == "" || known[i.Feature] {
			t.Errorf("%s: no label, or listed twice", i.Feature)
		}
		known[i.Feature] = true
	}
	for _, a := range agent.All() {
		for f := range a.Features() {
			if !known[f] {
				t.Errorf("%s declares %q, which isn't in AllFeatures", a.Kind(), f)
			}
		}
	}
}

// eventBacked are features no path of rush's asks for: they happen when
// the agent does them, and what shows them waits for it.
var eventBacked = map[agent.Feature]string{
	agent.FeatureQuestions: "the question sheet opens when the agent asks one",
	agent.FeatureRemote:    "remote sessions are listed as the Discoverer returns them",
}

// Every feature gates something: a command, a screen or a path outside
// the adapters asks agent.Supports for it, or it is an interface (or an
// event) the adapter has exactly when it has the feature.
func TestEveryFeatureGated(t *testing.T) {
	asked := map[string]bool{}
	fset := token.NewFileSet()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root || strings.HasSuffix(slash, "internal/adapters") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(slash, "internal/agent/feature.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		inAgent := f.Name.Name == "agent" // agent's own gates name features bare
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && inAgent && strings.HasPrefix(id.Name, "Feature") {
				asked[id.Name] = true
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "agent" && strings.HasPrefix(sel.Sel.Name, "Feature") {
					asked[sel.Sel.Name] = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	consts := featureConsts(t, fset, filepath.Join(root, "internal", "agent", "feature.go"))
	for _, i := range agent.AllFeatures() {
		name := consts[i.Feature]
		_, iface := implements[i.Feature]
		_, event := eventBacked[i.Feature]
		if !asked[name] && !iface && !event {
			t.Errorf("%s (%s) gates nothing: ask agent.Supports for it where it's used, or list it as an interface or an event", i.Feature, name)
		}
	}
}

// featureConsts are the names of the Feature constants, by value.
func featureConsts(t *testing.T, fset *token.FileSet, path string) map[agent.Feature]string {
	t.Helper()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := map[agent.Feature]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 || !strings.HasPrefix(vs.Names[0].Name, "Feature") {
			return true
		}
		if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out[agent.Feature(strings.Trim(lit.Value, `"`))] = vs.Names[0].Name
		}
		return true
	})
	return out
}

// Each agent's level is as far as it has been tried.
func TestLevels(t *testing.T) {
	want := map[agent.Kind]agent.Level{
		"claude": agent.LevelFull, "codex": agent.LevelTested, "copilot": agent.LevelTested,
		"deepseek": agent.LevelPreview, "glm": agent.LevelPreview, "kimi": agent.LevelTested,
		"vibe": agent.LevelPreview, "gemini": agent.LevelPreview, "opencode": agent.LevelPreview,
	}
	for k, l := range want {
		if got := agent.LevelOf(k); got != l {
			t.Errorf("%s is %s, want %s", k, got, l)
		}
	}
}

// An empty kind, read from what an older rush wrote, is Claude Code's;
// the agents' programs are told from others.
func TestLegacyKindAndPrograms(t *testing.T) {
	if k := agent.Migrated(""); k != "claude" || agent.Migrated("codex") != "codex" {
		t.Errorf("an empty kind is %q", k)
	}
	if _, ok := agent.Get(agent.LegacyKind); !ok {
		t.Error("the legacy kind names no agent")
	}
	if !agent.IsProgram("/opt/homebrew/bin/claude") || !agent.IsProgram("codex") || agent.IsProgram("zsh") {
		t.Error("agents' programs aren't told from others")
	}
}

// A rewind control must have both file restoration and transcript branching.
func TestRewindRequiresWorkingInterfaces(t *testing.T) {
	for _, a := range agent.All() {
		if !agent.Supports(a.Kind(), agent.FeatureRewind) {
			continue
		}
		if _, ok := a.(agent.Rewinder); !ok {
			t.Errorf("%s advertises rewind without file restoration", a.Kind())
		}
		if _, ok := a.(agent.Brancher); !ok {
			t.Errorf("%s advertises rewind without transcript branching", a.Kind())
		}
	}
}
