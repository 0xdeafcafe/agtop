package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

// The Workstreams place: what is happening across every repo, said the way
// you ask about it. Top, what is waiting on you: questions, turns that died
// on an error, and turns that finished without asking. Below, each repo:
// what its sessions are at, the last count they reported moving, how full
// their context is, and what heavy work is running in the checkout.

type workState struct {
	// pos is the row picked; an agent can be on two (waiting, and in its
	// repo). sel is who that was, so a reshuffle keeps them picked.
	pos  int
	sel  string
	args map[string]args // command lines looked up, by pid and start
}

// workPick finds the row picked in order, following its agent if the rows
// moved.
func (m *Model) workPick(order []*fleet.Agent) int {
	w := &m.work
	if len(order) == 0 {
		return -1
	}
	if w.pos < len(order) && order[w.pos].Key == w.sel {
		return w.pos
	}
	for i, a := range order {
		if a.Key == w.sel {
			w.pos = i
			return i
		}
	}
	w.pos = min(w.pos, len(order)-1)
	w.sel = order[w.pos].Key
	return w.pos
}

type args struct {
	line string
	at   time.Time
}

// workAgents are the sessions worth a row: open, busy or finished within
// a day, not put away, and not a subagent.
func (m *Model) workAgents() []*fleet.Agent {
	now := m.snap.At
	var out []*fleet.Agent
	for _, a := range m.snap.Agents {
		if a.Done || a.Past {
			continue
		}
		if a.Live() || a.Busy() || a.PID != 0 || now.Sub(a.UpdatedAt) < 24*time.Hour {
			out = append(out, a)
		}
	}
	return out
}

// waitingOnYou is what can't go on without you, the most stuck first:
// turns that died, then questions, then turns that finished.
func (m *Model) waitingOnYou(agents []*fleet.Agent) []*fleet.Agent {
	now := m.snap.At
	rank := func(a *fleet.Agent) int {
		switch {
		case a.Halted():
			return 0
		case a.NeedsYou() || a.Waiting():
			return 1
		case a.YourTurn(now):
			return 2
		}
		return -1
	}
	var out []*fleet.Agent
	for _, a := range agents {
		if rank(a) >= 0 {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].UpdatedAt.Before(out[j].UpdatedAt) // the longest waiting first
	})
	return out
}

// workOrder is every selectable row, top to bottom.
func (m *Model) workOrder() []*fleet.Agent {
	agents := m.workAgents()
	order := m.waitingOnYou(agents)
	for _, g := range m.workRepos(agents) {
		order = append(order, g.agents...)
	}
	return order
}

type workRepo struct {
	name, path string
	agents     []*fleet.Agent
	recent     time.Time
}

// workRepos groups sessions by repo, the busiest first.
func (m *Model) workRepos(agents []*fleet.Agent) []workRepo {
	by := map[string]*workRepo{}
	for _, a := range agents {
		g := by[a.Repo]
		if g == nil {
			name := filepath.Base(a.Repo)
			if a.Repo == "" {
				name = "No repository"
			}
			g = &workRepo{name: name, path: a.Repo}
			by[a.Repo] = g
		}
		g.agents = append(g.agents, a)
		if a.UpdatedAt.After(g.recent) {
			g.recent = a.UpdatedAt
		}
	}
	out := make([]workRepo, 0, len(by))
	for _, g := range by {
		sort.SliceStable(g.agents, func(i, j int) bool {
			li, lj := g.agents[i].Live() || g.agents[i].Busy(), g.agents[j].Live() || g.agents[j].Busy()
			if li != lj {
				return li
			}
			return g.agents[i].UpdatedAt.After(g.agents[j].UpdatedAt)
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].recent.After(out[j].recent) })
	return out
}

func (m *Model) workBody() []string {
	w := min(m.w-4, 170)
	now := m.snap.At
	agents := m.workAgents()
	pick := m.workPick(m.workOrder())
	row := 0
	next := func() bool { row++; return row-1 == pick }
	var out []string
	waiting := m.waitingOnYou(agents)
	meta := "nothing"
	if n := len(waiting); n > 0 {
		meta = fmt.Sprint(n)
	}
	out = append(out, rule("Waiting on you", meta, w), "")
	for _, a := range waiting {
		out = append(out, m.workWaitingRow(a, w, next()))
	}
	if len(waiting) == 0 {
		out = append(out, dim("  every session is working, or put away"))
	}
	for _, g := range m.workRepos(agents) {
		out = append(out, "", rule(g.name, m.workRepoMeta(g), w))
		for _, p := range m.workHeavy(g) {
			out = append(out, "  "+p)
		}
		out = append(out, "")
		for _, a := range g.agents {
			out = append(out, m.workRow(a, w, now, next()))
		}
	}
	return out
}

// workWaitingRow is one thing waiting on you: what it is, what it said,
// how long it has waited, and the key that answers it.
func (m *Model) workWaitingRow(a *fleet.Agent, w int, sel bool) string {
	now := m.snap.At
	var marker, what, key string
	switch {
	case a.Halted():
		marker, what, key = paint(cRed, "✗"), paint(cRed, "stopped · "+oneLine(a.HaltReason())), "alt+g continue"
	case a.NeedsYou() || a.Waiting():
		q := oneLine(a.Needs)
		if q == "" {
			q = oneLine(a.Detail)
		}
		marker, what, key = paint(cYellow, "?"), paint(cText, q), "enter answer"
	default:
		d := oneLine(a.Detail)
		if d == "" {
			d = "finished its turn"
		}
		marker, what, key = paint(cGreen, "◆"), paint(cSub, d), "alt+g keep going"
	}
	tail := faint(right(age(now.Sub(a.UpdatedAt)), 6))
	if sel {
		tail = dim(right(key, 18)) + tail
	}
	left := " " + marker + " " + paint(cText+bold, fit(oneLine(a.DisplayName), 24)) + "  "
	line := left + fit(what, w-cellw.String(left)-cellw.String(tail)-1) + " " + tail
	if sel {
		return highlight(line, w)
	}
	return line
}

// workRepoMeta says how many sessions share the repo, and warns when more
// than one works in the same checkout, where they tread on each other.
func (m *Model) workRepoMeta(g workRepo) string {
	running, shared := 0, 0
	for _, a := range g.agents {
		if a.Live() || a.Busy() {
			running++
		}
		if (a.Live() || a.Busy()) && g.path != "" && !strings.Contains(a.Cwd, "/.claude/worktrees/") && strings.HasPrefix(a.Cwd, g.path) {
			shared++
		}
	}
	meta := fmt.Sprintf("%d session%s · %d running", len(g.agents), plural(len(g.agents)), running)
	if shared > 1 {
		meta += paint(cYellow, fmt.Sprintf(" · %d in one checkout", shared))
	}
	return meta
}

// workRow is a session in its repo: its state, the last count it reported
// moving, how full its context is and its PR.
func (m *Model) workRow(a *fleet.Agent, w int, now time.Time, sel bool) string {
	marker := dim("◦")
	state := dim("idle " + age(now.Sub(a.UpdatedAt)))
	switch {
	case a.Halted():
		marker, state = paint(cRed, "✗"), paint(cRed, "stopped · "+oneLine(a.HaltReason()))
	case a.NeedsYou() || a.Waiting():
		marker, state = paint(cYellow, "?"), paint(cYellow, "asking you")
	case a.YourTurn(now):
		marker, state = paint(cGreen, "◆"), paint(cGreen, "your turn")
	case a.Live():
		marker = paint(cOrange, spinner[(m.tick+len(a.ID))%len(spinner)])
		state = oneLine(a.Detail)
		if p := m.previews[a.Key].p; p.Doing != "" {
			state = oneLine(p.Doing)
		}
		if state == "" {
			state = "working"
		}
		state = paint(cText, state)
	case a.Busy():
		marker, state = paint(cBlue, "◎"), paint(cBlue, lanesLine(a))
	case a.PID == 0:
		marker, state = faint("·"), faint("ended "+age(now.Sub(a.UpdatedAt))+" ago")
	}
	if n := a.Subagents; n > 0 && !a.Busy() {
		state += dim(fmt.Sprintf(" · %d lane%s running", n, plural(n)))
	}

	const ctxW, prW, ageW = 24, 10, 6
	progW := min(46, max(24, w/3))
	prog := ""
	if p := a.Spend.Progress; p != "" && now.Sub(a.Spend.ProgressAt) < 24*time.Hour {
		prog = paint(cSub, fit(p, progW-8)) + faint(right(age(now.Sub(a.Spend.ProgressAt)), 7))
	}
	ctx := ""
	if a.Spend.Context > 0 {
		pc := int(100 * a.Spend.Context / contextWindow(a))
		c := dim
		if pc >= 80 {
			c = func(s string) string { return paint(cYellow, s) }
		}
		ctx = c(fmt.Sprintf("ctx %d%%", pc))
		if a.Spend.Compacts > 0 {
			ctx += faint(fmt.Sprintf(" · %d× compacted", a.Spend.Compacts))
		}
	}
	pr := ""
	if len(a.PRs) > 0 {
		p := a.PRs[len(a.PRs)-1]
		pr = dim(fmt.Sprintf("#%d", p.Number))
		switch {
		case p.Checks.Failed > 0:
			pr += paint(cRed, " ✗")
		case p.Checks.Pending > 0:
			pr += paint(cYellow, " ◌")
		case p.Checks.Passed > 0:
			pr += paint(cGreen, " ✓")
		}
	}
	left := " " + marker + " " + paint(cText, fit(oneLine(a.DisplayName), 24)) + "  "
	// Narrow, the columns go least useful first: the PR, the context, then
	// the progress, so the state always has room.
	cols := []string{fit(prog, progW), fit(ctx, ctxW), fit(pr, prW)}
	var tail string
	room := 0
	for n := len(cols); n >= 0; n-- {
		tail = strings.Join(cols[:n], "") + faint(right(age(now.Sub(a.UpdatedAt)), ageW))
		if room = w - cellw.String(left) - cellw.String(tail) - 1; room >= 24 {
			break
		}
	}
	line := left + fit(state, room) + " " + tail
	if sel {
		return highlight(line, w)
	}
	return line
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
		parts = append(parts, fmt.Sprintf("%d lane%s running", agents, plural(agents)))
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

// workHeavy lists the heavy work under a repo's live sessions: what it is,
// whose it is, how long it has run and the cpu it takes.
func (m *Model) workHeavy(g workRepo) []string {
	tab := m.snap.Table
	if tab == nil {
		return nil
	}
	type heavy struct {
		what, who string
		cpu       float64
		start     time.Time
	}
	var found []heavy
	seen := map[string]bool{}
	for _, a := range g.agents {
		if a.PID == 0 || !(a.Live() || a.Busy()) {
			continue
		}
		for _, n := range tab.Tree(a.PID) {
			if n.Depth == 0 || !heavyTools[n.Comm] {
				continue
			}
			what := m.heavyWhat(n.PID, n.Start, n.Comm)
			if what == "" {
				continue
			}
			k := a.Key + what
			if seen[k] {
				continue // one line per kind of work per agent
			}
			seen[k] = true
			found = append(found, heavy{what, oneLine(a.DisplayName), n.CPU, n.Start})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].cpu > found[j].cpu })
	var out []string
	now := m.snap.At
	for i, h := range found {
		if i == 4 {
			out = append(out, faint(fmt.Sprintf("  …and %d more", len(found)-4)))
			break
		}
		cpu := dim(fmt.Sprintf("%.0f%%", h.cpu))
		if h.cpu >= 100 {
			cpu = paint(cYellow, fmt.Sprintf("%.0f%%", h.cpu))
		}
		out = append(out, paint(cOrange, "⚙ ")+paint(cText, h.what)+dim(" · "+h.who+" · "+age(now.Sub(h.start))+" · ")+cpu)
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

func (m *Model) workHint() string {
	pairs := []string{"↑↓", "move", "enter", "open", "alt+g", "keep going", "alt+d", "done", "esc", "back"}
	if a := m.workSelected(); a != nil && a.Halted() {
		pairs[5] = "continue"
	}
	return keysFit(m.w-4, pairs...)
}

func (m *Model) workSelected() *fleet.Agent {
	order := m.workOrder()
	if i := m.workPick(order); i >= 0 {
		return order[i]
	}
	return nil
}

func (m *Model) workKey(s string) tea.Cmd {
	order := m.workOrder()
	i := m.workPick(order)
	a := m.workSelected()
	move := func(d int) {
		if i >= 0 {
			m.work.pos = roundMove(i, d, len(order))
			m.work.sel = order[m.work.pos].Key
		}
	}
	switch s {
	case "esc", "q", "left":
		m.setView(placeAgents)
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
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
