package state

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

type stubAdapter struct{ kind agent.Kind }

func (s stubAdapter) Kind() agent.Kind                        { return s.kind }
func (s stubAdapter) Name() string                            { return string(s.kind) }
func (stubAdapter) Features() map[agent.Feature]agent.Support { return nil }
func (stubAdapter) Level() agent.Level                        { return agent.LevelPreview }
func (stubAdapter) Profiles() []agent.Profile                 { return nil }

// A provider is a profile of its own, so one session can run on it beside
// the default's; a profile of that name still wins.
func TestProviderIsAProfile(t *testing.T) {
	agent.Register(stubAdapter{"stubprovider"})
	c := Config{Profiles: []Profile{{Name: "work", Providers: []string{"claude"}}}}
	p, ok := c.ProfileNamed("StubProvider")
	if !ok || p.Name != "stubprovider" || len(p.Providers) != 1 || p.Providers[0] != "stubprovider" {
		t.Fatalf("got %+v, %v", p, ok)
	}
	if got := c.ProfileFor("/tmp", "stubprovider"); got.Providers[0] != "stubprovider" {
		t.Errorf("explicit provider gave %+v", got)
	}
	if _, ok := c.ProfileNamed("nobody"); ok {
		t.Error("an unknown name made a profile")
	}
	c.Profiles = append(c.Profiles, Profile{Name: "stubprovider", Providers: []string{"claude", "stubprovider"}})
	if p, _ := c.ProfileNamed("stubprovider"); len(p.Providers) != 2 {
		t.Errorf("the profile named after it should win: %+v", p)
	}
}
