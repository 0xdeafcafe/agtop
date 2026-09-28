package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func TestOtherAgentsAreMarked(t *testing.T) {
	m := &Model{}
	if b := m.badges(&fleet.Agent{Kind: "codex"}); !strings.Contains(b, "codex") {
		t.Errorf("a Codex session's badges = %q", b)
	}
	if b := m.badges(&fleet.Agent{Kind: "claude"}); b != "" {
		t.Errorf("a Claude Code session's badges = %q, want none", b)
	}
}

// installedAgent is an agent agtop can run, installed.
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

func TestWith(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m := &Model{store: &state.Store{}}
	d := &m.store.Config.Dispatch
	m.withAgent("nosuch")
	if d.Kind != "" {
		t.Errorf("an unknown agent was taken: %q", d.Kind)
	}
	m.withAgent("Installed")
	if d.Kind != "installed" {
		t.Errorf("#with installed = %q", d.Kind)
	}
	m.withAgent("claude")
	if d.Kind != "" {
		t.Errorf("#with claude = %q, want the default", d.Kind)
	}
}
