package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

// The Overview place: what is happening, over time. Top, what is going
// on now: each working session, what every subagent it runs last said,
// and the heavy work in its checkout. Below, what happened, newest first:
// what you asked, tasks ticked off, subagents started and finished (with
// what they reported), turns ended, questions, commits, PRs and errors.
// It is read from the transcripts, only while the place is open.

// workSince is how far back the overview looks.
const workSince = 24 * time.Hour

// workPages are the Overview's pages: what's going on and what happened,
// each project whole, and every agent at once on the Wall.
var workPages = []string{"Now", "Projects", "Wall"}

const (
	workNowPage = iota
	workProjects
	workWall
)

// setWorkPage shows one of the Overview's pages; the Wall has a mode of
// its own.
func (m *Model) setWorkPage(p int) {
	m.work.page = (p + len(workPages)) % len(workPages)
	m.mode = modeWork
	if m.work.page == workWall {
		m.mode = modeWall
	}
}

type workState struct {
	page int // which of workPages
	// pos is the row picked; sel is its id, so new rows above it keep it
	// picked.
	pos  int
	sel  string
	only string // show only this session's history, by key
	// projPos and projSel are the Projects page's pos and sel.
	projPos int
	projSel string

	tls     map[string]*claude.Timeline // by transcript path; the loader's alone
	views   map[string]claude.TimelineView
	loading bool
	loaded  time.Time

	args map[string]args // command lines looked up, by pid and start
}

type args struct {
	line string
	at   time.Time
}

type workTimelinesMsg struct {
	tls   map[string]*claude.Timeline
	views map[string]claude.TimelineView
}

// workLoad reads what the open sessions' transcripts have gained.
func (m *Model) workLoad() tea.Cmd {
	w := &m.work
	if w.loading {
		return nil
	}
	w.loading = true
	tls := w.tls
	paths := map[string]string{}
	for _, a := range m.workAgents() {
		if a.TranscriptPath != "" {
			paths[a.Key] = a.TranscriptPath
		}
	}
	return func() tea.Msg {
		if tls == nil {
			tls = map[string]*claude.Timeline{}
		}
		since := time.Now().Add(-workSince)
		views := map[string]claude.TimelineView{}
		want := make(map[string]bool, len(paths))
		for _, p := range paths {
			want[p] = true
		}
		for p := range tls {
			if !want[p] {
				delete(tls, p) // a session that's aged out of the overview
			}
		}
		for key, p := range paths {
			tl := tls[p]
			if tl == nil {
				tl = &claude.Timeline{}
				tls[p] = tl
			}
			tl.Update(p, since)
			views[key] = tl.View(since)
		}
		return workTimelinesMsg{tls: tls, views: views}
	}
}

func (m *Model) onWorkTimelines(msg workTimelinesMsg) {
	w := &m.work
	w.loading, w.loaded = false, time.Now()
	w.tls, w.views = msg.tls, msg.views
}

// workTick reads again every few seconds while the place is open.
func (m *Model) workTick() tea.Cmd {
	if m.mode != modeWork || time.Since(m.work.loaded) < 4*time.Second {
		return nil
	}
	return m.workLoad()
}

// workAgents are the sessions worth reading: open, busy or at work within
// the window, and not put away.
func (m *Model) workAgents() []*fleet.Agent {
	now := m.snap.At
	var out []*fleet.Agent
	for _, a := range m.snap.Agents {
		if a.Done || a.Past {
			continue
		}
		if a.Live() || a.Busy() || a.PID != 0 || now.Sub(a.UpdatedAt) < workSince {
			out = append(out, a)
		}
	}
	return out
}

// workRow is one line of the place; a row with an agent can be picked.
type workRow struct {
	id   string
	a    *fleet.Agent
	line string
}

// running is whether a subagent is still at work, as far as the
// transcripts tell: not ended, lately written, its session still there.
func running(r claude.Run, a *fleet.Agent, now time.Time) bool {
	return !r.Ended && now.Sub(r.Mod) < claude.RunStale && (a.Live() || a.Busy() || a.PID != 0)
}

// workNow is whether a session goes in Now: it is working, has work
// running, or waits on you.
func (m *Model) workNow(a *fleet.Agent, now time.Time) bool {
	if a.Live() || a.Busy() || a.Halted() || a.NeedsYou() || a.Waiting() || a.YourTurn(now) {
		return true
	}
	for _, r := range m.work.views[a.Key].Runs {
		if running(r, a, now) {
			return true
		}
	}
	return false
}

func (m *Model) workRows() []workRow {
	w := min(m.w-4, 170)
	now := m.snap.At
	agents := m.workAgents()
	var rows []workRow
	text := func(s string) { rows = append(rows, workRow{line: s}) }

	var live []*fleet.Agent
	for _, a := range agents {
		if m.workNow(a, now) {
			live = append(live, a)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].UpdatedAt.After(live[j].UpdatedAt) })
	lanes := 0
	for _, a := range live {
		for _, r := range m.work.views[a.Key].Runs {
			if running(r, a, now) {
				lanes++
			}
		}
	}
	meta := "nothing running"
	if len(live) > 0 {
		meta = fmt.Sprintf("%d session%s", len(live), plural(len(live)))
		if lanes > 0 {
			meta += fmt.Sprintf(" · %d subagent%s working", lanes, plural(lanes))
		}
	}
	text(rule("Now", meta, w))
	text("")
	for _, a := range live {
		rows = append(rows, workRow{id: "s" + a.Key, a: a, line: m.workSession(a, w, now)})
		for _, h := range m.workHeavy(a) {
			text("     " + h)
		}
		for _, r := range m.workRuns(a, now) {
			rows = append(rows, workRow{id: "r" + a.Key + r.ID, a: a, line: m.workRun(a, r, w, now)})
		}
	}
	if len(live) == 0 {
		text(dim("  no session is working, and none waits on you"))
	}

	type item struct {
		a *fleet.Agent
		e claude.Happening
	}
	var feed []item
	for _, a := range agents {
		if m.work.only != "" && a.Key != m.work.only {
			continue
		}
		for _, e := range m.work.views[a.Key].Events {
			if workShown(e) {
				feed = append(feed, item{a, e})
			}
		}
	}
	sort.SliceStable(feed, func(i, j int) bool { return feed[i].e.At.After(feed[j].e.At) })
	meta = "the last day"
	if o := m.agentByKey(m.work.only); o != nil {
		meta = "the last day of " + oneLine(o.DisplayName) + " · f for everyone"
	}
	text("")
	text(rule("What happened", meta, w))
	text("")
	if len(feed) == 0 {
		switch {
		case m.work.views == nil:
			text(dim("  reading transcripts…"))
		default:
			text(dim("  nothing yet"))
		}
	}
	day := now.Local().YearDay()
	for i, it := range feed {
		if d := it.e.At.Local().YearDay(); d != day {
			day = d
			text(faint("  " + it.e.At.Local().Format("Monday 2 January")))
		}
		id := fmt.Sprintf("e%s%d%d", it.a.Key, it.e.At.UnixNano(), it.e.Kind)
		rows = append(rows, workRow{id: id, a: it.a, line: m.workEvent(it.a, it.e, w)})
		if i == 400 {
			text(faint(fmt.Sprintf("  …and %d earlier", len(feed)-i-1)))
			break
		}
	}
	return rows
}

// workShown leaves out what says nothing: monitors ticking.
func workShown(e claude.Happening) bool {
	return !(e.Kind == claude.EvEnd && e.Run == "" && strings.HasPrefix(e.Text, "Monitor "))
}

// workRuns are a session's subagents worth a row: those working, and those
// that ended in the last half hour.
func (m *Model) workRuns(a *fleet.Agent, now time.Time) []claude.Run {
	var out []claude.Run
	for _, r := range m.work.views[a.Key].Runs {
		if running(r, a, now) || r.Ended && now.Sub(r.EndedAt) < 30*time.Minute {
			out = append(out, r)
		}
	}
	return out
}

// workSession is a session in Now: its state, then how far through its
// tasks it is and how full its context.
func (m *Model) workSession(a *fleet.Agent, w int, now time.Time) string {
	marker := dim("◦")
	state := dim("idle")
	switch {
	case a.Halted():
		marker, state = paint(cRed, "✗"), paint(cRed, "stopped · "+oneLine(a.HaltReason()))
	case a.NeedsYou() || a.Waiting():
		q := oneLine(a.Needs)
		if q == "" {
			q = oneLine(a.Detail)
		}
		marker, state = paint(cYellow, "?"), paint(cYellow, "asks: ")+paint(cText, q)
	case a.YourTurn(now):
		marker, state = paint(cGreen, "◆"), paint(cGreen, "your turn · ")+paint(cSub, m.workSaid(a))
	case a.Live():
		marker = paint(cOrange, spinner[(m.tick+len(a.ID))%len(spinner)])
		state = m.workSaid(a)
		if p := m.previews[a.Key].p; p.Doing != "" {
			state = oneLine(p.Doing)
		}
		if state == "" {
			state = "working"
		}
		state = paint(cText, state)
	case a.Busy():
		marker, state = paint(cBlue, "◎"), paint(cBlue, lanesLine(a))
	}
	var tail []string
	planned, ticked := 0, 0
	for _, e := range m.work.views[a.Key].Events {
		if e.Run == "" && e.Kind == claude.EvPlan {
			planned += e.N
		}
		if e.Run == "" && e.Kind == claude.EvTick {
			ticked++
		}
	}
	if planned > 0 {
		tail = append(tail, paint(cGreen, fmt.Sprintf("✓ %d/%d", min(ticked, planned), planned)))
	}
	if a.Spend.Context > 0 {
		pc := int(100 * a.Spend.Context / contextWindow(a))
		c := dim
		if pc >= 80 {
			c = func(s string) string { return paint(cYellow, s) }
		}
		tail = append(tail, c(fmt.Sprintf("ctx %d%%", pc)))
	}
	tail = append(tail, faint(right(age(now.Sub(a.UpdatedAt)), 5)))
	t := strings.Join(tail, "  ")
	left := " " + marker + " " + paint(cText+bold, fit(oneLine(a.DisplayName), 26)) + " "
	return left + fit(state, w-cellw.String(left)-cellw.String(t)-2) + "  " + t
}

// workSaid is what a session last said: Claude Code's summary of it, or
// its last turn's words when that summary is only a synthetic reply.
func (m *Model) workSaid(a *fleet.Agent) string {
	d := oneLine(a.Detail)
	if d != "" && d != "No response requested." {
		return d
	}
	ev := m.work.views[a.Key].Events
	for i := len(ev) - 1; i >= 0; i-- {
		if ev[i].Kind == claude.EvTurn && ev[i].Run == "" {
			return oneLine(ev[i].Text)
		}
	}
	return ""
}

// ansiTrim cuts s to w cells, marking the cut.
func ansiTrim(s string, w int) string {
	if cellw.String(s) <= w {
		return s
	}
	return fit(s, w)
}

// workRun is a subagent under its session: what it last said, and what it
// is doing now if that came after.
func (m *Model) workRun(a *fleet.Agent, r claude.Run, w int, now time.Time) string {
	name := r.Name
	if name == "" {
		name = r.Type
	}
	indent := strings.Repeat("  ", r.Depth)
	var marker, what, when string
	switch {
	case running(r, a, now):
		marker = paint(cOrange, spinner[(m.tick+len(r.ID))%len(spinner)])
		what = paint(cText, oneLine(r.Said))
		if r.DoingAt.After(r.SaidAt) && r.Doing != "" {
			if r.Said == "" {
				what = paint(cSub, r.Doing)
			} else {
				what += dim(" · " + r.Doing)
			}
		}
		last := r.SaidAt
		if r.DoingAt.After(last) {
			last = r.DoingAt
		}
		when = age(now.Sub(last))
	default:
		marker = workEndMark(r.Status)
		what = paint(cSub, oneLine(r.Said))
		when = "ended " + age(now.Sub(r.EndedAt))
	}
	if what == "" {
		what = dim("starting")
	}
	tail := faint(right(when, 9))
	left := "   " + indent + marker + " " + paint(cSub, fit(oneLine(name), 30)) + " "
	return left + fit(what, w-cellw.String(left)-cellw.String(tail)-1) + " " + tail
}

func workEndMark(status string) string {
	switch status {
	case "failed":
		return paint(cRed, "✗")
	case "stopped":
		return paint(cYellow, "⏸")
	}
	return paint(cGreen, "✓")
}

// workEvent is one thing that happened: when, whose, and what.
func (m *Model) workEvent(a *fleet.Agent, e claude.Happening, w int) string {
	whoW := min(46, max(20, w/3))
	who := oneLine(a.DisplayName)
	if e.Run != "" {
		for _, r := range m.work.views[a.Key].Runs {
			if r.ID == e.Run {
				n := r.Name
				if n == "" {
					n = r.Type
				}
				// The subagent says more than its session, which the rows
				// around it name.
				sw := min(cellw.String(who), max(8, whoW*2/5))
				who = strings.TrimRight(ansiTrim(who, sw), " ") + " › " + oneLine(n)
			}
		}
	}
	var mark, what string
	text := oneLine(e.Text)
	switch e.Kind {
	case claude.EvPrompt:
		mark, what = paint(cBlue, "›"), paint(cBlue, "you: ")+paint(cText, text)
	case claude.EvTurn:
		mark, what = dim("●"), paint(cSub, text)
	case claude.EvPlan:
		n := fmt.Sprintf("planned %d task%s", e.N, plural(e.N))
		mark, what = dim("☐"), dim(n+": ")+paint(cSub, text)
	case claude.EvTick:
		mark, what = paint(cGreen, "✓"), paint(cText, text)
	case claude.EvStart:
		mark, what = paint(cBlue, "⇢"), dim("started ")+paint(cText, text)
	case claude.EvEnd:
		mark = workEndMark(e.Status)
		switch {
		case e.Run == "":
			mark, what = faint("·"), faint(text) // a background command
		case e.Status == "failed":
			what = paint(cRed, "failed: ") + paint(cText, text)
		case e.Status == "stopped":
			what = paint(cYellow, "stopped ") + paint(cSub, text)
		default:
			what = paint(cGreen, "done: ") + paint(cText, text)
		}
	case claude.EvAsk:
		mark, what = paint(cYellow, "?"), paint(cYellow, "asked: ")+paint(cText, text)
	case claude.EvCommit:
		mark, what = paint(cOrange, "⎇"), paint(cText, text)
	case claude.EvPR:
		mark, what = paint(cBlue, "⇡"), paint(cBlue, "opened ")+paint(cText, text)
	case claude.EvError:
		mark, what = paint(cRed, "✗"), paint(cRed, "stopped: ")+paint(cText, text)
	case claude.EvCompact:
		mark, what = faint("↺"), faint("context compacted")
	}
	left := "  " + faint(e.At.Local().Format("15:04")) + "  " + paint(cSub, fit(who, whoW)) + " " + mark + " "
	return left + fit(what, w-cellw.String(left))
}

// workPick finds the row picked, following it if rows moved; rows
// without an agent can't be picked.
func (m *Model) workPick(rows []workRow) int { return pickRow(rows, &m.work.pos, &m.work.sel) }

// pickRow finds the row picked, by its id at *sel, following it if rows
// moved, else the nearest row with an agent to *pos.
func pickRow(rows []workRow, pos *int, sel *string) int {
	if *pos < len(rows) && rows[*pos].id == *sel && rows[*pos].a != nil {
		return *pos
	}
	for i, r := range rows {
		if r.id == *sel && r.a != nil {
			*pos = i
			return i
		}
	}
	for d := 0; d < len(rows); d++ {
		for _, i := range []int{*pos + d, *pos - d} {
			if i >= 0 && i < len(rows) && rows[i].a != nil {
				*pos, *sel = i, rows[i].id
				return i
			}
		}
	}
	return -1
}

func (m *Model) workBody() []string {
	rows := m.workRows()
	pick := m.workPick(rows)
	w := min(m.w-4, 170)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.line
		if i == pick {
			out[i] = highlight(r.line, w)
		}
	}
	return out
}

func (m *Model) workSelected() *fleet.Agent {
	rows := m.workRows()
	if i := m.workPick(rows); i >= 0 {
		return rows[i].a
	}
	return nil
}

func (m *Model) workHint() string {
	pairs := []string{"↑↓", "move", "enter", "open", "f", "only this session", "alt+g", "keep going", "esc", "back"}
	if m.work.only != "" {
		pairs[5] = "everyone"
	}
	if a := m.workSelected(); a != nil && a.Halted() {
		pairs[7] = "continue"
	}
	return keysFit(m.w-4, pairs...)
}

func (m *Model) workKey(s string) tea.Cmd {
	rows := m.workRows()
	i := m.workPick(rows)
	var a *fleet.Agent
	if i >= 0 {
		a = rows[i].a
	}
	move := func(d int) {
		last := -1
		for j, n := i+sign(d), 0; i >= 0 && j >= 0 && j < len(rows); j += sign(d) {
			if rows[j].a != nil {
				last = j
				if n++; n == abs(d) {
					break
				}
			}
		}
		if last >= 0 {
			m.work.pos, m.work.sel = last, rows[last].id
		}
	}
	switch s {
	case "esc", "q", "left":
		m.setView(placeAgents)
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-10)
	case "pgdown":
		move(10)
	case "f":
		if m.work.only != "" || a == nil {
			m.work.only = ""
		} else {
			m.work.only = a.Key
		}
	case "enter", "right":
		if a != nil {
			m.setView(placeAgents)
			m.sel = a.Key
			m.rebuild()
			return m.focusPane(a)
		}
	case "alt+g", "g":
		return m.keepGoing(a)
	case "alt+d":
		return m.markDone(a)
	}
	return nil
}

func sign(d int) int {
	if d < 0 {
		return -1
	}
	return 1
}

// lanesLine is a finished turn still waiting on its background work.
func lanesLine(a *fleet.Agent) string {
	agents, other := 0, 0
	for _, b := range a.Background {
		if strings.HasPrefix(b, "agent\x00") {
			agents++
		} else {
			other++
		}
	}
	var parts []string
	if agents > 0 {
		parts = append(parts, fmt.Sprintf("%d subagent%s running", agents, plural(agents)))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d task%s running", other, plural(other)))
	}
	if len(parts) == 0 {
		return "background work running"
	}
	return strings.Join(parts, ", ")
}

func contextWindow(a *fleet.Agent) int64 {
	if strings.Contains(a.Spend.Model, "haiku") {
		return 200_000
	}
	return 1_000_000
}

// heavyTools are what an agent runs that is worth seeing from the outside:
// installs, builds, type checks, tests and dev servers.
var heavyTools = map[string]bool{"node": true, "pnpm": true, "npm": true, "npx": true, "bun": true, "yarn": true,
	"go": true, "tsc": true, "tsgo": true, "vitest": true, "jest": true, "haven": true, "cargo": true, "make": true, "python3": true, "docker": true}

// workHeavy lists the heavy work under a live session: what it is, how
// long it has run and the cpu it takes.
func (m *Model) workHeavy(a *fleet.Agent) []string {
	tab := m.snap.Table
	if tab == nil || a.PID == 0 || !(a.Live() || a.Busy()) {
		return nil
	}
	type heavy struct {
		what  string
		cpu   float64
		start time.Time
	}
	var found []heavy
	seen := map[string]bool{}
	for _, n := range tab.Tree(a.PID) {
		if n.Depth == 0 || !heavyTools[n.Comm] {
			continue
		}
		what := m.heavyWhat(n.PID, n.Start, n.Comm)
		if what == "" || seen[what] {
			continue // one line per kind of work
		}
		seen[what] = true
		found = append(found, heavy{what, n.CPU, n.Start})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].cpu > found[j].cpu })
	var out []string
	now := m.snap.At
	for i, h := range found {
		if i == 3 {
			out = append(out, faint(fmt.Sprintf("…and %d more", len(found)-3)))
			break
		}
		cpu := dim(fmt.Sprintf("%.0f%%", h.cpu))
		if h.cpu >= 100 {
			cpu = paint(cYellow, fmt.Sprintf("%.0f%%", h.cpu))
		}
		out = append(out, paint(cOrange, "⚙ ")+paint(cText, h.what)+dim(" · "+age(now.Sub(h.start))+" · ")+cpu)
	}
	return out
}

// heavyWhat names a heavy process from its command line ("pnpm install",
// "vitest", "tsc -b"), or "" for one that isn't doing heavy work. Command
// lines cost a syscall, so each is looked up once.
func (m *Model) heavyWhat(pid int, start time.Time, comm string) string {
	if m.work.args == nil {
		m.work.args = map[string]args{}
	}
	k := fmt.Sprintf("%d@%d", pid, start.Unix())
	e, ok := m.work.args[k]
	if !ok {
		e = args{line: proc.CommandLine(pid), at: m.snap.At}
		m.work.args[k] = e
		if len(m.work.args) > 2000 {
			for key, v := range m.work.args {
				if m.snap.At.Sub(v.at) > time.Hour {
					delete(m.work.args, key)
				}
			}
		}
	}
	line := e.line
	if line == "" {
		line = comm
	}
	for _, kind := range []struct{ match, name string }{
		{" install", "install"}, {" i ", "install"}, {"typecheck", "typecheck"}, {"tsc", "tsc"}, {"tsgo", "tsgo"},
		{"vitest", "vitest"}, {"jest", "jest"}, {"playwright", "playwright"}, {"go test", "go test"}, {"go build", "go build"},
		{" build", "build"}, {" dev", "dev server"}, {"haven", "haven"}, {"lint", "lint"}, {"docker", "docker"},
	} {
		if strings.Contains(line+" ", kind.match) {
			tool := filepath.Base(strings.Fields(line)[0])
			if tool == "node" && len(strings.Fields(line)) > 1 {
				tool = filepath.Base(strings.Fields(line)[1])
			}
			if strings.Contains(tool, kind.name) || kind.name == tool {
				return kind.name
			}
			return tool + " " + kind.name
		}
	}
	return ""
}
