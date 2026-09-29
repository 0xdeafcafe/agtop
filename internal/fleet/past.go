package fleet

import (
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// pastEvery is how often a past conversation's subagents are looked at
// again: none can start, so no oftener than its adapter lists it.
const pastEvery = 15 * time.Second

// pastKeys are past conversations' row keys, by session id, made once.
type pastKeys map[string]string

// branches are the conversations agtop sessions left behind when rewound:
// each session shows them itself.
func (l *Loader) branches(hosted []host.Info, claimed map[string]bool) {
	for _, info := range hosted {
		cfg, ok := l.memo(filepath.Join(host.Root(), info.ID, "config.json"), func() any {
			cfg, err := host.ReadConfig(info.ID)
			if err != nil {
				return nil
			}
			return cfg
		}).(host.Config)
		if !ok {
			continue
		}
		for _, b := range cfg.Branches {
			claimed[b.SessionID] = true
		}
	}
}

// pastAgents are an account's conversations nothing has open: a terminal
// session that has closed keeps its row, and ones from before agtop ran
// are there too. A message resumes one in agtop mode.
func (l *Loader) pastAgents(acct claude.Account, claimed, seen map[string]bool, now time.Time) []*Agent {
	ov := l.store.Overlay
	var out []*Agent
	d, ok := discoverer(acct.Profile().Kind)
	if !ok {
		return nil
	}
	if l.pastKeys == nil {
		l.pastKeys = pastKeys{}
	}
	past := d.Past(acct.Profile())
	for i := range past {
		s := &past[i]
		c, _ := s.Extra.(claude.Convo)
		sid := s.ID
		if len(sid) < 8 || claimed[sid] {
			continue
		}
		key, ok := l.pastKeys[sid]
		if !ok {
			key = state.Key(acct.Name, "i:"+sid[:8])
			l.pastKeys[sid] = key
		}
		f := pastFile{path: s.Transcript, mod: s.UpdatedAt, c: c}
		if seen[key] {
			continue
		}
		seen[key] = true
		_, done := ov.Done[key]
		in := pastIn{acct: acct, path: f.path, c: f.c, mod: f.mod, name: ov.Names[key], done: done, group: ov.Groups[key],
			spend: l.spendVer[key], recent: now.Sub(f.mod) < 24*time.Hour}
		in.repo, in.branch = l.gitFor(firstNonEmpty(l.spend[key].Dir, f.c.Cwd), now)
		if in.recent {
			in.subs = l.pastSubagents(key, f.path, now)
		}
		// Most rows are just as they were a second ago: the same row does.
		if r, ok := l.pastRows[key]; ok && r.in == in {
			out = append(out, r.a)
			continue
		}
		j := agent.Job{
			ID: sid[:8], Account: acct.Name, Name: f.c.Title, State: "stopped", Cwd: f.c.Cwd,
			SessionID: sid, CreatedAt: f.c.Started, UpdatedAt: f.mod, TranscriptPath: f.path,
		}
		a := &Agent{Job: j, Key: key, Acct: acct.Profile(), Kind: string(acct.Profile().Kind), DisplayName: f.c.Title, Past: true}
		if in.name != "" {
			a.DisplayName = in.name
		}
		a.Done, a.Group = done, in.group
		a.Repo, a.Branch = in.repo, in.branch
		a.Spend = l.spend[key]
		a.Subs = in.subs
		l.pastRows[key] = pastRow{in: in, a: a}
		out = append(out, a)
	}
	return out
}

// pastFile is one transcript as the adapter lists it.
type pastFile struct {
	path string
	mod  time.Time
	c    claude.Convo
}

// pastIn is everything a past conversation's row is made from.
type pastIn struct {
	acct                      claude.Account
	path                      string
	c                         claude.Convo
	mod                       time.Time
	name, group, repo, branch string
	done, recent              bool
	spend                     int // l.spendVer's
	subs                      agent.SubagentStats
}

type pastRow struct {
	in pastIn
	a  *Agent
}

// pastSubagents is subagents for a conversation nothing has open: none can
// start, so the folder is looked at again only as often as the listing,
// and its transcript isn't read for which are working: only one still
// writing is, its process gone or not.
func (l *Loader) pastSubagents(key, transcript string, now time.Time) agent.SubagentStats {
	if e, ok := l.subs[key]; ok && e.st.Direct+e.st.Nested == 0 && now.Sub(e.at) < pastEvery {
		return e.st
	}
	st := claude.ReadSubagentStats(transcript, now)
	l.subs[key] = subsEntry{st: st, at: now}
	return st
}
