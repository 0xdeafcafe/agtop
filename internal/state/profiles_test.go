package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// fakeAgent is a provider for the resolver: installed unless missing.
type fakeAgent struct {
	agent.Adapter
	kind    agent.Kind
	missing bool
}

func (f fakeAgent) Kind() agent.Kind { return f.kind }
func (f fakeAgent) Name() string     { return string(f.kind) }

type missingAgent struct{ fakeAgent }

func (missingAgent) Program() (string, []string) { return "agtop-no-such-program", nil }

func init() {
	for _, k := range []agent.Kind{"claude", "pa", "pb", "pc"} {
		agent.Register(fakeAgent{kind: k})
	}
	agent.Register(missingAgent{fakeAgent{kind: "gone"}})
	agent.Recheck()
}

// A session's profile is the one picked for it, else its folder's longest
// rule, else the default.
func TestProfileFor(t *testing.T) {
	home, _ := os.UserHomeDir()
	c := Config{
		Profiles: []Profile{
			{Name: "Default", Providers: []string{"claude"}},
			{Name: "work", Providers: []string{"pa"}},
			{Name: "deep", Providers: []string{"pb"}},
		},
		DefaultProfile: "Default",
		FolderRules: []FolderRule{
			{Path: "~/src", Profile: "work"},
			{Path: "~/src/deep", Profile: "deep"},
			{Path: "/tmp/gone", Profile: "nobody"},
		},
	}
	for _, tc := range []struct{ cwd, explicit, want string }{
		{"/elsewhere", "", "Default"},
		{filepath.Join(home, "src"), "", "work"},
		{filepath.Join(home, "src", "x"), "", "work"},
		{filepath.Join(home, "src", "deep", "y"), "", "deep"},
		{filepath.Join(home, "src", "deeper"), "", "work"}, // a prefix of a name isn't a folder
		{filepath.Join(home, "src", "deep"), "WORK", "work"},
		{filepath.Join(home, "src"), "nope", "work"},
		{"/tmp/gone/a", "", "Default"}, // a rule naming no profile
	} {
		if got := c.ProfileFor(tc.cwd, tc.explicit).Name; got != tc.want {
			t.Errorf("ProfileFor(%q, %q) = %q, want %q", tc.cwd, tc.explicit, got, tc.want)
		}
	}
}

// The picker starts on the first installed provider while an account of it
// has room, and moves on only when the profile mixes.
func TestPick(t *testing.T) {
	out := Room{"pa": {{ID: "a1", Current: true, Out: true, Used: 99}, {ID: "a2", Out: true, Used: 97}}}
	stay := Profile{Name: "s", Providers: []string{"gone", "pa", "pb"}}
	p, ok := stay.Pick(Room{"pa": {{ID: "a1", Current: true, Used: 50}, {ID: "a2", Used: 10}}})
	if !ok || p.Kind != "pa" || p.Account.ID != "a1" || p.Wait {
		t.Fatalf("with room, picked %+v; want the account in use", p)
	}
	p, _ = stay.Pick(Room{"pa": {{ID: "a1", Current: true, Out: true}, {ID: "a2", Used: 40}, {ID: "a3", Used: 20}}})
	if p.Kind != "pa" || p.Account.ID != "a3" {
		t.Fatalf("with the one in use out, picked %+v; want the roomiest", p)
	}
	p, _ = stay.Pick(out)
	if p.Kind != "pa" || !p.Wait || p.Account.ID != "a1" {
		t.Fatalf("staying, all out, picked %+v; want to wait on pa", p)
	}
	mix := stay
	mix.Mix = MixMix
	p, _ = mix.Pick(out)
	if p.Kind != "pb" || p.Wait {
		t.Fatalf("mixing, all out, picked %+v; want pb", p)
	}
	if _, ok := (Profile{Providers: []string{"gone"}}).Pick(nil); ok {
		t.Fatal("picked a provider that isn't installed")
	}
	if n, ok := mix.Next("pa", out); !ok || n.Kind != "pb" {
		t.Fatalf("next after pa is %+v", n)
	}
	if _, ok := mix.Next("pb", out); ok {
		t.Fatal("found a provider after the last")
	}
}

// PickFor gives an account of one provider, or says there's none.
func TestPickFor(t *testing.T) {
	p := Profile{Providers: []string{"pa", "claude", "gone"}}
	if got, ok := p.PickFor("claude", nil); !ok || got.Kind != "claude" {
		t.Fatalf("PickFor(claude) = %+v, %v", got, ok)
	}
	if _, ok := p.PickFor("pb", nil); ok {
		t.Fatal("picked a provider the profile doesn't list")
	}
	if _, ok := p.PickFor("gone", nil); ok {
		t.Fatal("picked a provider that isn't installed")
	}
	if got, ok := p.PickFor("pa", Room{"pa": {{ID: "x", Current: true, Out: true}}}); ok || !got.Wait {
		t.Fatalf("picked %+v, %v with every account out", got, ok)
	}
}

// An older config's default agent, order and choice when nearly out
// become the Default profile, once; the old fields are still written.
func TestMigrateProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGTOP_HOME", dir)
	path := filepath.Join(dir, "config.json")
	old := `{"dispatch":{"kind":"pa"},"switchOnLimit":"agent","agentOrder":["pb","pa"]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load()
	c := &s.Config
	d := c.Default()
	if c.DefaultProfile != "Default" || len(c.Profiles) != 1 || d.Mix != MixMix || d.Limit() != LimitAccount {
		t.Fatalf("migrated to %+v", c.Profiles)
	}
	if want := []string{"pa", "pb", "claude", "gone", "pc"}; !equal(d.Providers, want) {
		t.Fatalf("providers %v, want %v", d.Providers, want)
	}
	// Changed here: written back where older agtops read it.
	d.Mix, d.OnLimit, d.Providers = "", LimitWait, []string{"claude", "pb"}
	c.SetProfile(d.Name, d)
	c.SetProfile("", Profile{Name: "work", Providers: []string{"pc"}})
	c.SetRule("~/work", "work")
	if err := s.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Dispatch       Dispatch
		SwitchOnLimit  string
		StayOnAccount  bool
		AgentOrder     []string
		Profiles       []Profile
		DefaultProfile string
	}
	b, _ := os.ReadFile(path)
	if err := jsonx.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Dispatch.Kind != "claude" || raw.SwitchOnLimit != OnLimitOff || !raw.StayOnAccount || !equal(raw.AgentOrder, []string{"claude", "pb"}) || len(raw.Profiles) != 2 {
		t.Fatalf("saved %s", b)
	}
	// Loaded again, nothing is made twice.
	s = Load()
	if len(s.Config.Profiles) != 2 || s.Config.Default().Limit() != LimitWait {
		t.Fatalf("loaded again as %+v", s.Config.Profiles)
	}
	// Renaming keeps rules and the default on it; deleting drops its rules.
	s.Config.SetProfile("default", Profile{Name: "Home", Providers: []string{"pb"}})
	s.Config.SetProfile("work", Profile{Name: "Job", Providers: []string{"pc"}})
	if s.Config.DefaultProfile != "Home" || s.Config.FolderRules[0].Profile != "Job" || s.Config.Dispatch.Kind != "pb" {
		t.Fatalf("after renaming: %+v", s.Config)
	}
	if !s.Config.DeleteProfile("job") || len(s.Config.FolderRules) != 0 || s.Config.DeleteProfile("home") {
		t.Fatalf("after deleting: %+v", s.Config)
	}
	s.Config.SetDefaultProvider("pa")
	if got := s.Config.Default().Providers; !equal(got, []string{"pa", "pb"}) {
		t.Fatalf("default provider set to %v", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
