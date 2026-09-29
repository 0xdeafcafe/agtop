package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/state"
)

// An agent a session ran from its shell (claude -p, codex exec) wrote a
// session of its own. rush finds it (by when it started, where, and what
// it was asked), draws its steps under the command's row, and lists it
// with the session's subagents.

// A row of an agent the shell ran leads with its provider's glyph.
func init() { convo.Look = glyph }

// spawnPrefix marks a spawned agent among the subagent runs.
const spawnPrefix = "spawn:"

// spawnEvery is how often a spawned agent not yet found is looked for, and
// spawnGrace how long after its command ended it still may be.
const (
	spawnEvery = 2 * time.Second
	spawnGrace = 10 * time.Second
)

// spawnRun is a spawned agent's session, once found, and how it's read.
type spawnRun struct {
	kind   agent.Kind
	prompt string
	path   string      // its transcript
	tail   *convo.Tail // Claude Code's, read as it grows
	hist   *history    // another agent's, read again when it changes
	sess   *convo.Session
	born   time.Time
	mod    time.Time // when what it wrote was last seen to change
	looked time.Time // when it was last looked for, while not found
	lost   bool      // looked for past its command's end, and not found
	fresh  bool      // its session was just read again, to give its step
	hosted string    // the rush session it runs as, when rush hosts it
}

// view is the spawned agent's conversation as a Tail, for the subagent
// views: Claude Code's own, or another agent's as last read.
func (r *spawnRun) view() *convo.Tail {
	if r.tail != nil {
		return r.tail
	}
	return &convo.Tail{Sess: r.sess}
}

// spawnFoundMsg brings the sessions of spawned agents looked for in the
// background, by the step that ran each; an empty path is one not found.
type spawnFoundMsg struct {
	key    string
	found  map[string]agent.Session
	hosted map[string]string // the rush sessions of those rush hosts
}

// spawnWant is a spawned agent to look for.
type spawnWant struct {
	step       string
	sp         convo.Spawn
	dir        string
	start, end time.Time
}

// refreshSpawns follows the agents the session's shell ran: those found
// are read as they grow and drawn under their rows; those not yet found
// are looked for in the background.
func (m *Model) refreshSpawns() tea.Cmd {
	c := m.host
	if c == nil || c.sess == nil {
		return nil
	}
	steps := c.sess.Spawns()
	if len(steps) == 0 {
		return nil
	}
	if c.spawns == nil {
		c.spawns = map[string]*spawnRun{}
	}
	now := time.Now()
	var want []spawnWant
	taken := map[string]bool{}
	for _, r := range c.spawns {
		taken[r.path] = true
	}
	for _, st := range steps {
		r := c.spawns[st.ID]
		if r == nil {
			r = &spawnRun{}
			c.spawns[st.ID] = r
		}
		if r.path != "" {
			continue // read with the pane's transcripts: refreshSubs
		}
		if r.lost || c.spawnLooking || now.Sub(r.looked) < spawnEvery {
			continue
		}
		// One whose command has ended is looked for until a while after,
		// and at least once: a past conversation's are looked for as it opens.
		end := st.End
		if st.Status == convo.Running || end.IsZero() {
			end = now
		} else if !r.looked.IsZero() && r.looked.After(end.Add(spawnGrace)) {
			r.lost = true
			continue
		}
		r.looked = now
		sp, _ := st.Spawn()
		want = append(want, spawnWant{step: st.ID, sp: sp, dir: m.spawnDir(c, sp), start: st.Start, end: end})
	}
	if len(want) == 0 {
		return nil
	}
	c.spawnLooking = true
	key := c.key
	var mine []agent.Profile // the session's own profile, where its spawns likely wrote
	if a := m.agentByKey(c.key); a != nil {
		mine = append(mine, a.Acct)
	}
	own := c.path
	return func() tea.Msg {
		found, hosted := map[string]agent.Session{}, map[string]string{}
		for _, w := range want {
			if s, ok := findSpawn(w, mine, own, taken); ok {
				found[w.step] = s
				taken[s.Transcript] = true
			}
		}
		// Run through rush's stand-in, it's a rush session: what you
		// type while you watch it goes to it.
		if len(found) > 0 {
			infos := host.List()
			for i := range infos {
				for step := range found {
					if infos[i].SessionID == found[step].ID && infos[i].Meta["spawnedBy"] != "" {
						hosted[step] = infos[i].ID
					}
				}
			}
		}
		return spawnFoundMsg{key: key, found: found, hosted: hosted}
	}
}

// onSpawnFound takes in the spawned agents found, and reads each.
func (m *Model) onSpawnFound(msg spawnFoundMsg) {
	c := m.host
	if c == nil || c.key != msg.key {
		return
	}
	c.spawnLooking = false
	for id, s := range msg.found {
		r, st := c.spawns[id], c.sess.Step(id)
		if r == nil || st == nil || r.path != "" {
			continue
		}
		sp, _ := st.Spawn()
		r.kind, r.prompt, r.path, r.born, r.hosted = s.Kind, sp.Prompt, s.Transcript, s.CreatedAt, msg.hosted[id]
		if agent.ReadsAsClaude(s.Kind) {
			r.tail = convo.NewTail(s.Transcript)
		} else {
			r.hist = &history{kind: s.Kind, s: s}
		}
		c.paneKick = true // read it now, in the background
		if m.loader != nil && len(s.ID) >= 8 && s.Profile.Name != "" {
			// Its row is listed with this session's from now on.
			m.loader.LinkSpawn(state.Key(s.Profile.Name, "i:"+s.ID[:8]), c.key)
		}
	}
}

// takeSpawns takes in what the found agents the shell ran have written,
// read by refreshSubs (grew are the tails that took in something), and
// draws each under its row when anything has.
func (m *Model) takeSpawns(c *hostConn, grew map[*convo.Tail]bool, hists []spawnHist) {
	for _, sh := range hists {
		if sh.sess != nil {
			sh.r.sess = sh.sess
			sh.r.fresh = true
		}
	}
	for _, st := range c.sess.Spawns() {
		r := c.spawns[st.ID]
		if r == nil || r.path == "" {
			continue
		}
		changed := r.fresh
		if r.tail != nil {
			changed = changed || r.sess == nil || grew[r.tail]
			r.sess = r.tail.Sess
		}
		r.fresh = false
		if changed {
			r.mod = time.Now()
		}
		if r.sess != nil && (changed || st.Child() != r.sess) {
			c.sess.SetChild(st, r.sess)
		}
	}
}

// spawnDir is where a spawned agent ran: the folder its command went to,
// against the session's own.
func (m *Model) spawnDir(c *hostConn, sp convo.Spawn) string {
	base := firstNonEmpty(c.sess.Info.Cwd, c.sess.Cwd)
	d := sp.Dir
	if strings.HasPrefix(d, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			d = filepath.Join(home, d[2:])
		}
	}
	switch {
	case d == "":
		return base
	case filepath.IsAbs(d) || base == "":
		return filepath.Clean(d)
	}
	return filepath.Join(base, d)
}

// findSpawn is the session a spawned agent wrote: one of its agent's,
// begun while its command ran, asked what the command asked, and in the
// folder it ran in when that's known. own and taken are transcripts that
// are someone else's.
func findSpawn(w spawnWant, mine []agent.Profile, own string, taken map[string]bool) (agent.Session, bool) {
	from, to := w.start.Add(-3*time.Second), w.end.Add(3*time.Second)
	fits := func(s agent.Session) bool {
		if s.Transcript == "" || s.Transcript == own || taken[s.Transcript] {
			return false
		}
		if s.CreatedAt.Before(from) || s.CreatedAt.After(to) {
			return false
		}
		return samePrompt(w.sp.Prompt, s.Name)
	}
	a, ok := agent.Get(w.sp.Kind)
	if !ok {
		return agent.Session{}, false
	}
	if f, ok := a.(agent.SpawnFinder); ok {
		return f.FindSpawn(append(mine, a.Profiles()...), w.dir, w.start, fits)
	}
	d, ok := a.(agent.Discoverer)
	if !ok {
		return agent.Session{}, false
	}
	var best agent.Session
	for _, p := range a.Profiles() {
		list := d.Live(p)
		if time.Since(w.end) > time.Minute {
			list = append(list, d.Past(p)...)
		}
		for _, s := range list {
			if !fits(s) || s.Cwd != "" && w.dir != "" && !sameDir(s.Cwd, w.dir) {
				continue
			}
			if best.Transcript == "" || s.CreatedAt.Before(best.CreatedAt) {
				best = s
			}
		}
	}
	return best, best.Transcript != ""
}

// samePrompt is whether a session's first words are what the command
// asked: the start of it, as agents clip and fold what they keep. A prompt
// not known (fed from a file) matches anything.
func samePrompt(asked, got string) bool {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	a, g := norm(asked), norm(got)
	if a == "" {
		return true
	}
	n := min(len(a), len(g), 60)
	return n > 0 && a[:n] == g[:n]
}

func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// spawnSubs are the spawned agents found, as subagent runs.
func (c *hostConn) spawnSubs() []convo.Subagent {
	var out []convo.Subagent
	for _, st := range c.sess.Spawns() {
		r := c.spawns[st.ID]
		if r == nil || r.path == "" {
			continue
		}
		sp, _ := st.Spawn()
		sa := convo.Subagent{ID: spawnPrefix + st.ID, Type: sp.Name, Description: firstNonEmpty(oneLineUI(sp.Prompt), sp.From),
			Model: sp.Model, ToolUseID: st.ID, Path: r.path, Born: r.born.UnixNano()}
		if !r.mod.IsZero() {
			sa.Mod = r.mod.UnixNano() // noticed as it was read, not asked of the disk
		}
		if r.tail != nil {
			sa.Size = r.tail.Size()
		}
		out = append(out, sa)
	}
	return out
}

// spawnState is how a spawned agent stands: its command still running, or
// its background shell; else how the command ended.
func (c *hostConn) spawnState(sa convo.Subagent) (status string, live bool) {
	for _, j := range c.sess.Jobs() {
		if j.ToolUseID == sa.ToolUseID {
			if j.Running() {
				return "", true
			}
			if j.Status == "failed" || j.Status == "stopped" {
				return j.Status, false
			}
		}
	}
	st := c.sess.Step(sa.ToolUseID)
	switch {
	case st == nil:
		return "", false
	case st.Status == convo.Running:
		return "", true
	case st.Status == convo.Failed:
		return "failed", false
	case st.Status == convo.Lost:
		return "stopped", false
	}
	return "", false
}

// watchedHost is the rush session of the spawned agent the pane shows,
// when rush hosts it: what you send goes to it rather than the session.
func (m *Model) watchedHost(c *hostConn) string {
	if !m.watchingSub(c) {
		return ""
	}
	if r := c.spawnRunFor(c.subOpen); r != nil {
		return r.hosted
	}
	return ""
}

// spawnRunFor is the spawned agent a subagent run id stands for, or nil.
func (c *hostConn) spawnRunFor(id string) *spawnRun {
	if !strings.HasPrefix(id, spawnPrefix) || c.spawns == nil {
		return nil
	}
	if r := c.spawns[strings.TrimPrefix(id, spawnPrefix)]; r != nil && r.path != "" && r.sess != nil {
		return r
	}
	return nil
}

func oneLineUI(s string) string { return strings.Join(strings.Fields(s), " ") }
