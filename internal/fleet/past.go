package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// pastEvery is how often an account's projects folder is listed again for
// conversations that ended or appeared since.
const pastEvery = 15 * time.Second

// pastListing is an account's transcripts as last listed.
type pastListing struct {
	at    time.Time
	files []pastFile
}

// pastFile is one transcript, read again only when it changes.
type pastFile struct {
	path string
	mod  time.Time
	size int64
	c    claude.Convo
	ok   bool
	key  string // its row's, made once
}

// transcripts lists every conversation in an account's projects folder.
func (l *Loader) transcripts(acct claude.Account, now time.Time) []pastFile {
	root := acct.ProjectsDir()
	ls, ok := l.past[root]
	if ok && now.Sub(ls.at) < pastEvery {
		return ls.files
	}
	prev := make(map[string]pastFile, len(ls.files))
	for _, f := range ls.files {
		prev[f.path] = f
	}
	ls = pastListing{at: now, files: make([]pastFile, 0, len(ls.files))}
	projects, _ := os.ReadDir(root)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(root, p.Name())
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			st, err := e.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, ok := prev[path]
			if !ok || !f.mod.Equal(st.ModTime()) || f.size != st.Size() {
				f = pastFile{path: path, mod: st.ModTime(), size: st.Size()}
				f.c, f.ok = claude.ReadConvo(path)
			}
			ls.files = append(ls.files, f)
		}
	}
	l.past[root] = ls
	return ls.files
}

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
	files := l.transcripts(acct, now)
	for i := range files {
		f := &files[i]
		sid := f.c.SessionID
		if !f.ok || len(sid) < 8 || claimed[sid] {
			continue
		}
		if f.key == "" {
			f.key = state.Key(acct.Name, "i:"+sid[:8])
		}
		key := f.key
		if seen[key] {
			continue
		}
		seen[key] = true
		_, done := ov.Done[key]
		in := pastIn{acct: acct, path: f.path, c: f.c, mod: f.mod, name: ov.Names[key], done: done, group: ov.Groups[key],
			spend: l.spendVer[key], recent: now.Sub(f.mod) < 24*time.Hour}
		in.repo, in.branch = l.gitFor(f.c.Cwd, now)
		if in.recent {
			in.subs = l.pastSubagents(key, f.path, now)
		}
		// Most rows are just as they were a second ago: the same row does.
		if r, ok := l.pastRows[key]; ok && r.in == in {
			out = append(out, r.a)
			continue
		}
		j := claude.Job{
			ID: sid[:8], Account: acct.Name, Name: f.c.Title, State: "stopped", Cwd: f.c.Cwd,
			SessionID: sid, CreatedAt: f.c.Started, UpdatedAt: f.mod, TranscriptPath: f.path,
		}
		a := &Agent{Job: j, Key: key, Acct: acct, DisplayName: f.c.Title, Past: true}
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

// pastIn is everything a past conversation's row is made from.
type pastIn struct {
	acct                      claude.Account
	path                      string
	c                         claude.Convo
	mod                       time.Time
	name, group, repo, branch string
	done, recent              bool
	spend                     int // l.spendVer's
	subs                      claude.SubagentStats
}

type pastRow struct {
	in pastIn
	a  *Agent
}

// pastSubagents is subagents for a conversation nothing has open: none can
// start, so the folder is looked at again only as often as the listing.
func (l *Loader) pastSubagents(key, transcript string, now time.Time) claude.SubagentStats {
	if e, ok := l.subs[key]; ok && e.st.Direct+e.st.Nested == 0 && now.Sub(e.at) < pastEvery {
		return e.st
	}
	st := l.subagents(key, transcript, now)
	if e, ok := l.subs[key]; !ok || e.dir.IsZero() {
		l.subs[key] = subsEntry{st: st, at: now} // no subagents folder
	}
	return st
}
