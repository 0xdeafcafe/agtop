package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// The Overview's Projects page: each repository an agent worked in over
// the last day, whole. Its branch and how far it is from pushed, where it
// pushes to, its last commits, the pull requests its agents opened, and
// every linked worktree with its own branch and changes, each with the
// agents working in it. Folders that aren't repositories come last.

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

func (m *Model) projectRows() []workRow {
	w := min(m.w-4, 170)
	now := m.snap.At
	var rows []workRow
	text := func(s string) { rows = append(rows, workRow{line: fit(s, w)}) }
	agent := func(a *fleet.Agent, indent string) {
		rows = append(rows, workRow{id: "a" + a.Key, a: a, line: indent + m.workSession(a, w-len(indent), now)})
	}
	list := m.projects()
	if len(list) == 0 {
		text(dim("  no agent has worked anywhere in the last day"))
		return rows
	}
	for _, p := range list {
		f := m.folders.byRoot[p.key]
		for _, l := range m.projectHead(p, w) {
			text(l)
		}
		// The main checkout's agents, then each worktree's, every one
		// the repository has once it's known whole.
		for _, a := range p.agents {
			if treeOf(a) == "" {
				agent(a, "  ")
			}
		}
		for _, t := range projectTrees(p, f) {
			n := 0
			for _, a := range p.agents {
				if treeOf(a) == t {
					n++
				}
			}
			text(treeHead(t, f, n))
			for _, a := range p.agents {
				if treeOf(a) == t {
					agent(a, "    ")
				}
			}
		}
		text("")
	}
	return rows
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

// treeHead heads a worktree's agents on the Projects page: its name, what
// git says of it, and whether any agent is in it.
func treeHead(t string, f fleet.Folder, agents int) string {
	var b strings.Builder
	b.WriteString("  " + faint("⎇ ") + paint(cSub, filepath.Base(t)))
	if st, ok := f.Trees[t]; ok {
		b.WriteString("  " + gitBits(st))
	}
	if agents == 0 {
		b.WriteString(dim(" · ") + faint("no agent"))
	}
	return b.String()
}

// projectPRs are the pull requests a project's agents opened or pushed
// to, each once, open ones first.
func projectPRs(agents []*fleet.Agent) []claude.PR {
	var out []claude.PR
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
func prLine(pr claude.PR, w int) string {
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

func (m *Model) projectsBody() []string {
	rows := m.projectRows()
	pick := pickRow(rows, &m.work.projPos, &m.work.projSel)
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

func (m *Model) projectsHint() string {
	return keysFit(m.w-4, "↑↓", "move", "enter", "open", "ctrl+y", "its PR", "alt+g", "keep going", "[ ]", "pages", "esc", "back")
}

func (m *Model) projectsKey(s string) tea.Cmd {
	rows := m.projectRows()
	i := pickRow(rows, &m.work.projPos, &m.work.projSel)
	var a *fleet.Agent
	if i >= 0 {
		a = rows[i].a
	}
	move := func(d int) {
		for j, n := i+sign(d), 0; i >= 0 && j >= 0 && j < len(rows); j += sign(d) {
			if rows[j].a != nil {
				m.work.projPos, m.work.projSel = j, rows[j].id
				if n++; n == abs(d) {
					break
				}
			}
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
	case "enter", "right":
		if a != nil {
			m.setView(placeAgents)
			m.sel = a.Key
			m.rebuild()
			return m.focusPane(a)
		}
	case "ctrl+y":
		return m.openPR(a)
	case "alt+g", "g":
		return m.keepGoing(a)
	}
	return nil
}
