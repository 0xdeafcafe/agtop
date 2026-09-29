// Package fleet folds every source (jobs, roster, pins, transcripts, the
// process table, plan usage) into one snapshot the UI renders.
package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/proc"
	"github.com/0xdeafcafe/agtop/internal/state"
)

type Agent struct {
	agent.Job
	// Extra is the adapter's own record of it: Claude Code's job file
	// (claude.Job) for its background sessions.
	Extra any
	Key   string
	// Acct is the profile it runs in: the agent's config folder.
	Acct        agent.Profile
	DisplayName string
	Pinned      bool
	Done        bool
	Group       string
	Worker      *claude.Worker
	Repo        string
	// Root is the main checkout of Repo's repository: Repo itself, or for
	// a linked worktree the checkout it was made from.
	Root        string
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
	// Past is a conversation nothing has open, known from its transcript
	// alone: a message resumes it in agtop mode.
	Past bool
	// Temp is how much disk its temp work takes, as last measured.
	Temp int64
	// Kind is the agent an agtop session runs: empty is Claude Code.
	Kind string
	// Profile is the profile an agtop session was started under.
	Profile string
	// History is another agent's transcript, read through its adapter:
	// TranscriptPath is only ever Claude Code's.
	History string
	// Remote is a session running on its agent's own servers: Copilot's
	// coding agent on GitHub.
	Remote bool
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
func (l *Loader) Nudge(key string) {
	l.inMu.Lock()
	defer l.inMu.Unlock()
	if l.in.nudged == nil {
		l.in.nudged = map[string]time.Time{}
	}
	l.in.nudged[key] = time.Now()
	l.in.stale = true
}

// JustFinished is a turn that ended moments ago; it lingers in Working so a
// finish is noticed rather than vanishing into history.
func (a *Agent) JustFinished(now time.Time) bool {
	return a.State == "done" && !a.Busy() && now.Sub(a.UpdatedAt) < 2*time.Minute
}

// Busy is a finished turn whose background work is still running in a live
// process; a job file can claim work long after its process has gone.
// Subagents still writing count too: the job file lists them late, and a
// terminal session never does. Their transcripts are proof enough on their
// own, as a background job's process isn't always known.
func (a *Agent) Busy() bool {
	return a.PID != 0 && a.Job.Busy() || !a.Live() && a.Subs.Direct+a.Subs.Nested > 0
}

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
	Halt  *claude.Halt // the error its last turn ended on, if any
	// Progress is the last count it reported moving ("lint 11,065 →
	// 9,052"), and when; Context is its newest message's context, and
	// Compacts how often that was compacted.
	Progress   string
	ProgressAt time.Time
	Context    int64
	Compacts   int
}

type AccountView struct {
	claude.Account
	Usage claude.Usage // who it's signed in as, and its plan
	// Quota is the plan's limits, as Usage read them.
	Quota   usage.Quota
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
	Logins   []LoginView
	Machine  Machine
	Table    *proc.Table
}

// Loader keeps the cheap caches between refreshes.
type Loader struct {
	// mu is held by Load, which runs off the UI's goroutine. What the UI
	// hands in waits in "in", under a lock of its own held only a moment,
	// so the UI never waits for a Load to finish; Load takes it in first.
	mu      sync.Mutex
	inMu    sync.Mutex
	in      inbox
	store   *state.Store
	args    map[int]argsEntry
	git     map[string]gitInfo
	roots   map[string]string // folder → main checkout, for mainCheckout
	usage   map[string]usageEntry
	prevTab *proc.Table
	spend   map[string]Spend
	nudged  map[string]time.Time
	subs    map[string]subsEntry
	fetched map[string]claude.Usage
	// UsagePath is the readings every agtop process and session shares;
	// usageMod is its time when last read.
	UsagePath string
	usageMod  time.Time
	files     map[string]fileMemo
	hosts     host.Lister
	print     map[int]printEntry
	Temp      *TempSizes
	// pastRows are past conversations' rows as last made, and spendVer
	// counts each agent's spend updates, so an unchanged row is reused.
	pastRows map[string]pastRow
	// pastKeys are the built-in agent's past conversations' row keys.
	pastKeys pastKeys
	// others are other agents' past sessions, by profile folder.
	others   map[string]othersListing
	spendVer map[string]int
	watching *watching
}

// printEntry remembers whether a pid runs claude -p, by its start time.
type printEntry struct {
	start time.Time
	print bool
}

// inbox is what was handed in since the last Load began.
type inbox struct {
	settle  bool
	stale   bool
	spend   map[string]Spend
	nudged  map[string]time.Time
	fetched []fetchedIn // in the order they came
}

type fetchedIn struct {
	configDir string
	u         claude.Usage
}

// takeIn takes in what was handed in since the last Load; l.mu is held.
func (l *Loader) takeIn() {
	l.inMu.Lock()
	in := l.in
	l.in = inbox{}
	l.inMu.Unlock()
	for k, v := range in.spend {
		l.spend[k] = v
		l.spendVer[k]++
	}
	for k, t := range in.nudged {
		l.nudged[k] = t
	}
	for _, f := range in.fetched {
		l.setFetched(f.configDir, f.u)
	}
	if in.stale {
		l.changedSince()
	}
	if in.settle && l.watching != nil {
		l.watching.settle = true
	}
}

// SetFetched stores a usage reading fetched from Anthropic for an account.
func (l *Loader) SetFetched(configDir string, u claude.Usage) {
	l.inMu.Lock()
	defer l.inMu.Unlock()
	l.in.fetched = append(l.in.fetched, fetchedIn{configDir, u})
	l.in.stale = true
}

func (l *Loader) setFetched(configDir string, u claude.Usage) {
	if old, ok := l.fetched[configDir]; ok && u.FetchedAt.IsZero() {
		old.Problem = u.Problem // keep the last good numbers, note why they're not refreshing
		l.fetched[configDir] = old
		return
	}
	l.fetched[configDir] = u
}

// syncUsage takes the readings shared through UsagePath that are newer
// than those it has: a session's, made as it runs, reach the header within
// a second rather than at the next fetch.
func (l *Loader) syncUsage() {
	st, err := os.Stat(l.UsagePath)
	if err != nil || st.ModTime().Equal(l.usageMod) {
		return
	}
	l.usageMod = st.ModTime()
	for k, f := range claude.LoadFetchedUsage(l.UsagePath) {
		if old, ok := l.fetched[k]; ok && !f.Usage.FetchedAt.After(old.FetchedAt) {
			continue
		}
		if time.Now().Before(f.Wait) {
			f.Usage.Problem = "rate-limited to " + f.Wait.Local().Format("15:04")
		}
		l.fetched[k] = f.Usage
	}
}

type subsEntry struct {
	st   claude.SubagentStats
	at   time.Time
	dir  time.Time // the subagents folder's time when counted
	main int64     // the transcript's size when counted
	gone bool      // whether its process was known to be gone
	runs *claude.SubagentRuns
}

// subagents counts an agent's subagent runs, and those still working.
// Whether one is working is read from the transcripts (SubagentRuns): its
// call without a result, or a background launch not yet reported done,
// however long the run itself has been quiet, unless gone says the
// session's process is known to have exited. It's counted again when a run
// started (the folder changed), when the transcript grew (a run ended or
// was woken), when some were working, or after 30s.
func (l *Loader) subagents(key, transcript string, gone bool, now time.Time) claude.SubagentStats {
	var dir time.Time
	if st, err := os.Stat(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")); err == nil {
		dir = st.ModTime()
	} else {
		return claude.SubagentStats{}
	}
	var main int64
	if st, err := os.Stat(transcript); err == nil {
		main = st.Size()
	}
	e, ok := l.subs[key]
	working := e.st.Direct+e.st.Nested > 0
	if ok && e.runs != nil && e.dir.Equal(dir) && e.main == main && e.gone == gone && now.Sub(e.at) < 30*time.Second && (!working || now.Sub(e.at) < 3*time.Second) {
		return e.st
	}
	if e.runs == nil {
		e.runs = &claude.SubagentRuns{}
	}
	e.runs.Gone = gone
	e.st, e.at, e.dir, e.main, e.gone = e.runs.Stats(transcript, now), now, dir, main, gone
	l.subs[key] = e
	return e.st
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
		store: s,
		args:  map[int]argsEntry{}, git: map[string]gitInfo{}, roots: map[string]string{}, usage: map[string]usageEntry{},
		spend: map[string]Spend{}, nudged: map[string]time.Time{}, subs: map[string]subsEntry{}, fetched: map[string]claude.Usage{},
		files: map[string]fileMemo{}, pastRows: map[string]pastRow{}, spendVer: map[string]int{}, print: map[int]printEntry{},
		Temp: LoadTempSizes(), UsagePath: filepath.Join(state.Dir(), "usage.json"),
	}
}

// SetSpend receives cost totals from the background scanner.
func (l *Loader) SetSpend(m map[string]Spend) {
	if len(m) == 0 {
		return
	}
	l.inMu.Lock()
	defer l.inMu.Unlock()
	if l.in.spend == nil {
		l.in.spend = make(map[string]Spend, len(m))
	}
	for k, v := range m {
		l.in.spend[k] = v
	}
	l.in.stale = true
}

func (l *Loader) Load(sampleProcs bool) *Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load(sampleProcs)
}

// LoadFrom is Load reading the config and overlay from s, a copy the UI
// made (state.Store.Copy), rather than the store it goes on changing.
func (l *Loader) LoadFrom(s *state.Store, sampleProcs bool) *Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.store = s
	return l.load(sampleProcs)
}

func (l *Loader) load(sampleProcs bool) *Snapshot { //nolint:gocognit,gocyclo,maintidx // Load's body as it was, moved under the lock
	l.takeIn()
	now := time.Now()
	if sampleProcs {
		if snap, ok := l.reuse(now); ok {
			return snap
		}
	}
	l.began()
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
	// Processes of sessions a row stands for: a claude -p run under one of
	// them is its spawn, counted with its subagents rather than a row.
	parents := map[int]bool{}
	for _, info := range hosted {
		parents[info.ClaudePID], parents[info.HostPID] = true, true
	}
	var spawned []int
	// Conversations a row already stands for: the rest are past ones.
	claimed := map[string]bool{}
	for id := range ours {
		claimed[id] = true
	}
	l.branches(hosted, claimed)
	l.syncUsage()
	for _, acct := range []claude.Account{active} { // ~/.claude: every session runs there
		roster := l.memo(acct.RosterPath(), func() any { return claude.ReadRoster(acct) }).(claude.Roster)
		prs := l.memo(acct.PRCachePath(), func() any { return claude.ReadPRCache(acct) }).(map[string]claude.PR)
		pins := l.memo(claude.PinsPath(acct), func() any {
			pins := map[string]int{}
			for i, id := range claude.ReadPins(acct) {
				pins[id] = i + 1
			}
			return pins
		}).(map[string]int)
		av := AccountView{Account: acct, Daemon: daemon.Client{Account: acct}.Running(), Current: true}
		av.Usage = l.readUsage(acct)
		av.Quota = av.Usage.Quota(claude.UsageKey(acct, av.Usage))
		// Its sessions, as its adapter finds them: background jobs, then
		// every session file whose process is alive.
		var jobs []claude.Job
		var sessions []claude.Session
		live := builtinLive(acct)
		for i := range live {
			switch x := live[i].Extra.(type) {
			case claude.Job:
				jobs = append(jobs, x)
			case claude.Session:
				sessions = append(sessions, x)
			}
		}
		byJob := map[string]claude.Session{}
		for _, ss := range sessions {
			parents[ss.PID] = true
		}
		for _, ss := range sessions {
			claimed[ss.SessionID] = true
			if ss.JobID != "" {
				byJob[ss.JobID] = ss
			}
		}
		for i := range jobs {
			j := &jobs[i]
			id := j.ID
			key := state.Key(acct.Name, id)
			seen[key] = true
			claimed[j.SessionID] = true
			a := &Agent{Job: j.Job, Extra: *j, Key: key, Acct: acct.Profile(), DisplayName: j.Name}
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
				// Its process isn't always known, so it's never taken as gone.
				a.Subs = l.subagents(key, j.TranscriptPath, false, now)
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
			j := agent.Job{
				ID: ss.SessionID[:8], Account: acct.Name, Name: ss.Name, State: st, Cwd: ss.Cwd,
				SessionID: ss.SessionID, CreatedAt: ss.StartedAt(), UpdatedAt: ss.UpdatedAt(),
				TranscriptPath: filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(ss.Cwd), ss.SessionID+".jsonl"),
			}
			headless := l.isPrint(tab, ss.PID)
			if headless && spawnOf(tab, ss.PID, parents) != 0 {
				spawned = append(spawned, ss.PID)
				continue
			}
			if st == "idle" {
				j.Detail = "open in a terminal"
				if headless {
					j.Detail = "run by another program"
				}
			}
			a := &Agent{Job: j, Key: key, Acct: acct.Profile(), DisplayName: ss.Name, Interactive: true, Headless: headless, PID: ss.PID}
			if n := ov.Names[key]; n != "" {
				a.DisplayName = n
			}
			_, a.Done = ov.Done[key]
			a.Group = ov.Groups[key]
			a.Repo, a.Branch = l.gitFor(ss.Cwd, now)
			a.Spend = l.spend[key]
			a.Subs = l.subagents(key, j.TranscriptPath, false, now) // listed only while its process runs
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
			// Every Claude session runs in ~/.claude, whatever name its
			// folder had when it started.
			if otherAgent(info.Kind) {
				continue
			}
			a := l.hostedAgent(acct, info, tab, now)
			if a.Live() {
				av.Live++
			}
			av.Agents++
			av.Spend += a.Spend.Cost
			av.Today += a.Spend.Today
			snap.Agents = append(snap.Agents, a)
		}
		for _, a := range l.pastAgents(acct, claimed, seen, now) {
			av.Agents++
			av.Spend += a.Spend.Cost
			av.Today += a.Spend.Today
			snap.Agents = append(snap.Agents, a)
		}
		snap.Accounts = append(snap.Accounts, av)
	}
	// Other agents' sessions belong to no Claude folder: they're listed
	// under their own profile's name.
	for _, info := range hosted {
		if otherAgent(info.Kind) {
			snap.Agents = append(snap.Agents, l.hostedAgent(claude.Account{Name: info.Account}, info, tab, now))
		}
	}
	snap.Agents = append(snap.Agents, l.otherAgents(claimed, seen, now)...)
	// Rows kept from one reading to the next (past conversations) are
	// the loader's: the snapshot gets its own, which the UI may change
	// while the next reading is made.
	for i, a := range snap.Agents {
		c := *a
		snap.Agents[i] = &c
	}
	countSpawns(tab, snap.Agents, spawned)
	if len(snap.Accounts) > 0 {
		snap.Logins = l.logins(cfg, snap.Accounts[0], now)
	}
	if tab != nil {
		snap.Machine = l.machine(tab, snap)
		l.prevTab = tab
		snap.Table = tab
	}
	l.Temp.Save()
	listed := make(map[string]bool, len(snap.Agents))
	for _, a := range snap.Agents {
		a.Temp = l.Temp.Bytes(a.Key)
		if a.Repo != "" {
			a.Root = firstNonEmpty(mainCheckout(a.Repo, l.roots), a.Repo)
		}
		listed[a.Key] = true
	}
	for k := range l.subs {
		if !listed[k] {
			delete(l.subs, k) // an agent no longer listed: its runs' reader goes
		}
	}
	sort.SliceStable(snap.Agents, func(i, j int) bool {
		return snap.Agents[i].Age(now) < snap.Agents[j].Age(now)
	})
	if sampleProcs {
		l.read(snap, hosted)
	}
	return snap
}

// hosted turns an agtop-mode session's info into an agent row.
// builtinProfile is the built-in agent's profile for acct.
func builtinProfile(acct claude.Account) agent.Profile {
	return agent.Profile{Kind: agent.BuiltinKind(), Name: acct.Name, Dir: acct.ConfigDir}
}

// builtinDiscoverer is how the built-in agent's sessions are found.
func builtinDiscoverer() (agent.Discoverer, bool) {
	a, ok := agent.Get(agent.BuiltinKind())
	if !ok {
		return nil, false
	}
	d, ok := a.(agent.Discoverer)
	return d, ok
}

// builtinLive is the built-in agent's running sessions in acct.
func builtinLive(acct claude.Account) []agent.Session {
	if d, ok := builtinDiscoverer(); ok {
		return d.Live(builtinProfile(acct))
	}
	return nil
}

// otherAgent is whether kind is an agent other than the built-in one,
// whose sessions Load finds itself.
func otherAgent(kind string) bool { return !agent.IsBuiltin(agent.Kind(kind)) }

// builtinComm is whether a process called comm is the built-in agent's
// program.
func builtinComm(comm string) bool {
	p := agent.ProgramOf(agent.BuiltinKind())
	return p != "" && (comm == p || strings.HasSuffix(comm, "/"+p))
}

// hostedAgent is an agtop session's row, with what you've set on it.
func (l *Loader) hostedAgent(acct claude.Account, info host.Info, tab *proc.Table, now time.Time) *Agent {
	ov := l.store.Overlay
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
	if a.Job.TranscriptPath != "" {
		// Its host says whether Claude Code runs: when neither runs, nor
		// does anything Claude Code started.
		gone := a.PID == 0 || info.Proto >= 3 && info.ClaudePID == 0 && info.State != "working"
		a.Subs = l.subagents(a.Key, a.Job.TranscriptPath, gone, now)
	}
	// A transcript is priced call by call, subagents and all; the host's
	// own figure is only for agents that leave none. (Older hosts summed
	// Claude Code's running totals, so the one they saved can be far out.)
	if !a.Spend.Ready && a.Spend.Cost < info.CostUSD {
		a.Spend.Cost = info.CostUSD
	}
	return a
}

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
	j := agent.Job{
		ID: info.ID, Account: acct.Name, Name: name, State: st, Detail: info.Detail, Needs: info.Needs,
		Cwd: info.Cwd, SessionID: info.SessionID, CreatedAt: info.StartedAt, UpdatedAt: info.UpdatedAt,
	}
	if !otherAgent(info.Kind) {
		j.TranscriptPath = filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(info.Cwd), info.SessionID+".jsonl")
	}
	// What it runs in the background, as Claude Code's own background
	// sessions record theirs, so the list says so alike.
	for _, t := range info.Background {
		kind := "shell"
		switch t.Type {
		case "local_agent", "remote_agent", "in_process_teammate":
			kind = "agent"
			j.Subagents++
		case "monitor_mcp", "monitor_ws":
			kind = "monitor"
		case "local_bash":
		default:
			kind = "task"
		}
		j.Running = append(j.Running, claude.Task{Kind: kind, Label: t.Label, StartedAt: t.StartedAt})
		j.Background = append(j.Background, kind+"\x00"+t.Label)
		j.InFlight++
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
	case info.Retry != nil && info.Retry.Offline:
		j.Detail = "offline · continues when the network is back"
	case info.Retry != nil && info.Retry.Proof:
		j.Detail = "API error · continues once the connection holds"
	case info.Retry != nil:
		j.Detail = fmt.Sprintf("API error · retry %d of %d", info.Retry.Attempt, info.Retry.Max)
	case info.Error != "" && st == "done":
		j.Detail = "stopped mid-turn · your next message resumes it"
	}
	a := &Agent{Job: j, Key: state.Key(acct.Name, "a:"+info.ID), Acct: agent.Profile{Kind: agent.KindOf(info.Kind), Name: acct.Name, Dir: acct.ConfigDir}, DisplayName: name, Agtop: true, Kind: info.Kind, Profile: info.Profile}
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
// Windows that have reset since either reading are dropped.
func (l *Loader) freshest(acct claude.Account, cached claude.Usage) claude.Usage {
	f, ok := l.fetched[acct.ConfigDir]
	if cached.AccountID != "" {
		// Readings are kept by the login it's signed in as.
		g, gok := l.fetched[claude.Login{ID: cached.AccountID}.UsageKey()]
		if gok && (!ok || g.FetchedAt.After(f.FetchedAt) || f.AccountID != cached.AccountID) {
			f, ok = g, true
			f.AccountID = cached.AccountID
		}
	}
	if !ok {
		return cached.Since(time.Now())
	}
	// A reading made before the folder was signed in as another account is
	// that account's, not this one's.
	other := f.AccountID != "" && cached.AccountID != "" && f.AccountID != cached.AccountID
	if f.FetchedAt.After(cached.FetchedAt) && !other {
		f.AccountID, f.Email, f.Org, f.Plan = cached.AccountID, cached.Email, cached.Org, cached.Plan
		f.Role, f.Billing, f.OrgType, f.Extra = cached.Role, cached.Billing, cached.OrgType, cached.Extra
		return f.Since(time.Now())
	}
	if !other {
		cached.Problem = f.Problem
	}
	return cached.Since(time.Now())
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
	workerOf := make(map[int]*Agent, len(snap.Agents))
	agentOf := make(map[int]*Agent, len(snap.Agents)) // every agent's own root process
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
		case builtinComm(p.Comm):
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
				if hasClaudeAncestor(tab, p) || underAgent(tab, p, agentOf) || !builtinComm(strings.Fields(cmd + " x")[0]) {
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
	return p != nil && p.Comm == agent.ProgramOf(agent.BuiltinKind())
}

func hasClaudeAncestor(tab *proc.Table, p *proc.Proc) bool {
	for i, pid := 0, p.PPID; i < 64 && pid > 1; i++ {
		q := tab.Procs[pid]
		if q == nil {
			return false
		}
		if builtinComm(q.Comm) {
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
	if a.Remote {
		return "on GitHub"
	}
	return "open in a terminal"
}

// spawnOf is the nearest process above pid that is one of parents: the
// session whose shell ran it. Zero when none is.
func spawnOf(tab *proc.Table, pid int, parents map[int]bool) int {
	if tab == nil || tab.Procs[pid] == nil || parents[tab.Procs[pid].PPID] {
		// Run straight from a session's process is no shell's: a host's
		// own Claude Code, before the host says which it is.
		return 0
	}
	for i, p := 0, tab.Procs[pid]; p != nil && i < 12; i++ {
		if p.PPID <= 1 {
			return 0
		}
		if parents[p.PPID] && p.PPID != pid {
			return p.PPID
		}
		p = tab.Procs[p.PPID]
	}
	return 0
}

// countSpawns counts each agent run from a session's shell among that
// session's working subagents: its row's process is above it.
func countSpawns(tab *proc.Table, agents []*Agent, spawned []int) {
	if len(spawned) == 0 {
		return
	}
	byPID := map[int]*Agent{}
	for _, a := range agents {
		if a.PID != 0 {
			byPID[a.PID] = a
		}
	}
	for _, pid := range spawned {
		for i, p := 0, tab.Procs[pid]; p != nil && i < 12; i, p = i+1, tab.Procs[p.PPID] {
			if a := byPID[p.PPID]; a != nil {
				a.Subs.Direct++
				a.Subs.Spawned++
				break
			}
		}
	}
}
