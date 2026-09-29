package claude

import (
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
)

// Claude Code's sessions, as found on disk. Live lists its background jobs
// (Extra is their claude.Job) and then every session file whose process is
// alive (Extra is its claude.Session), jobs' own included: the row for a
// job takes its status from its session. Past lists every transcript in
// the profile's projects folder. What only a row needs (the daemon's
// workers, pins, PRs, processes) is left to whoever draws it.

// found keeps what was read, so a listing every second reads only what
// changed.
var found = struct {
	sync.Mutex
	jobs     map[string]jobRead     // by the job's folder
	sessions map[string]sessionRead // by its file
	past     map[string]pastListing // by projects folder
}{jobs: map[string]jobRead{}, sessions: map[string]sessionRead{}, past: map[string]pastListing{}}

type jobRead struct {
	j       claude.Job
	mod     time.Time
	checked time.Time
}

type sessionRead struct {
	mod  time.Time
	size int64
	s    claude.Session
	ok   bool
}

// pastEvery is how often a projects folder is listed again for
// conversations that ended or appeared since.
const pastEvery = 15 * time.Second

type pastListing struct {
	at       time.Time
	files    map[string]pastFile
	sessions []agent.Session
}

// pastFile is one transcript, read again only when it changes.
type pastFile struct {
	mod  time.Time
	size int64
	c    claude.Convo
	ok   bool
}

// Live is p's background jobs, then its sessions whose process is alive.
func (Adapter) Live(p agent.Profile) []agent.Session {
	acct := Account(p)
	found.Lock()
	defer found.Unlock()
	var out []agent.Session
	ids := claude.ListJobIDs(acct)
	jobs := make(map[string]bool, len(ids))
	for _, id := range ids {
		dir := filepath.Join(acct.JobsDir(), id)
		jobs[dir] = true
		j, ok := readJob(acct, id, dir)
		if !ok {
			continue
		}
		out = append(out, agent.Session{Kind: Kind, Profile: p, ID: j.SessionID, Name: j.Name, State: j.State,
			Detail: j.Detail, Needs: j.Needs, Cwd: j.Cwd, Transcript: j.TranscriptPath, Todos: j.TodoItems,
			Running: j.Running, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, Extra: j, Job: &j.Job})
	}
	for dir := range found.jobs {
		if strings.HasPrefix(dir, acct.JobsDir()) && !jobs[dir] {
			delete(found.jobs, dir)
		}
	}
	sessions := readSessions(acct)
	for i := range sessions {
		ss := &sessions[i]
		s := agent.Session{Kind: Kind, Profile: p, ID: ss.SessionID, Name: ss.Name, Cwd: ss.Cwd, PID: ss.PID,
			Transcript: filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(ss.Cwd), ss.SessionID+".jsonl"),
			CreatedAt:  ss.StartedAt(), UpdatedAt: ss.UpdatedAt(), Extra: *ss,
			JobID: ss.JobID, Interactive: ss.Kind == "interactive", Status: ss.Status}
		if ss.StatusMs > 0 {
			s.StatusAt = ss.StatusAt()
		}
		out = append(out, s)
	}
	return out
}

// readJob is job id, read again only when its file changed. A finished
// job's file rarely does: it's looked at every 10s, a live one's every
// time. Called with found held.
func readJob(acct claude.Account, id, dir string) (claude.Job, bool) {
	r, ok := found.jobs[dir]
	if ok && !r.j.Live() && r.j.InFlight == 0 && time.Since(r.checked) < 10*time.Second {
		return r.j, true
	}
	st, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		delete(found.jobs, dir)
		return claude.Job{}, false
	}
	now := time.Now()
	if ok && r.mod.Equal(st.ModTime()) {
		r.checked = now
		found.jobs[dir] = r
		return r.j, true
	}
	j, err := claude.LoadJob(acct, id)
	if err != nil {
		if ok {
			return r.j, true // mid-write; keep the last good read
		}
		return claude.Job{}, false
	}
	found.jobs[dir] = jobRead{j: j, mod: st.ModTime(), checked: now}
	return j, true
}

// codenames is each live session's name for the others (rush-8a, as
// ListAgents lists it) by session id, kept apart from found so the UI can
// read it without waiting on a listing. A session that ended keeps its
// name: messages sent to it earlier still name it.
var codenames atomic.Pointer[map[string]string]

// Codenames is what codenames holds. Don't change it.
func (Adapter) Codenames() map[string]string { return knownCodenames() }

func knownCodenames() map[string]string {
	if p := codenames.Load(); p != nil {
		return *p
	}
	return nil
}

// noteCodename keeps s's name in codenames, copying the map only when it
// changes. Called with found held, so only one writer runs at a time.
func noteCodename(s claude.Session) {
	cur := knownCodenames()
	if s.Name == "" || cur[s.SessionID] == s.Name {
		return
	}
	next := make(map[string]string, len(cur)+1)
	maps.Copy(next, cur)
	next[s.SessionID] = s.Name
	codenames.Store(&next)
}

// readSessions is acct's session files whose process is alive, parsing
// only the files that changed. Called with found held.
func readSessions(acct claude.Account) []claude.Session {
	dir := filepath.Join(acct.ConfigDir, "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []claude.Session
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		var mod time.Time
		size := int64(-1)
		if st, err := e.Info(); err == nil {
			mod, size = st.ModTime(), st.Size()
		}
		r, ok := found.sessions[path]
		if !ok || r.size != size || !r.mod.Equal(mod) {
			r = sessionRead{mod: mod, size: size}
			r.s, r.ok = claude.ReadSession(path)
			found.sessions[path] = r
			if r.ok {
				noteCodename(r.s)
			}
		}
		if r.ok && claude.Alive(r.s.PID) {
			out = append(out, r.s)
		}
	}
	return out
}

// Past is every conversation in p's projects folder, listed again at most
// every pastEvery. The list is shared: don't change it.
func (Adapter) Past(p agent.Profile) []agent.Session {
	acct := Account(p)
	root := acct.ProjectsDir()
	found.Lock()
	defer found.Unlock()
	ls, ok := found.past[root]
	now := time.Now()
	if ok && now.Sub(ls.at) < pastEvery {
		return ls.sessions
	}
	next := pastListing{at: now, files: make(map[string]pastFile, len(ls.files)), sessions: make([]agent.Session, 0, len(ls.sessions))}
	projects, _ := os.ReadDir(root)
	for _, pd := range projects {
		if !pd.IsDir() {
			continue
		}
		dir := filepath.Join(root, pd.Name())
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
			f, ok := ls.files[path]
			if !ok || !f.mod.Equal(st.ModTime()) || f.size != st.Size() {
				f = pastFile{mod: st.ModTime(), size: st.Size()}
				f.c, f.ok = claude.ReadConvo(path)
			}
			next.files[path] = f
			if f.ok {
				next.sessions = append(next.sessions, agent.Session{Kind: Kind, Profile: p, ID: f.c.SessionID, Name: f.c.Title,
					State: "stopped", Cwd: f.c.Cwd, Transcript: path, CreatedAt: f.c.Started, UpdatedAt: f.mod, Extra: f.c})
			}
		}
	}
	found.past[root] = next
	return next.sessions
}

// SessionCwd reads where the session pid runs works now, from the session
// file Claude Code keeps for it: entering a worktree moves it.
func (Adapter) SessionCwd(p agent.Profile, pid int) (sessionID, cwd string, ok bool) {
	acct := claude.AccountOf(p)
	if acct.ConfigDir == "" {
		acct = claude.DefaultAccount()
	}
	ss, ok := claude.ReadSession(filepath.Join(acct.ConfigDir, "sessions", strconv.Itoa(pid)+".json"))
	return ss.SessionID, ss.Cwd, ok
}

var (
	_ agent.Discoverer = Adapter{}
	_ agent.CwdReader  = Adapter{}
)
