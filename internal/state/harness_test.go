package state

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// rider is provider pr in harness h, as agent kind.
type rider struct {
	fakeAgent
	pr string
	h  agent.Kind
}

func (r rider) Provider() string  { return r.pr }
func (r rider) Rides() agent.Kind { return r.h }
func (r rider) Kind() agent.Kind  { return r.kind }
func (r rider) Name() string      { return string(r.kind) }

func init() {
	agent.Register(rider{fakeAgent{kind: "pa-pb"}, "pa", "pb"})
	agent.Recheck()
}

// A provider runs in the harness its profile gives it, else the config's,
// else its own.
func TestRunsIn(t *testing.T) {
	c := Config{Profiles: []Profile{
		{Name: "own", Providers: []string{"pa", "pc"}},
		{Name: "rides", Providers: []string{"pa"}, RunsIn: map[string]string{"pa": "pb"}},
	}}
	own, _ := c.ProfileNamed("own")
	if !equal(own.Kinds(), []string{"pa", "pc"}) {
		t.Fatalf("own kinds %v", own.Kinds())
	}
	rides, _ := c.ProfileNamed("rides")
	if !equal(rides.Kinds(), []string{"pa-pb"}) || !rides.Has("pa-pb") || !rides.Has("pa") {
		t.Fatalf("rides kinds %v", rides.Kinds())
	}
	if p, ok := rides.Pick(nil); !ok || p.Kind != "pa-pb" {
		t.Fatalf("picked %+v", p)
	}
	c.SetRunsIn("pa", "pb")
	own, _ = c.ProfileNamed("own")
	if !equal(own.Kinds(), []string{"pa-pb", "pc"}) {
		t.Fatalf("with pa in pb everywhere, own kinds %v", own.Kinds())
	}
	if p, _ := c.ProfileNamed("pa"); !p.Builtin || !equal(p.Kinds(), []string{"pa-pb"}) {
		t.Fatalf("pa's own profile %+v", p)
	}
	c.SetRunsIn("pa", "pa") // its own harness: the default again
	if _, ok := c.RunsIn["pa"]; ok {
		t.Fatalf("RunsIn kept its default: %v", c.RunsIn)
	}
	// A rider's kind names its provider's profile in that harness.
	if p, ok := c.ProfileNamed("pa-pb"); !ok || !equal(p.Kinds(), []string{"pa-pb"}) {
		t.Fatalf("pa-pb's profile %+v", p)
	}
	// A profile saved doesn't keep the config's choice as its own.
	c.SetRunsIn("pa", "pb")
	own, _ = c.ProfileNamed("own")
	c.SetProfile("own", own)
	if len(c.Profiles[0].RunsIn) != 0 {
		t.Fatalf("saved %+v", c.Profiles[0])
	}
}
