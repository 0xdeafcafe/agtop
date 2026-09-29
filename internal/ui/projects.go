package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// The Agents place's Projects page: each repository an agent worked in
// over the last day, whole. Its branch and how far it is from pushed,
// where it pushes to, its last commits, the pull requests its agents
// opened, and every linked worktree with its own branch and changes, each
// with the agents working in it. Folders that aren't repositories come
// last.
//
// A project shows only its head until the cursor is on it or inside it;
// then it opens to its worktrees and agents. ↑↓ go from head to head, and
// within a project once enter has stepped into it. What's safe to remove
// is said on the worktree or agent it belongs to, and processes no project
// owns (orphans, and Claude processes that aren't agents) sit in a System
// section at the end.

// project is one repository on the page, and its agents by worktree ("" for
// the main checkout).
type project struct {
	key, title string
	agents     []*fleet.Agent
	open       bool // an agent in it is running
}

func (m *Model) projects() []*project {
	byKey := map[string]*project{}
	keys := map[string]bool{}
	for _, a := range m.workAgents() {
		k := folderKey(a)
		p := byKey[k]
		if p == nil {
			p = &project{key: k}
			byKey[k] = p
		}
		p.agents = append(p.agents, a)
		p.open = p.open || a.Open() || a.Busy()
		keys[k] = true
	}
	titles := folderTitles(keys)
	out := make([]*project, 0, len(byKey))
	for _, p := range byKey {
		p.title = titles[p.key]
		sort.SliceStable(p.agents, func(i, j int) bool {
			a, b := p.agents[i], p.agents[j]
			if t, u := treeOf(a), treeOf(b); t != u {
				return t < u
			}
			return m.sortLess(a, b)
		})
		out = append(out, p)
	}
	// Repositories before other folders; those with an agent running
	// first; then by name, so a project keeps its place.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := filepath.IsAbs(a.key) && m.rootIsRepo(a.key), filepath.IsAbs(b.key) && m.rootIsRepo(b.key); ra != rb {
			return ra
		}
		if a.open != b.open {
			return a.open
		}
		return cmpLower(a.title, b.title) < 0
	})
	return out
}

// worktreeAt is what the clean-up scan knows of the worktree at path, or a
// bare unchecked one before it has looked.
func (m *Model) worktreeAt(path string) fleet.Worktree {
	for _, w := range m.clean.wts {
		if w.Path == path {
			return w
		}
	}
	return fleet.Worktree{Path: path}
}

// openProject is the project the cursor is on or inside, the one shown
// open.
func (m *Model) openProject(list []*project) string {
	sel := m.work.projSel
	switch {
	case strings.HasPrefix(sel, "p"):
		return strings.TrimPrefix(sel, "p")
	case strings.HasPrefix(sel, "a"):
		if a := m.agentByKey(strings.TrimPrefix(sel, "a")); a != nil {
			return folderKey(a)
		}
	case strings.HasPrefix(sel, "w"):
		for _, p := range list {
			for _, t := range projectTrees(p, m.folders.byRoot[p.key]) {
				if "w"+t == sel {
					return p.key
				}
			}
		}
	}
	return ""
}

func (m *Model) projectRows() []workRow {
	w := min(m.w-4, 170)
	now := m.snap.At
	var rows []workRow
	text := func(s string) { rows = append(rows, workRow{line: fit(s, w)}) }
	agentRow := func(a *fleet.Agent, indent string) {
		line := indent + m.workSession(a, w-len(indent), now)
		if a.Temp >= tempShown {
			line = fit(line+"  "+m.tempTail(a, now), w)
		}
		rows = append(rows, workRow{id: "a" + a.Key, a: a, owner: folderKey(a), line: line})
	}
	list := m.projects()
	if len(list) == 0 {
		text(dim("  no agent has worked anywhere in the last day"))
	}
	open := m.openProject(list)
	for _, p := range list {
		f := m.folders.byRoot[p.key]
		head := m.projectHead(p, w)
		rows = append(rows, workRow{id: "p" + p.key, proj: p, line: fit(head[0], w)})
		for _, l := range head[1:] {
			text(l)
		}
		if p.key != open {
			text("")
			continue
		}
		// The main checkout's agents, then each worktree's, every one
		// the repository has once it's known whole.
		for _, a := range p.agents {
			if treeOf(a) == "" {
				agentRow(a, "  ")
			}
		}
		for _, t := range projectTrees(p, f) {
			n := 0
			for _, a := range p.agents {
				if treeOf(a) == t {
					n++
				}
			}
			wt := m.worktreeAt(t)
			rows = append(rows, workRow{id: "w" + t, wt: &wt, owner: p.key, line: m.worktreeRow(t, f, wt, n, w, now)})
			for _, a := range p.agents {
				if treeOf(a) == t {
					agentRow(a, "    ")
				}
			}
		}
		text("")
	}
	return append(rows, m.systemRows(w, now)...)
}

// projectHead is what heads a project: its name and what its agents are
// doing, where it is and pushes to, what git says of it, its last commits
// and its agents' pull requests.
func (m *Model) projectHead(p *project, w int) []string {
	now := m.snap.At
	f, known := m.folders.byRoot[p.key]
	head := paint(cText+bold, p.title) + "  " + folderMeta(p.agents, now)
	out := []string{head + " " + faint(strings.Repeat("─", max(0, w-cellw.String(head)-1)))}
	if filepath.IsAbs(p.key) {
		where := faint(tildify(p.key))
		if f.Remote != "" {
			where = where + dim("  ·  ") + paint(cSub, f.Remote)
		}
		out = append(out, "  "+where)
	}
	switch {
	case !filepath.IsAbs(p.key) || !m.rootIsRepo(p.key):
	case !known:
		out = append(out, "  "+faint("asking git…"))
	default:
		g := gitBits(f.Git)
		if f.Git.Branch != "" && !f.Git.Upstream && f.Git.Err == "" {
			g = g + dim(" · ") + faint("not pushed anywhere")
		}
		out = append(out, "  "+g)
		for _, c := range f.Recent {
			out = append(out, "  "+faint("● ")+dim(right(age(now.Sub(c.At)), 4)+"  ")+paint(cSub, c.Subject))
		}
	}
	for _, pr := range projectPRs(p.agents) {
		out = append(out, "  "+prLine(pr, w-2))
	}
	return out
}

// projectTrees are a project's linked worktrees: those its agents are in,
// then the rest git lists.
func projectTrees(p *project, f fleet.Folder) []string {
	var trees []string
	seen := map[string]bool{}
	for _, a := range p.agents {
		if t := treeOf(a); t != "" && !seen[t] {
			seen[t] = true
			trees = append(trees, t)
		}
	}
	for _, t := range f.Linked {
		if !seen[t] {
			seen[t] = true
			trees = append(trees, t)
		}
	}
	return trees
}

// worktreeRow is a worktree on the Projects page: its name, what git says of
// it, whether any agent is in it, and whether it can go without losing
// anything.
func (m *Model) worktreeRow(t string, f fleet.Folder, wt fleet.Worktree, agents, w int, now time.Time) string {
	var b strings.Builder
	b.WriteString("  " + faint("⎇ ") + paint(cSub, filepath.Base(t)))
	if st, ok := f.Trees[t]; ok {
		b.WriteString("  " + gitBits(st) + baseShort(st))
	}
	if agents == 0 {
		b.WriteString(dim(" · ") + faint("no agent"))
	}
	if wt.Size > 0 {
		b.WriteString("  " + faint(disk(wt.Size)))
	}
	var mark, status string
	switch {
	case wt.Checked.IsZero():
		mark, status = faint("·"), dim("not looked at yet")
	case wt.Err != "":
		mark, status = paint(cRed, "!"), paint(cRed, wt.Err)
	case !wt.Safe():
		mark, status = paint(cYellow, "✗"), paint(cYellow, "would lose "+wt.Losses())
	case m.running(wt.Agents) != nil:
		mark, status = paint(cGreen, "✓"), dim("clean and pushed")
	default:
		mark = paint(cGreen, "✓")
		switch due := m.dueIn(wt.Agents, now); {
		case due == 0:
			status = paint(cGreen, "clean and pushed · goes at the next tidy-up")
		case due > 0:
			status = paint(cGreen, "clean and pushed") + dim(" · goes in "+dur(due.Round(time.Minute)))
		case agents == 0:
			status = paint(cGreen, "clean and pushed") + dim(" · x removes it")
		default:
			status = paint(cGreen, "clean and pushed") + dim(" · goes once its agents are done")
		}
	}
	return fit(b.String()+"   "+mark+" "+status, w)
}

// tempTail is an agent's temp work, on its row once there's enough of it to
// mention: how much, and when it goes.
func (m *Model) tempTail(a *fleet.Agent, now time.Time) string {
	size := disk(a.Temp) + " temp"
	switch due := m.dueIn([]string{a.Key}, now); {
	case a.PID != 0:
		return dim(size)
	case due == 0:
		return paint(cGreen, size+" · goes at the next tidy-up")
	case due > 0:
		return dim(size + " · goes in " + dur(due.Round(time.Minute)))
	default:
		return dim(size + " · x removes it")
	}
}

// procRow is a process in the System section.
type procRow struct {
	pid, depth, n int
	label, cmd    string
	mem           uint64
	cpu           float64
	start         time.Time
	role          fleet.Role
	other         bool // a Claude process of no agent, not an orphan's child
}

// systemRows are the processes no project owns: orphans first, with the few
// processes holding most of each one's memory, then the Claude processes
// that belong to no agent.
func (m *Model) systemRows(w int, now time.Time) []workRow {
	tab := m.snap.Table
	if tab == nil {
		return nil
	}
	var procs []procRow
	for _, r := range m.snap.Machine.Rows {
		if r.Role != fleet.RoleOrphan {
			continue
		}
		procs = append(procs, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, other: true})
		kids := tab.Tree(r.PID)[1:]
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Footprint > kids[j].Footprint })
		for _, n := range kids[:min(3, len(kids))] {
			if n.Footprint >= 32<<20 {
				procs = append(procs, procRow{pid: n.PID, depth: 1, cmd: m.shortCmd(n.PID, n.Comm), mem: n.Footprint, cpu: n.CPU, n: 1, start: n.Start, role: fleet.RoleOrphan})
			}
		}
	}
	for _, r := range m.snap.Machine.Rows {
		if r.Role == fleet.RoleWorker || r.Role == fleet.RoleOrphan {
			continue
		}
		procs = append(procs, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, other: true})
	}
	if len(procs) == 0 {
		return nil
	}
	const cpuW, memW, procW = 9, 8, 8
	left := max(20, w-2-cpuW-memW-procW)
	cols := func(r procRow) string {
		p := ""
		if r.n > 1 {
			p = strconv.Itoa(r.n)
		}
		return cpuColor(r.cpu, right(fmt.Sprintf("%.1f%%", r.cpu), cpuW)) + memColor(r.mem, right(mem(r.mem), memW)) + dim(right(p, procW))
	}
	rows := []workRow{{line: rule("System", "processes no project owns", w)}}
	if mc := m.snap.Machine; mc.Orphans > 0 {
		rows = append(rows, workRow{line: fit(paint(cYellow+bold, fmt.Sprintf("  %d orphaned", mc.Orphans))+paint(cYellow, " · holding "+mem(mc.OrphanMem))+
			dim(" · their sessions ended; they run until you end them · ")+paint(cOrange, "x")+dim(" ends one, ")+paint(cOrange, "X")+dim(" all"), w)})
	}
	lastRole := fleet.Role(-1)
	for _, r := range procs {
		switch {
		case r.other && r.role == fleet.RoleOrphan:
			lbl := fit(r.label+" · "+age(now.Sub(r.start))+" old", 30)
			line := "  " + paint(cYellow, lbl) + paint(cText, fit(trimCmd(r.cmd, left-32), left-32)) + cols(r)
			rows = append(rows, workRow{id: "s" + strconv.Itoa(r.pid), proc: &r, line: line})
		case r.other:
			if r.role != lastRole {
				lastRole = r.role
				rows = append(rows, workRow{line: dim("  " + roleName(r.role))})
			}
			line := "    " + paint(cSub, fit(r.label, 30)) + faint(fit(trimCmd(r.cmd, left-34), left-34)) + cols(r)
			rows = append(rows, workRow{id: "s" + strconv.Itoa(r.pid), proc: &r, line: line})
		default:
			line := "  " + strings.Repeat(" ", 30) + faint("└ "+fit(trimCmd(r.cmd, left-34), left-34)) + cols(r)
			rows = append(rows, workRow{line: line})
		}
	}
	return rows
}

// endProc asks before ending a System process: an orphan's whole tree, or
// one process with SIGTERM.
func (m *Model) endProc(r procRow) {
	if r.role == fleet.RoleOrphan && r.other {
		m.confirm = &confirmation{
			question: fmt.Sprintf("End %s and everything under it, freeing about %s?", trimCmd(r.cmd, 40), mem(r.mem)),
			detail:   "SIGTERM, then SIGKILL after 3s",
			onYes:    func() tea.Cmd { return endOrphans([]procRow{r}) },
			bangText: "SIGKILL it now",
			onBang:   killTree(r.pid, r.start),
		}
		return
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("Send SIGTERM to %d?", r.pid),
		detail:   trimCmd(r.cmd, 80),
		onYes: func() tea.Cmd {
			return cmdErr(fmt.Sprintf("sent SIGTERM to %d", r.pid), func() error { return actions.Terminate(r.pid) })
		},
		bangText: "SIGKILL it and everything under it",
		onBang:   killTree(r.pid, r.start),
	}
}

// projectPRs are the pull requests a project's agents opened or pushed
// to, each once, open ones first.
func projectPRs(agents []*fleet.Agent) []agent.PR {
	var out []agent.PR
	seen := map[string]bool{}
	for _, a := range agents {
		for _, pr := range a.PRs {
			k := pr.URL
			if k == "" {
				k = strconv.Itoa(pr.Number)
			}
			if !seen[k] {
				seen[k] = true
				out = append(out, pr)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].State == "OPEN" && out[j].State != "OPEN" })
	return out
}

// prLine is a pull request in a line: number, state, checks and title.
func prLine(pr agent.PR, w int) string {
	col := cGreen
	switch {
	case pr.State == "MERGED":
		col = cBlue
	case pr.State == "CLOSED" || pr.State == "unknown":
		col = cDim
	case pr.Checks.Failed > 0:
		col = cRed
	}
	s := paint(col, "⇡ #"+strconv.Itoa(pr.Number)) + " " + dim(strings.ToLower(pr.State))
	if c := pr.Checks; c.Passed+c.Failed+c.Pending > 0 {
		s += "  " + paint(cGreen, fmt.Sprintf("%d✓", c.Passed))
		if c.Failed > 0 {
			s += " " + paint(cRed, fmt.Sprintf("%d✗", c.Failed))
		}
		if c.Pending > 0 {
			s += " " + dim(fmt.Sprintf("%d…", c.Pending))
		}
	}
	if pr.Review != "" {
		s += dim(" · " + strings.ToLower(strings.ReplaceAll(pr.Review, "_", " ")))
	}
	return s + "  " + paint(cText, fit(pr.Title, max(10, w-cellw.String(s)-2)))
}

// projectsSummary heads the page: the machine's load, and how much can go
// without losing anything.
func (m *Model) projectsSummary(w int) string {
	mc := m.snap.Machine
	var safe int64
	for _, r := range m.cleanRows() {
		switch {
		case r.wt != nil && r.wt.Safe() && m.running(r.wt.Agents) == nil:
			safe += r.wt.Size
		case r.agent != nil && r.agent.PID == 0:
			safe += r.agent.Temp
		}
	}
	s := dim(fmt.Sprintf("%s ram · %.0f%% cpu", mem(mc.TotalMem), mc.TotalCPU))
	switch {
	case m.clean.checking && m.clean.checked.IsZero():
		s += dim(" · ") + paint(cOrange, spinner[m.tick%len(spinner)]) + dim(" looking at worktrees with git…")
	case safe > 0:
		s += dim(" · ") + paint(cGreen, disk(safe)) + dim(" can go without losing anything (A)")
	}
	return fit(s, w)
}

func (m *Model) projectsBody() []string {
	rows := m.projectRows()
	pick := pickRow(rows, &m.work.projPos, &m.work.projSel)
	w := min(m.w-4, 170)
	out := make([]string, 0, len(rows)+2)
	out = append(out, m.projectsSummary(w), "")
	for i, r := range rows {
		line := r.line
		if i == pick {
			line = highlight(line, w)
		}
		out = append(out, line)
	}
	return out
}

func (m *Model) projectsHint() string {
	rows := m.projectRows()
	var r workRow
	if i := pickRow(rows, &m.work.projPos, &m.work.projSel); i >= 0 {
		r = rows[i]
	}
	w := m.w - 4
	switch {
	case r.proc != nil:
		k := []string{"↑↓", "move", "x", "end", "!", "SIGKILL tree"}
		if m.snap.Machine.Orphans > 0 {
			k = append(k, "X", "end all orphans")
		}
		return keysFit(w, append(k, "esc", "back")...)
	case r.wt != nil:
		return keysFit(w, "↑↓", "move", "x", "remove", "A", "remove all that's safe", "r", "check again", "←", "the project", "esc", "back")
	case r.proj != nil:
		return keysFit(w, "↑↓", "move", "enter", "into it", "A", "remove all that's safe", "[ ]", "pages", "esc", "back")
	}
	k := []string{"↑↓", "move", "enter", "open", "ctrl+y", "its PR", "alt+g", "keep going"}
	if r.a != nil && r.a.Temp >= tempShown && r.a.PID == 0 {
		k = append(k, "x", "remove temp work")
	}
	return keysFit(w, append(k, "←", "the project", "esc", "back")...)
}

func (m *Model) projectsKey(s string) tea.Cmd {
	rows := m.projectRows()
	i := pickRow(rows, &m.work.projPos, &m.work.projSel)
	var r workRow
	if i >= 0 {
		r = rows[i]
	}
	// Up and down stay at the level the cursor is at: from project to
	// project over their heads, or within the one it's inside. Only the
	// project under the cursor is open, so crossing into the next from
	// inside would fold everything above the cursor away.
	move := func(d int) {
		for j, n := i+sign(d), 0; i >= 0 && j >= 0 && j < len(rows); j += sign(d) {
			if rows[j].pickable() && rows[j].owner == r.owner {
				m.work.projPos, m.work.projSel = j, rows[j].id
				if n++; n == abs(d) {
					break
				}
			}
		}
	}
	switch s {
	case "esc", "q":
		m.setView(placeAgents)
	case "left":
		// Out of a project to its head; from a head, out of Projects.
		if r.owner == "" {
			m.setView(placeAgents)
			return nil
		}
		for j, h := range rows {
			if h.id == "p"+r.owner {
				m.work.projPos, m.work.projSel = j, h.id
			}
		}
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-10)
	case "pgdown":
		move(10)
	case "enter", "right":
		switch {
		case r.proj != nil:
			// The project is already open under the cursor: step into it.
			if i+1 < len(rows) {
				for j := i + 1; j < len(rows); j++ {
					if rows[j].pickable() && rows[j].owner == r.proj.key {
						m.work.projPos, m.work.projSel = j, rows[j].id
						break
					}
				}
			}
		case r.a != nil:
			m.setView(placeAgents)
			m.sel = r.a.Key
			m.rebuild()
			return m.focusPane(r.a)
		case r.wt != nil:
			m.askRemoveWorktree(*r.wt)
		}
	case "ctrl+y":
		return m.openPR(r.a)
	case "alt+g", "g":
		return m.keepGoing(r.a)
	case "x", "ctrl+x", "backspace", "delete":
		switch {
		case r.wt != nil:
			m.askRemoveWorktree(*r.wt)
		case r.proc != nil:
			m.endProc(*r.proc)
		case r.a != nil && r.a.Temp >= tempShown:
			m.askClean(r.a)
		}
	case "!":
		if p := r.proc; p != nil {
			m.confirm = &confirmation{
				question: fmt.Sprintf("SIGKILL %d and everything under it?", p.pid),
				detail:   trimCmd(p.cmd, 80),
				onYes:    killTree(p.pid, p.start),
			}
		}
	case "X":
		if mc := m.snap.Machine; mc.Orphans > 0 {
			var ends []procRow
			for _, o := range rows {
				if o.proc != nil && o.proc.role == fleet.RoleOrphan && o.proc.other {
					ends = append(ends, *o.proc)
				}
			}
			m.confirm = &confirmation{
				question: fmt.Sprintf("End all %d orphaned process trees and free about %s?", len(ends), mem(mc.OrphanMem)),
				detail:   "SIGTERM, then SIGKILL after 3s",
				onYes:    func() tea.Cmd { return endOrphans(ends) },
			}
		}
	case "A":
		m.askCleanSafe(m.cleanRows())
	case "r":
		return m.scanWorktrees()
	}
	return nil
}
