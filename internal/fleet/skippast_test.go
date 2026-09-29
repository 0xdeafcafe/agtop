package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/state"
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
	t.Setenv("AGTOP_HOME", home+"/agtop")
	agent.Register(pastAgent{dir: home + "/past"})

	l := NewLoader(&state.Store{})
	l.Watch(time.Hour)
	count := func() (past, live int) {
		for _, a := range l.Load(true).Agents {
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
