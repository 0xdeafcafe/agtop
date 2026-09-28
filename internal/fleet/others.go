package fleet

import (
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// othersEvery is how often other agents' past sessions are listed again:
// listing reads the head of every one.
const othersEvery = time.Minute

// othersListing is one profile's past sessions as last listed.
type othersListing struct {
	at       time.Time
	sessions []agent.Session
}

// otherAgents are the sessions of every agent that can list them (the
// built-in agent's are found by Load itself), less those a row already
// stands for: running ones (a codex in a terminal), which agtop can only
// show, and past ones, which a message resumes in agtop mode with their
// agent.
func (l *Loader) otherAgents(claimed, seen map[string]bool, now time.Time) []*Agent {
	if l.others == nil {
		l.others = map[string]othersListing{}
	}
	var out []*Agent
	for _, a := range agent.All() {
		d, ok := a.(agent.Discoverer)
		if !ok || agent.IsBuiltin(a.Kind()) {
			continue
		}
		for _, p := range a.Profiles() {
			for _, s := range d.Live(p) {
				if len(s.ID) < 8 || claimed[s.ID] {
					continue
				}
				claimed[s.ID] = true
				key := state.Key(p.Name, "i:"+s.ID[:8])
				seen[key] = true
				ag := l.otherRow(a, p, s, key, now)
				ag.State, ag.Interactive = s.State, true
				out = append(out, ag)
			}
			ls, ok := l.others[p.Dir]
			if !ok || now.Sub(ls.at) >= othersEvery {
				ls = othersListing{at: now, sessions: d.Past(p)}
				l.others[p.Dir] = ls
			}
			for _, s := range ls.sessions {
				if len(s.ID) < 8 || claimed[s.ID] {
					continue
				}
				key := state.Key(p.Name, "i:"+s.ID[:8])
				if seen[key] {
					continue
				}
				seen[key] = true
				ag := l.otherRow(a, p, s, key, now)
				// One on the agent's servers stays there: agtop only shows it.
				ag.Past, ag.Interactive = !s.Remote, s.Remote
				if s.Remote {
					ag.State = s.State
				}
				out = append(out, ag)
			}
		}
	}
	return out
}

// otherRow is another agent's session as a row, with what you've set on it.
func (l *Loader) otherRow(a agent.Adapter, p agent.Profile, s agent.Session, key string, now time.Time) *Agent {
	ov := l.store.Overlay
	j := agent.Job{ID: s.ID[:8], Account: p.Name, Name: s.Name, State: "stopped", Cwd: s.Cwd,
		SessionID: s.ID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	ag := &Agent{Job: j, Key: key, Acct: claude.Account{Name: p.Name, ConfigDir: p.Dir}, DisplayName: s.Name,
		Kind: string(a.Kind()), History: s.Transcript, Remote: s.Remote, PRs: s.PRs}
	ag.Detail, ag.Needs = s.Detail, s.Needs
	if n := ov.Names[key]; n != "" {
		ag.DisplayName = n
	}
	_, ag.Done = ov.Done[key]
	ag.Group = ov.Groups[key]
	if s.Remote {
		ag.Repo = s.Repo
	} else {
		ag.Repo, ag.Branch = l.gitFor(s.Cwd, now)
	}
	return ag
}
