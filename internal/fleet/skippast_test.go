package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// pastAgent lists one past session and one running.
type pastAgent struct{ dir string }

func (pastAgent) Kind() agent.Kind   { return "pastfake" }
func (pastAgent) Name() string       { return "Past" }
func (pastAgent) Level() agent.Level { return agent.LevelPreview }
func (pastAgent) Features() map[agent.Feature]agent.Support {
	return map[agent.Feature]agent.Support{}
}
func (a pastAgent) Profiles() []agent.Profile {
	return []agent.Profile{{Kind: "pastfake", Name: "past", Dir: a.dir}}
}
func (pastAgent) Live(agent.Profile) []agent.Session {
	return []agent.Session{{ID: "11111111-live", Cwd: "/repo", UpdatedAt: time.Now()}}
}
func (pastAgent) Past(agent.Profile) []agent.Session {
	return []agent.Session{{ID: "22222222-past", Cwd: "/repo", UpdatedAt: time.Now().Add(-time.Hour)}}
}

// SkipPast leaves past sessions out of a reading, running ones in, and the
// next reading after it's turned off has them again.
func TestSkipPastLeavesPastSessionsOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUSH_HOME", home+"/rush")
	agent.Register(pastAgent{dir: home + "/past"})

	l := NewLoader(&state.Store{})
	l.Watch(time.Hour)
	count := func() (past, live int) {
		for _, a := range l.Load(true).Agents {
			// Other installed adapters may see live sessions through explicit home overrides.
			if a.Kind != "pastfake" {
				continue
			}
			switch {
			case a.Past:
				past++
			case a.Interactive:
				live++
			}
		}
		return past, live
	}
	l.SkipPast(true)
	if past, live := count(); past != 0 || live != 1 {
		t.Fatalf("with SkipPast: %d past, %d running; want 0, 1", past, live)
	}
	l.SkipPast(false)
	if past, live := count(); past != 1 || live != 1 {
		t.Fatalf("after: %d past, %d running; want 1, 1", past, live)
	}
}

// An agent hidden in the overlay is in no reading, however it's found, and
// the transcript behind it is left alone.
func TestHiddenAgentsAreInNoReading(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RUSH_HOME", home+"/rush")
	agent.Register(pastAgent{dir: home + "/past"})

	s := &state.Store{}
	l := NewLoader(s)
	keys := func() (out []string) {
		for _, a := range l.Load(true).Agents {
			if a.Kind == "pastfake" {
				out = append(out, a.Key)
			}
		}
		return out
	}
	all := keys()
	if len(all) != 2 {
		t.Fatalf("want a past and a live row, got %v", all)
	}
	s.Hide(all[0])
	if got := keys(); len(got) != 1 || got[0] == all[0] {
		t.Fatalf("hidden %s still listed: %v", all[0], got)
	}
}
