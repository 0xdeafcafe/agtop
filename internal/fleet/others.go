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

// otherPast are the past sessions of every agent but Claude Code that can
// list them, less those a row already stands for. A message resumes one
// in agtop mode, with its agent.
func (l *Loader) otherPast(claimed, seen map[string]bool, now time.Time) []*Agent {
	if l.others == nil {
		l.others = map[string]othersListing{}
	}
	ov := l.store.Overlay
	var out []*Agent
	for _, a := range agent.All() {
		d, ok := a.(agent.Discoverer)
		if !ok || a.Kind() == "claude" {
			continue
		}
		for _, p := range a.Profiles() {
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
				j := claude.Job{ID: s.ID[:8], Account: p.Name, Name: s.Name, State: "stopped", Cwd: s.Cwd,
					SessionID: s.ID, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
				ag := &Agent{Job: j, Key: key, Acct: claude.Account{Name: p.Name, ConfigDir: p.Dir}, DisplayName: s.Name,
					Past: true, Kind: string(a.Kind()), History: s.Transcript}
				if n := ov.Names[key]; n != "" {
					ag.DisplayName = n
				}
				_, ag.Done = ov.Done[key]
				ag.Group = ov.Groups[key]
				ag.Repo, ag.Branch = l.gitFor(s.Cwd, now)
				out = append(out, ag)
			}
		}
	}
	return out
}
