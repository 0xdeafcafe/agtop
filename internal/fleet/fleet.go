// Package fleet folds every source (jobs, roster, pins, transcripts, the
// process table, plan usage) into one snapshot the UI renders.
package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/proc"
	"github.com/0xdeafcafe/agtop/internal/state"
)

type Agent struct {
	claude.Job
	Key         string
	Acct        claude.Account
	DisplayName string
	Pinned      bool
	Done        bool
	Group       string
	Worker      *claude.Worker
	Repo        string
	Branch      string
	Mem         uint64
	CPU         float64
	Procs       int
	Spend       Spend
	PRs         []claude.PR
	Interactive bool
	// Headless is an interactive-kind session that is really `claude -p`
	// driven by some other program: it can't be replied to at all.
	Headless bool
	PID      int  // root of the process tree
	Checking bool // turn just ended; Claude Code has not classified it yet
	Subs     claude.SubagentStats
	Seen     bool // the user has opened or answered this question already
	// Agtop is a session agtop runs itself, headless, through a host
	// process; its pane is the conversation rather than Claude Code's screen.
	Agtop bool
	// Temp is how much disk its temp work takes, as last measured.
	Temp int64
}

// NeedsYou is a live agent asking something the user has not looked at yet.
func (a *Agent) NeedsYou() bool {
	return a.State == "blocked" && !a.Checking && a.PID != 0 && !a.Seen
}

// Waiting is a question the user has seen and left for later.
func (a *Agent) Waiting() bool {
	return a.State == "blocked" && !a.Checking && a.PID != 0 && a.Seen
}

// applyStatus trusts the session's live busy/idle flag over the job file,
// whose state Claude Code only re-summarises every 15-40s.
func (a *Agent) applyStatus(ss claude.Session) {
	switch {
	case ss.Status == "busy" && a.State == "running":
		a.State = "working"
	case ss.Status == "busy" && a.State == "blocked":
		a.State, a.Needs, a.Detail = "working", "", ""
	case ss.Status == "busy" && a.State == "done" && len(a.Background) == 0:
		a.State, a.Detail = "working", ""
	case ss.Status == "idle" && a.State == "working" && ss.StatusMs > 0 && ss.StatusAt().After(a.UpdatedAt):
		a.State, a.Checking, a.Needs = "blocked", true, ""
	}
}

// Nudge marks agents the user just sent something to as working until their
// own files catch up.
func (l *Loader) Nudge(key string) { l.nudged[key] = time.Now() }

// JustFinished is a turn that ended moments ago; it lingers in Working so a
// finish is noticed rather than vanishing into history.
func (a *Agent) JustFinished(now time.Time) bool {
	return a.State == "done" && !a.Busy() && now.Sub(a.UpdatedAt) < 2*time.Minute
}

// Busy is a finished turn whose background work is still running in a live
// process; a job file can claim work long after its process has gone.
func (a *Agent) Busy() bool { return a.Job.Busy() && a.PID != 0 }

// Age is what the native view prints on the right: time since last change.
func (a *Agent) Age(now time.Time) time.Duration {
	t := a.UpdatedAt
	if a.ModTime.After(t) {
		t = a.ModTime
	}
	return now.Sub(t)
}

// Elapsed is how long the agent has existed, frozen once it stops.
func (a *Agent) Elapsed(now time.Time) time.Duration {
	end := now
	if !a.Live() {
		end = a.UpdatedAt
		if !a.Spend.Last.IsZero() && a.Spend.Last.Before(end) {
			end = a.Spend.Last
		}
	}
	d := end.Sub(a.CreatedAt)
	if d < 0 {
		return 0
	}
	return d
}

type Spend struct {
	Cost  float64
	Usage claude.TokenUsage
	Model string
	First time.Time
	Last  time.Time
	PRs   []string
	Dirs  []string // folders the agent worked in, subagents included
	Today float64
	Ready bool
}

type AccountView struct {
	claude.Account
	Usage   claude.Usage
	Daemon  bool
	Live    int
	Agents  int
	Spend   float64
	Today   float64
	Current bool
}

type Role int

const (
	RoleOther Role = iota
	RoleDaemon
	RoleView
	RoleWorker
	RoleSpare
	RoleOrphan
)

type ProcRow struct {
	PID   int
	Role  Role
	Label string
	Cmd   string
	Mem   uint64
	CPU   float64
	Procs int
	Start time.Time
	Key   string // the agent a worker belongs to
}

type Machine struct {
	Rows     []ProcRow
	TotalMem uint64
	TotalCPU float64
	Spares   int
	SpareMem uint64
	// Orphans are processes whose session has ended; they run until the
	// user keeps or kills them.
	Orphans   int
	OrphanMem uint64
}

type Snapshot struct {
	At       time.Time
	Agents   []*Agent
	Accounts []AccountView
	Machine  Machine
	Table    *proc.Table
}

// Loader keeps the cheap caches between refreshes.
type Loader struct {
	store   *state.Store
	jobs    map[string]claude.Job // by key, reloaded on mtime change
	mtimes  map[string]time.Time
	checked map[string]time.Time // when each job's file was last looked at
	args    map[int]argsEntry
	git     map[string]gitInfo
	usage   map[string]usageEntry
	prevTab *proc.Table
	spend   map[string]Spend
	nudged  map[string]time.Time
	subs    map[string]subsEntry
	fetched map[string]claude.Usage
	files   map[string]fileMemo
	hosts   host.Lister
	print   map[int]printEntry
	Temp    *TempSizes
}

// printEntry remembers whether a pid runs claude -p, by its start time.
type printEntry struct {
	start time.Time
	print bool
}

// SetFetched stores a usage reading fetched from Anthropic for an account.
func (l *Loader) SetFetched(configDir string, u claude.Usage) {
	if old, ok := l.fetched[configDir]; ok && u.FetchedAt.IsZero() {
		old.Problem = u.Problem // keep the last good numbers, note why they're not refreshing
		l.fetched[configDir] = old
		return
	}
	l.fetched[configDir] = u
}

type subsEntry struct {
	st  claude.SubagentStats
	at  time.Time
	dir time.Time // the subagents folder's time when counted
}

// subagents counts an agent's subagent runs, and those still working.
// Counting reads the folder and every run's time, so it's done again only
// when a run started (the folder changed), when some were working (they
// may have stopped), or after 30s.
func (l *Loader) subagents(key, transcript string, now time.Time) claude.SubagentStats {
	var dir time.Time
	if st, err := os.Stat(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")); err == nil {
		dir = st.ModTime()
	} else {
		return claude.SubagentStats{}
	}
	e, ok := l.subs[key]
	working := e.st.Direct+e.st.Nested > 0
	if ok && e.dir.Equal(dir) && now.Sub(e.at) < 30*time.Second && (!working || now.Sub(e.at) < 3*time.Second) {
		return e.st
	}
	e = subsEntry{st: claude.ReadSubagentStats(transcript, now), at: now, dir: dir}
	l.subs[key] = e
	return e.st
}

// sessions lists an account's live Claude Code sessions, parsing only the
// session files that changed.
func (l *Loader) sessions(acct claude.Account) []claude.Session {
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
		p := filepath.Join(dir, e.Name())
		ss, ok := l.memo(p, func() any {
			s, ok := claude.ReadSession(p)
			if !ok {
				return nil
			}
			return s
		}).(claude.Session)
		if ok && claude.Alive(ss.PID) {
			out = append(out, ss)
		}
	}
	return out
}

// isPrint reports whether pid is claude -p, reading its arguments once.
func (l *Loader) isPrint(tab *proc.Table, pid int) bool {
	var start time.Time
	if tab != nil {
		if p := tab.Procs[pid]; p != nil {
			start = p.Start
		}
	}
	if e, ok := l.print[pid]; ok && !start.IsZero() && e.start.Equal(start) {
		return e.print
	}
	v := isPrint(proc.Args(pid))
	if !start.IsZero() {
		l.print[pid] = printEntry{start: start, print: v}
	}
	return v
}

type argsEntry struct {
	start time.Time
	cmd   string
}

type gitInfo struct {
	repo, branch string
	at           time.Time
}

type usageEntry struct {
	u   claude.Usage
	mod time.Time
}

func NewLoader(s *state.Store) *Loader {
	return &Loader{
		store: s, jobs: map[string]claude.Job{}, mtimes: map[string]time.Time{},
		args: map[int]argsEntry{}, git: map[string]gitInfo{}, usage: map[string]usageEntry{},
		spend: map[string]Spend{}, nudged: map[string]time.Time{}, subs: map[string]subsEntry{}, fetched: map[string]claude.Usage{},
		files: map[string]fileMemo{}, print: map[int]printEntry{}, checked: map[string]time.Time{},
		Temp: LoadTempSizes(),
	}
}

// SetSpend receives cost totals from the background scanner.
func (l *Loader) SetSpend(m map[string]Spend) {
	for k, v := range m {
		l.spend[k] = v
	}
}

func (l *Loader) Load(sampleProcs bool) *Snapshot {
	now := time.Now()
	snap := &Snapshot{At: now}
	cfg := l.store.Config
	ov := l.store.Overlay
	active := cfg.ActiveAccount()

	var tab *proc.Table
	if sampleProcs {
		tab = proc.Snapshot(l.prevTab)
	}

	seen := map[string]bool{}
	hosted := l.hosts.List()
	// Claude Code processes agtop's own hosts run: they register as
	// sessions too, but they're the agtop agents, not agents of their own.
	ours := map[string]bool{}
	oursPID := map[int]bool{}
	for _, info := range hosted {
		ours[info.SessionID] = true
		if info.ClaudePID != 0 {
			oursPID[info.ClaudePID] = true
		}
	}
	for _, acct := range cfg.AllAccounts() {
		roster := l.memo(acct.RosterPath(), func() any { return claude.ReadRoster(acct) }).(claude.Roster)
		prs := l.memo(acct.PRCachePath(), func() any { return claude.ReadPRCache(acct) }).(map[string]claude.PR)
		pins := l.memo(claude.PinsPath(acct), func() any {
			pins := map[string]int{}
			for i, id := range claude.ReadPins(acct) {
				pins[id] = i + 1
			}
			return pins
		}).(map[string]int)
		av := AccountView{Account: acct, Daemon: daemon.Client{Account: acct}.Running(), Current: acct.Name == active.Name}
		av.Usage = l.readUsage(acct)
		sessions := l.sessions(acct)
		byJob := map[string]claude.Session{}
		for _, ss := range sessions {
			if ss.JobID != "" {
				byJob[ss.JobID] = ss
			}
		}
		for _, id := range claude.ListJobIDs(acct) {
			key := state.Key(acct.Name, id)
			seen[key] = true
			j, ok := l.job(acct, id, key)
			if !ok {
				continue
			}
			a := &Agent{Job: j, Key: key, Acct: acct, DisplayName: j.Name}
			if ss, ok := byJob[id]; ok {
				a.applyStatus(ss)
			}
			if a.State == "running" { // only a busy session makes it work
				a.State = "done"
			}
			if t, ok := l.nudged[key]; ok {
				switch {
				case now.Sub(t) > 20*time.Second || j.UpdatedAt.After(t) && j.State == "working":
					delete(l.nudged, key)
				case !a.Live():
					a.State, a.Needs, a.Checking, a.Detail = "working", "", false, ""
				}
			}
			if n := ov.Names[key]; n != "" {
				a.DisplayName = n
			}
			a.Pinned = pins[id] > 0
			_, a.Done = ov.Done[key]
			a.Group = ov.Groups[key]
			if t, ok := ov.Seen[key]; ok && !j.UpdatedAt.After(t) {
				a.Seen = true
			}
			if w, ok := roster.Workers[id]; ok {
				w := w
				a.Worker = &w
			}
			a.Repo, a.Branch = l.gitFor(j.Cwd, now)
			if j.WorktreeBranch != "" {
				a.Branch = j.WorktreeBranch
			}
			a.Spend = l.spend[key]
			if j.TranscriptPath != "" && (a.Live() || a.PID != 0 || now.Sub(j.UpdatedAt) < 24*time.Hour) {
				a.Subs = l.subagents(key, j.TranscriptPath, now)
			}
			for _, u := range a.Spend.PRs {
				// Only PRs Claude Code linked to a session; a URL merely
				// mentioned in a transcript is not this agent's PR.
				if pr, ok := prs[u]; ok {
					pr.URL = u
					a.PRs = append(a.PRs, pr)
				}
			}
			if a.Worker != nil {
				a.PID = a.Worker.PID
				// A roster left behind by a crashed daemon names pids that are
				// gone or reused; only a live claude process counts.
				if tab != nil && !isClaudePID(tab, a.PID) {
					a.PID, a.Worker = 0, nil
				}
			}
			if tab != nil && a.Live() && a.PID == 0 {
				a.State, a.Detail = "stopped", "lost its process"
			}
			l.sample(tab, a)
			if a.Live() {
				av.Live++
			}
			av.Agents++
			av.Spend += a.Spend.Cost
			av.Today += a.Spend.Today
			snap.Agents = append(snap.Agents, a)
		}
		for _, ss := range sessions {
			if ss.Kind != "interactive" || len(ss.SessionID) < 8 || tab != nil && !isClaudePID(tab, ss.PID) || ours[ss.SessionID] || oursPID[ss.PID] {
				continue
			}
			key := state.Key(acct.Name, "i:"+ss.SessionID[:8])
			seen[key] = true
			st := "idle"
			if ss.Status == "busy" || ss.Status == "shell" {
				st = "working"
			}
			j := claude.Job{
				ID: ss.SessionID[:8], Account: acct.Name, Name: ss.Name, State: st, Cwd: ss.Cwd,
				SessionID: ss.SessionID, CreatedAt: ss.StartedAt(), UpdatedAt: ss.UpdatedAt(),
				TranscriptPath: filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(ss.Cwd), ss.SessionID+".jsonl"),
			}
			headless := l.isPrint(tab, ss.PID)
			if st == "idle" {
				j.Detail = "open in a terminal"
				if headless {
					j.Detail = "run by another program"
				}
			}
			a := &Agent{Job: j, Key: key, Acct: acct, DisplayName: ss.Name, Interactive: true, Headless: headless, PID: ss.PID}
			if n := ov.Names[key]; n != "" {
				a.DisplayName = n
			}
			_, a.Done = ov.Done[key]
			a.Group = ov.Groups[key]
			a.Repo, a.Branch = l.gitFor(ss.Cwd, now)
			a.Spend = l.spend[key]
			l.sample(tab, a)
			if a.Live() {
				av.Live++
			}
			av.Agents++
			av.Spend += a.Spend.Cost
			av.Today += a.Spend.Today
			snap.Agents = append(snap.Agents, a)
		}
		for _, info := range hosted {
			if info.Account != acct.Name && !(info.Account == "" && acct.IsDefault()) {
				continue
			}
			a := l.hosted(acct, info, tab, now)
			if n := ov.Names[a.Key]; n != "" {
				a.DisplayName = n
			}
			_, a.Done = ov.Done[a.Key]
			a.Group = ov.Groups[a.Key]
			if t, ok := ov.Seen[a.Key]; ok && !info.UpdatedAt.After(t) {
				a.Seen = true
			}
			a.Spend = l.spend[a.Key]
			if a.Spend.Cost < info.CostUSD {
				a.Spend.Cost = info.CostUSD
			}
			if a.Live() {
				av.Live++
			}
			av.Agents++
			av.Spend += a.Spend.Cost
			av.Today += a.Spend.Today
			snap.Agents = append(snap.Agents, a)
		}
		snap.Accounts = append(snap.Accounts, av)
	}
	for k := range l.jobs {
		if !seen[k] {
			delete(l.jobs, k)
			delete(l.mtimes, k)
			delete(l.checked, k)
		}
	}
	if tab != nil {
		snap.Machine = l.machine(tab, snap)
		l.prevTab = tab
		snap.Table = tab
	}
	for _, a := range snap.Agents {
		a.Temp = l.Temp.Sizes[a.Key].Bytes
	}
	sort.SliceStable(snap.Agents, func(i, j int) bool {
		return snap.Agents[i].Age(now) < snap.Agents[j].Age(now)
	})
	return snap
}

// hosted turns an agtop-mode session's info into an agent row.
func (l *Loader) hosted(acct claude.Account, info host.Info, tab *proc.Table, now time.Time) *Agent {
	st := info.State
	switch st {
	case "idle", "starting":
		st = "done"
	case "":
		st = "stopped"
	}
	name := info.Name
	if name == "" {
		name = info.Detail
	}
	if name == "" {
		name = "agtop session " + info.ID
	}
	j := claude.Job{
		ID: info.ID, Account: acct.Name, Name: name, State: st, Detail: info.Detail, Needs: info.Needs,
		Cwd: info.Cwd, SessionID: info.SessionID, CreatedAt: info.StartedAt, UpdatedAt: info.UpdatedAt,
		TranscriptPath: filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(info.Cwd), info.SessionID+".jsonl"),
	}
	switch {
	case info.Limit != nil:
		j.Detail = "usage limit"
		if !info.Limit.ResetsAt.IsZero() {
			j.Detail += " · resets " + info.Limit.ResetsAt.Local().Format("15:04")
		}
		if info.Limit.Ask {
			st, j.State, j.Needs = "blocked", "blocked", "continue when the limit resets?"
		}
	case info.Retry != nil && info.Retry.GaveUp:
		j.Detail = "API error · " + info.Retry.Why
	case info.Retry != nil:
		j.Detail = fmt.Sprintf("API error · retry %d of %d", info.Retry.Attempt, info.Retry.Max)
	case info.Error != "" && st == "done":
		j.Detail = "stopped mid-turn · your next message resumes it"
	}
	a := &Agent{Job: j, Key: state.Key(acct.Name, "a:"+info.ID), Acct: acct, DisplayName: name, Agtop: true}
	if info.State != "stopped" && info.HostPID > 0 && (tab == nil || tab.Procs[info.HostPID] != nil) {
		a.PID = info.HostPID
	}
	a.Repo, a.Branch = l.gitFor(info.Cwd, now)
	l.sample(tab, a)
	return a
}

func (l *Loader) sample(tab *proc.Table, a *Agent) {
	if tab == nil || a.PID == 0 {
		return
	}
	tab.Fill(l.prevTab, []int{a.PID})
	a.Mem, a.CPU, a.Procs = tab.Sum(a.PID, nil)
}

func (l *Loader) job(acct claude.Account, id, key string) (claude.Job, bool) {
	// A finished job's file rarely changes: it's looked at every 10s, a
	// live one's every time.
	if j, ok := l.jobs[key]; ok && !j.Live() && j.InFlight == 0 && time.Since(l.checked[key]) < 10*time.Second {
		return j, true
	}
	l.checked[key] = time.Now()
	st, err := os.Stat(filepath.Join(acct.JobsDir(), id, "state.json"))
	if err != nil {
		return claude.Job{}, false
	}
	if j, ok := l.jobs[key]; ok && l.mtimes[key].Equal(st.ModTime()) {
		return j, true
	}
	j, err := claude.LoadJob(acct, id)
	if err != nil {
		if old, ok := l.jobs[key]; ok {
			return old, true // mid-write; keep the last good read
		}
		return claude.Job{}, false
	}
	l.jobs[key], l.mtimes[key] = j, st.ModTime()
	return j, true
}

func (l *Loader) readUsage(acct claude.Account) claude.Usage {
	st, err := os.Stat(acct.StatePath())
	if err != nil {
		return l.freshest(acct, claude.Usage{})
	}
	if e, ok := l.usage[acct.ConfigDir]; ok && e.mod.Equal(st.ModTime()) {
		return l.freshest(acct, e.u)
	}
	u, _ := claude.ReadUsage(acct)
	l.usage[acct.ConfigDir] = usageEntry{u: u, mod: st.ModTime()}
	return l.freshest(acct, u)
}

// freshest prefers agtop's own fetch when it is newer than Claude Code's cache.
func (l *Loader) freshest(acct claude.Account, cached claude.Usage) claude.Usage {
	f, ok := l.fetched[acct.ConfigDir]
	if !ok {
		return cached
	}
	if f.FetchedAt.After(cached.FetchedAt) {
		f.Email, f.Org, f.Plan = cached.Email, cached.Org, cached.Plan
		f.Role, f.Billing, f.OrgType, f.Extra = cached.Role, cached.Billing, cached.OrgType, cached.Extra
		return f
	}
	cached.Problem = f.Problem
	return cached
}

// gitFor finds the repository and branch for a folder by reading .git
// directly; no git subprocess.
func (l *Loader) gitFor(dir string, now time.Time) (string, string) {
	if dir == "" {
		return "", ""
	}
	if g, ok := l.git[dir]; ok && now.Sub(g.at) < 30*time.Second {
		return g.repo, g.branch
	}
	var g gitInfo
	g.at = now
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		p := filepath.Join(d, ".git")
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		gitDir := p
		if !st.IsDir() {
			b, _ := os.ReadFile(p)
			s := strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:"))
			if !filepath.IsAbs(s) {
				s = filepath.Join(d, s)
			}
			gitDir = s
		}
		g.repo = d
		if b, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
			h := strings.TrimSpace(string(b))
			g.branch = strings.TrimPrefix(h, "ref: refs/heads/")
			if len(g.branch) == 40 {
				g.branch = g.branch[:8]
			}
		}
		break
	}
	l.git[dir] = g
	return g.repo, g.branch
}

func (l *Loader) cmdline(p *proc.Proc) string {
	if e, ok := l.args[p.PID]; ok && e.start.Equal(p.Start) {
		return e.cmd
	}
	c := proc.CommandLine(p.PID)
	if c == "" {
		c = p.Comm
	}
	l.args[p.PID] = argsEntry{start: p.Start, cmd: c}
	return c
}

func (l *Loader) machine(tab *proc.Table, snap *Snapshot) Machine {
	var m Machine
	workerOf := map[int]*Agent{}
	agentOf := map[int]*Agent{} // every agent's own root process
	for _, a := range snap.Agents {
		if a.Worker != nil {
			workerOf[a.Worker.PID] = a
		}
		if a.PID != 0 && tab.Procs[a.PID] != nil {
			agentOf[a.PID] = a
		}
	}
	for pid := range l.args {
		if _, ok := tab.Procs[pid]; !ok {
			delete(l.args, pid)
		}
	}
	for pid := range l.print {
		if _, ok := tab.Procs[pid]; !ok {
			delete(l.print, pid)
		}
	}
	for pid, p := range tab.Procs {
		var row ProcRow
		switch {
		case agentOf[pid] != nil && workerOf[pid] == nil:
			// An agtop session's host, or a claude in a terminal: the agent.
			a := agentOf[pid]
			row = ProcRow{PID: pid, Cmd: l.cmdline(p), Start: p.Start, Role: RoleWorker, Label: a.DisplayName, Key: a.Key}
		case p.Comm == "claude" || strings.HasSuffix(p.Comm, "/claude"):
			cmd := l.cmdline(p)
			row = ProcRow{PID: pid, Cmd: cmd, Start: p.Start}
			switch {
			case strings.Contains(cmd, " daemon run"):
				row.Role, row.Label = RoleDaemon, "daemon"
			case strings.Contains(cmd, "--bg-pty-host"):
				if a := workerOf[pid]; a != nil {
					row.Role, row.Label, row.Key = RoleWorker, a.DisplayName, a.Key
				} else {
					row.Role, row.Label = RoleSpare, "spare (pre-warmed, unclaimed)"
				}
			case strings.Contains(cmd, "--bg-spare"):
				continue // counted inside its pty host
			case strings.HasSuffix(strings.TrimSpace(cmd), " agents") || strings.Contains(cmd, " agents "):
				row.Role, row.Label = RoleView, "claude agents (native view)"
			default:
				if hasClaudeAncestor(tab, p) || underAgent(tab, p, agentOf) || !strings.HasSuffix(strings.Fields(cmd + " x")[0], "claude") {
					continue
				}
				row.Role, row.Label = RoleOther, "claude (interactive)"
			}
		case p.PPID == 1:
			cmd := l.cmdline(p)
			if !strings.Contains(cmd, "/.claude/") {
				continue
			}
			row = ProcRow{PID: pid, Role: RoleOrphan, Cmd: ShellCmd(cmd), Start: p.Start, Label: orphanLabel(cmd)}
		default:
			continue
		}
		m.Rows = append(m.Rows, row)
	}
	roots := make(map[int]bool, len(m.Rows))
	for _, r := range m.Rows {
		roots[r.PID] = true
	}
	for i := range m.Rows {
		r := &m.Rows[i]
		tab.Fill(l.prevTab, []int{r.PID})
		r.Mem, r.CPU, r.Procs = tab.Sum(r.PID, roots)
		m.TotalMem += r.Mem
		m.TotalCPU += r.CPU
		if r.Role == RoleSpare {
			m.Spares++
			m.SpareMem += r.Mem
		}
		if r.Role == RoleOrphan {
			m.Orphans++
			m.OrphanMem += r.Mem
		}
	}
	sort.Slice(m.Rows, func(i, j int) bool {
		if m.Rows[i].Role != m.Rows[j].Role {
			return m.Rows[i].Role < m.Rows[j].Role
		}
		return m.Rows[i].Mem > m.Rows[j].Mem
	})
	return m
}

func isClaudePID(tab *proc.Table, pid int) bool {
	p := tab.Procs[pid]
	return p != nil && p.Comm == "claude"
}

func hasClaudeAncestor(tab *proc.Table, p *proc.Proc) bool {
	for i, pid := 0, p.PPID; i < 64 && pid > 1; i++ {
		q := tab.Procs[pid]
		if q == nil {
			return false
		}
		if q.Comm == "claude" {
			return true
		}
		pid = q.PPID
	}
	return false
}

// underAgent reports whether p runs inside one of the agents' trees.
func underAgent(tab *proc.Table, p *proc.Proc, agentOf map[int]*Agent) bool {
	for i, pid := 0, p.PPID; i < 64 && pid > 1; i++ {
		if agentOf[pid] != nil {
			return true
		}
		q := tab.Procs[pid]
		if q == nil {
			return false
		}
		pid = q.PPID
	}
	return false
}

func orphanLabel(cmd string) string {
	i := strings.Index(cmd, "/.claude/jobs/")
	if i >= 0 && len(cmd) >= i+22 {
		return "job " + cmd[i+14:i+22]
	}
	return "ended session"
}

// ShellCmd is what a Bash-tool shell was asked to run: the command inside
// Claude Code's eval '…' wrapper, without the snapshot sourcing around it.
func ShellCmd(cmd string) string {
	i := strings.Index(cmd, "eval '")
	if i < 0 {
		return cmd
	}
	rest := cmd[i+6:]
	var b strings.Builder
	for len(rest) > 0 {
		if strings.HasPrefix(rest, `'"'"'`) {
			b.WriteByte('\'')
			rest = rest[5:]
			continue
		}
		if rest[0] == '\'' {
			return b.String()
		}
		b.WriteByte(rest[0])
		rest = rest[1:]
	}
	return cmd
}

// isPrint reports whether a claude command line runs it non-interactively.
func isPrint(args []string) bool {
	for _, a := range args {
		if a == "-p" || a == "--print" || strings.HasPrefix(a, "--output-format") {
			return true
		}
	}
	return false
}

// Where says where an interactive-kind agent is being driven from.
func (a *Agent) Where() string {
	if a.Headless {
		return "run by another program (claude -p)"
	}
	return "open in a terminal"
}
