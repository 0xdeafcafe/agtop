// Package fleet folds every source (jobs, roster, pins, transcripts, the
// process table, plan usage) into one snapshot the UI renders.
package fleet

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/daemon"
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
	PID         int  // root of the process tree
	Checking    bool // turn just ended; Claude Code has not classified it yet
	Subs        claude.SubagentStats
	Seen        bool // the user has opened or answered this question already
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
	args    map[int]argsEntry
	git     map[string]gitInfo
	usage   map[string]usageEntry
	prevTab *proc.Table
	spend   map[string]Spend
	nudged  map[string]time.Time
	subs    map[string]subsEntry
}

type subsEntry struct {
	st claude.SubagentStats
	at time.Time
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
		spend: map[string]Spend{}, nudged: map[string]time.Time{}, subs: map[string]subsEntry{},
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
	for _, acct := range cfg.AllAccounts() {
		roster := claude.ReadRoster(acct)
		prs := claude.ReadPRCache(acct)
		pins := map[string]int{}
		for i, id := range claude.ReadPins(acct) {
			pins[id] = i + 1
		}
		av := AccountView{Account: acct, Daemon: daemon.Client{Account: acct}.Running(), Current: acct.Name == active.Name}
		av.Usage = l.readUsage(acct)
		sessions := claude.ReadSessions(acct)
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
				e, ok := l.subs[key]
				if !ok || now.Sub(e.at) > 3*time.Second {
					e = subsEntry{st: claude.ReadSubagentStats(j.TranscriptPath, now), at: now}
					l.subs[key] = e
				}
				a.Subs = e.st
			}
			for _, u := range a.Spend.PRs {
				if pr, ok := prs[u]; ok {
					a.PRs = append(a.PRs, pr)
				}
			}
			if a.Worker != nil {
				a.PID = a.Worker.PID
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
			if ss.Kind != "interactive" || ss.SessionID == "" {
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
			if st == "idle" {
				j.Detail = "open in a terminal"
			}
			a := &Agent{Job: j, Key: key, Acct: acct, DisplayName: ss.Name, Interactive: true, PID: ss.PID}
			if n := ov.Names[key]; n != "" {
				a.DisplayName = n
			}
			_, a.Done = ov.Done[key]
			a.Group = ov.Groups[key]
			a.Repo, a.Branch = l.gitFor(ss.Cwd, now)
			a.Spend = l.spend[key]
			l.sample(tab, a)
			snap.Agents = append(snap.Agents, a)
		}
		snap.Accounts = append(snap.Accounts, av)
	}
	for k := range l.jobs {
		if !seen[k] {
			delete(l.jobs, k)
			delete(l.mtimes, k)
		}
	}
	if tab != nil {
		snap.Machine = l.machine(tab, snap)
		l.prevTab = tab
		snap.Table = tab
	}
	sort.SliceStable(snap.Agents, func(i, j int) bool {
		return snap.Agents[i].Age(now) < snap.Agents[j].Age(now)
	})
	return snap
}

func (l *Loader) sample(tab *proc.Table, a *Agent) {
	if tab == nil || a.PID == 0 {
		return
	}
	tab.Fill(l.prevTab, []int{a.PID})
	a.Mem, a.CPU, a.Procs = tab.Sum(a.PID, nil)
}

func (l *Loader) job(acct claude.Account, id, key string) (claude.Job, bool) {
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
		return claude.Usage{}
	}
	if e, ok := l.usage[acct.ConfigDir]; ok && e.mod.Equal(st.ModTime()) {
		return e.u
	}
	u, _ := claude.ReadUsage(acct)
	l.usage[acct.ConfigDir] = usageEntry{u: u, mod: st.ModTime()}
	return u
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
	for _, a := range snap.Agents {
		if a.Worker != nil {
			workerOf[a.Worker.PID] = a
		}
	}
	for pid := range l.args {
		if _, ok := tab.Procs[pid]; !ok {
			delete(l.args, pid)
		}
	}
	for pid, p := range tab.Procs {
		var row ProcRow
		switch {
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
				if hasClaudeAncestor(tab, p) || !strings.HasSuffix(strings.Fields(cmd + " x")[0], "claude") {
					continue
				}
				row.Role, row.Label = RoleOther, "claude (interactive)"
			}
		case p.PPID == 1:
			cmd := l.cmdline(p)
			if !strings.Contains(cmd, "/.claude/") {
				continue
			}
			row = ProcRow{PID: pid, Role: RoleOrphan, Cmd: cmd, Start: p.Start, Label: orphanLabel(cmd)}
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
	}
	sort.Slice(m.Rows, func(i, j int) bool {
		if m.Rows[i].Role != m.Rows[j].Role {
			return m.Rows[i].Role < m.Rows[j].Role
		}
		return m.Rows[i].Mem > m.Rows[j].Mem
	})
	return m
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

func orphanLabel(cmd string) string {
	i := strings.Index(cmd, "/.claude/jobs/")
	if i >= 0 && len(cmd) >= i+22 {
		return "left by job " + cmd[i+14:i+22]
	}
	return "left by a finished session"
}
