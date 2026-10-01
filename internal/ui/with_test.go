package ui

import (
	"context"
	"strings"
	"testing"

	_ "github.com/0xdeafcafe/rush/internal/adapters/gemini"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

func TestEveryHarnessIsNamed(t *testing.T) {
	m := &Model{}
	for kind, want := range map[string]string{"claude": "Claude Code", "": "Claude Code", "codex": "Codex", "kimi": "Kimi", "vibe": "Vibe", "gemini": "Gemini", "ollama": "Claude Code · Ollama"} {
		if b := m.badges(&fleet.Agent{Kind: kind}, true); !strings.Contains(b, want) {
			t.Errorf("%s badge=%q, want %s", kind, b, want)
		}
	}
}

func TestLongAgentTitlePreservesHarnessIdentity(t *testing.T) {
	m, _ := benchModel(180, 40)
	a := m.snap.Agents[0]
	a.DisplayName = strings.Repeat("long title ", 30)
	a.Kind = "claude"
	m.host.sess.Info.Kind = "kimi"
	for _, stacked := range []bool{false, true} {
		got := m.agentLine(a, 100, 100, true, 60, stacked, "")
		if !strings.Contains(got, "Kimi") || strings.Contains(got, "Claude Code") {
			t.Fatalf("identity lost or stale (stacked=%v): %s", stacked, got)
		}
	}
}

// installedAgent is an agent rush can run, installed.
type installedAgent struct{}

func init() { agent.Register(installedAgent{}) }

func (installedAgent) Kind() agent.Kind                          { return "installed" }
func (installedAgent) Name() string                              { return "Installed" }
func (installedAgent) Features() map[agent.Feature]agent.Support { return nil }
func (installedAgent) Level() agent.Level                        { return agent.LevelPreview }
func (installedAgent) Profiles() []agent.Profile {
	return []agent.Profile{{Kind: "installed", Name: "installed", Dir: "/x/.installed"}}
}
func (installedAgent) Start(context.Context, agent.StartOptions) (agent.Conn, error) { return nil, nil }

// #with is #new without a task: the next session, once, and never a
// default.
func TestWith(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(120, 40)
	before := m.store.Config.DefaultAgent()
	m.withAgent("nosuch@nowhere")
	if m.startOver != nil {
		t.Errorf("an unknown route was taken: %+v", m.startOver)
	}
	m.withAgent("codex@openai-sub")
	if m.startOver == nil || m.startOver.kind != "codex" {
		t.Fatalf("#with codex@openai-sub = %+v", m.startOver)
	}
	if m.store.Config.DefaultAgent() != before {
		t.Errorf("#with changed the default to %s", m.store.Config.DefaultAgent())
	}
}
