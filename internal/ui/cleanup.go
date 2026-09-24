package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// The Cleanup view: agents' worktrees and temp work, what can go without
// losing anything, and what goes by itself. Stopping an agent never removes
// anything; done work that is committed and pushed goes once it has been
// left alone for a while (Settings › Claude › Clean up done work after).

// cleanup is what the Cleanup view and the automatic tidy-up know.
type cleanup struct {
	wts      []fleet.Worktree
	checking bool      // worktrees are being looked at in the background
	checked  time.Time // when they last were, fully
	cursor   int
	// kept are done worktrees the tidy-up found unsafe, and when: it says
	// so once and looks again only after an hour.
	kept map[string]time.Time
}

type worktreesMsg struct {
	wts  []fleet.Worktree
	full bool // every worktree checked and measured, for the view
}

type tidiedMsg struct {
	wts    []fleet.Worktree
	gone   []string // worktree paths removed
	freed  int64
	temp   tempMsg // temp work measured again after cleaning
	kept   map[string]string
	failed error
}

type removedMsg struct {
	path  string
	freed int64
	err   error
}

// agentCopies are the agents as they are now, safe to read from a command.
func (m *Model) agentCopies() []*fleet.Agent {
	out := make([]*fleet.Agent, len(m.snap.Agents))
	for i, a := range m.snap.Agents {
		b := *a
		out[i] = &b
	}
	return out
}

// scanWorktrees finds every agent's worktree and checks each with git (in
// the background: a big checkout takes seconds), for the Cleanup view.
func (m *Model) scanWorktrees() tea.Cmd {
	c := &m.clean
	if c.checking {
		return nil
	}
	c.checking = true
	agents := m.agentCopies()
	return func() tea.Msg {
		wts := fleet.FindWorktrees(agents)
		for i := range wts {
			wts[i].Check()
		}
		return worktreesMsg{wts: wts, full: true}
	}
}

// untouched is how long ago an agent was last active or marked done.
func (m *Model) untouched(a *fleet.Agent, now time.Time) time.Duration {
	d := a.Age(now)
	if t, ok := m.store.Overlay.Done[a.Key]; ok && now.Sub(t) < d {
		d = now.Sub(t)
	}
	return d
}

// dueIn is how long until done work of these agents goes by itself: zero
// when it's due now, negative when it never will (not all done, one still
// running, or the tidy-up is off).
func (m *Model) dueIn(keys []string, now time.Time) time.Duration {
	after := m.store.Config.CleanupAfter()
	if after == 0 || len(keys) == 0 {
		return -1
	}
	due := time.Duration(0)
	for _, k := range keys {
		a := m.agentByKey(k)
		if a == nil || !a.Done || a.PID != 0 {
			return -1
		}
		due = max(due, after-m.untouched(a, now))
	}
	return due
}

// tidy is the automatic clean-up, looked at once a minute: the worktrees
// and temp work of agents done and untouched for long enough. A worktree
// goes only if git says every change is committed and pushed; one that
// isn't is kept, and said once.
func (m *Model) tidy() tea.Cmd {
	c := &m.clean
	if m.tick%60 != 30 || c.checking || m.store.Config.CleanupAfter() == 0 {
		return nil
	}
	now := time.Now()
	agents := m.agentCopies()
	var tempDue []*fleet.Agent
	for _, a := range agents {
		if a.Temp >= tempShown && m.dueIn([]string{a.Key}, now) == 0 {
			tempDue = append(tempDue, a)
		}
	}
	due := map[string]bool{} // agent keys whose work is due
	for _, a := range agents {
		due[a.Key] = m.dueIn([]string{a.Key}, now) == 0
	}
	kept := map[string]bool{}
	for p, t := range c.kept {
		kept[p] = now.Sub(t) < time.Hour
	}
	c.checking = true
	return func() tea.Msg {
		msg := tidiedMsg{kept: map[string]string{}, temp: tempMsg{}}
		msg.wts = fleet.FindWorktrees(agents)
		for _, w := range msg.wts {
			ready := len(w.Agents) > 0 && !kept[w.Path]
			for _, k := range w.Agents {
				ready = ready && due[k]
			}
			if !ready {
				continue
			}
			size := fleet.DiskUsage([]fleet.TempDir{{Path: w.Path}})
			if err := fleet.RemoveWorktree(w, false); err != nil {
				msg.kept[w.Path] = err.Error()
				continue
			}
			msg.gone = append(msg.gone, w.Path)
			msg.freed += size
		}
		for _, a := range tempDue {
			before := a.Temp
			if err := fleet.CleanTemp(a); err != nil && msg.failed == nil {
				msg.failed = err
			}
			left := fleet.DiskUsage(a.TempDirs())
			msg.temp[a.Key] = fleet.TempSize{Bytes: left, At: time.Now()}
			msg.freed += before - left
		}
		return msg
	}
}

func (m *Model) onWorktrees(msg worktreesMsg) {
	c := &m.clean
	c.checking = false
	if msg.full {
		c.wts, c.checked = msg.wts, time.Now()
		return
	}
	c.wts = mergeChecks(msg.wts, c.wts)
}

// mergeChecks keeps what was known about worktrees still there.
func mergeChecks(found, known []fleet.Worktree) []fleet.Worktree {
	by := map[string]fleet.Worktree{}
	for _, w := range known {
		by[w.Path] = w
	}
	for i, w := range found {
		if k, ok := by[w.Path]; ok && !k.Checked.IsZero() {
			k.Agents = w.Agents
			found[i] = k
		}
	}
	return found
}

func (m *Model) onTidied(msg tidiedMsg) {
	c := &m.clean
	c.checking = false
	gone := map[string]bool{}
	for _, p := range msg.gone {
		gone[p] = true
	}
	var left []fleet.Worktree
	for _, w := range mergeChecks(msg.wts, c.wts) {
		if !gone[w.Path] {
			left = append(left, w)
		}
	}
	c.wts = left
	if c.kept == nil {
		c.kept = map[string]time.Time{}
	}
	var notes []string
	for p, why := range msg.kept {
		if _, said := c.kept[p]; !said {
			notes = append(notes, "kept "+filepath.Base(p)+": "+strings.TrimPrefix(why, filepath.Base(p)+" isn't safe to remove: "))
		}
		c.kept[p] = time.Now()
	}
	if len(msg.temp) > 0 {
		m.onCleanedQuiet(msg.temp)
	}
	switch {
	case len(msg.gone) > 0:
		names := make([]string, len(msg.gone))
		for i, p := range msg.gone {
			names[i] = filepath.Base(p)
		}
		m.flash("cleaned up done work: "+strings.Join(names, ", ")+" · freed "+disk(msg.freed)+" (committed and pushed; branches kept)", false)
	case msg.freed >= tempShown:
		m.flash("cleaned up "+disk(msg.freed)+" of done agents' temp work", false)
	case len(notes) > 0:
		m.flash(strings.Join(notes, " · ")+" · the Cleanup view has it", true)
	}
	if msg.failed != nil {
		m.flash("cleaning up: "+msg.failed.Error(), true)
	}
}

// onCleanedQuiet takes in temp sizes measured after a clean-up.
func (m *Model) onCleanedQuiet(sizes tempMsg) {
	m.loader.Temp.Set(sizes)
	for _, a := range m.snap.Agents {
		if e, ok := sizes[a.Key]; ok {
			a.Temp = e.Bytes
		}
	}
	m.rebuild()
}

// cleanRow is one line of the Cleanup view: a worktree, or an agent's temp
// work.
type cleanRow struct {
	wt    *fleet.Worktree
	agent *fleet.Agent // temp work
}

func (m *Model) cleanRows() []cleanRow {
	var rows []cleanRow
	for i := range m.clean.wts {
		rows = append(rows, cleanRow{wt: &m.clean.wts[i]})
	}
	var temp []*fleet.Agent
	for _, a := range m.snap.Agents {
		if a.Temp >= tempShown {
			temp = append(temp, a)
		}
	}
	sort.SliceStable(temp, func(i, j int) bool { return temp[i].Temp > temp[j].Temp })
	for _, a := range temp {
		rows = append(rows, cleanRow{agent: a})
	}
	return rows
}

// running is the first agent in keys with a process.
func (m *Model) running(keys []string) *fleet.Agent {
	for _, k := range keys {
		if a := m.agentByKey(k); a != nil && a.PID != 0 {
			return a
		}
	}
	return nil
}

func (m *Model) agentNames(keys []string) string {
	var out []string
	for _, k := range keys {
		if a := m.agentByKey(k); a != nil {
			out = append(out, oneLine(a.DisplayName))
		}
	}
	if len(out) > 2 {
		return out[0] + ", " + out[1] + fmt.Sprintf(" +%d", len(out)-2)
	}
	return strings.Join(out, ", ")
}

func (m *Model) cleanupBody() []string {
	c := &m.clean
	now := time.Now()
	w := m.w - 4
	rows := m.cleanRows()
	c.cursor = min(max(c.cursor, 0), max(len(rows)-1, 0))
	var wtTotal, tempTotal, safe int64
	for _, r := range rows {
		switch {
		case r.wt != nil:
			wtTotal += r.wt.Size
			if r.wt.Safe() && m.running(r.wt.Agents) == nil {
				safe += r.wt.Size
			}
		case r.agent != nil:
			tempTotal += r.agent.Temp
			if r.agent.PID == 0 {
				safe += r.agent.Temp
			}
		}
	}
	title := paint(cText+bold, "Cleanup") + dim(fmt.Sprintf("  ·  %s in worktrees · %s of temp work · %s can go without losing anything", disk(wtTotal), disk(tempTotal), disk(safe)))
	rule := "Stopping an agent never removes anything. Done work goes by itself once it's committed, pushed and untouched for "
	if after := m.store.Config.CleanupAfter(); after > 0 {
		rule += dur(after) + " (Settings › Claude)."
	} else {
		rule = "Automatic clean-up is off (Settings › Claude); nothing goes unless you remove it here."
	}
	out := []string{title, dim(rule), ""}
	switch {
	case c.checking && c.checked.IsZero():
		out = append(out, paint(cOrange, spinner[m.tick%len(spinner)])+dim(" looking at every agent's worktrees with git…"), "")
	case c.checking:
		out = append(out, paint(cOrange, spinner[m.tick%len(spinner)])+dim(" checking again…"), "")
	}
	section := ""
	for i, r := range rows {
		s := "Worktrees"
		if r.agent != nil {
			s = "Temp work"
		}
		if s != section {
			section = s
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, dim(s))
		}
		var line string
		if r.wt != nil {
			line = m.worktreeLine(*r.wt, w, now)
		} else {
			line = m.tempLine(r.agent, w, now)
		}
		if i == c.cursor {
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		}
		out = append(out, line)
	}
	if len(rows) == 0 && !c.checking {
		out = append(out, dim("Nothing to clean: no agent worktrees, and no temp work over a megabyte."))
	}
	return out
}

func (m *Model) worktreeLine(wt fleet.Worktree, w int, now time.Time) string {
	name := filepath.Base(wt.Path)
	where := filepath.Base(wt.Repo)
	if wt.Branch != "" {
		where += " · " + wt.Branch
	}
	size := dim(right("…", 7))
	if wt.Size > 0 {
		size = right(disk(wt.Size), 7)
		if wt.Size >= 1<<30 {
			size = paint(cYellow, size)
		}
	}
	who := dim("no agent")
	if len(wt.Agents) > 0 {
		who = paint(cSub, m.agentNames(wt.Agents))
	}
	var mark, status string
	run := m.running(wt.Agents)
	switch {
	case wt.Checked.IsZero():
		mark, status = faint("·"), dim("not looked at yet")
	case wt.Err != "":
		mark, status = paint(cRed, "!"), paint(cRed, wt.Err)
	case !wt.Safe():
		mark, status = paint(cYellow, "✗"), paint(cYellow, "would lose "+wt.Losses())
	case run != nil:
		mark, status = paint(cGreen, "✓"), dim("clean and pushed · "+oneLine(run.DisplayName)+" is still running in it")
	default:
		mark = paint(cGreen, "✓")
		switch due := m.dueIn(wt.Agents, now); {
		case due == 0:
			status = paint(cGreen, "clean and pushed · goes at the next tidy-up")
		case due > 0:
			status = paint(cGreen, "clean and pushed") + dim(" · goes in "+dur(due.Round(time.Minute)))
		case len(wt.Agents) == 0:
			status = paint(cGreen, "clean and pushed") + dim(" · x removes it")
		default:
			status = paint(cGreen, "clean and pushed") + dim(" · goes once its agents are done (alt+d)")
		}
	}
	left := " " + mark + " " + paint(cText, fit(name, 28)) + " " + size + "  " + dim(fit(where, 42)) + " " + fit(who, 30)
	return fit(left+"  "+status, w)
}

func (m *Model) tempLine(a *fleet.Agent, w int, now time.Time) string {
	size := right(disk(a.Temp), 7)
	if a.Temp >= 1<<30 {
		size = paint(cYellow, size)
	}
	var status string
	switch due := m.dueIn([]string{a.Key}, now); {
	case a.PID != 0:
		status = dim("running · its temp work may be in use")
	case due == 0:
		status = paint(cGreen, "done · goes at the next tidy-up")
	case due > 0:
		status = dim("done · goes in " + dur(due.Round(time.Minute)))
	default:
		status = dim("stopped " + age(a.Age(now)) + " ago · x removes it · alt+d marks it done")
	}
	left := " " + paint(cSub, "▤") + " " + paint(cText, fit(oneLine(a.DisplayName), 28)) + " " + size + "  " + dim(fit("scratch folders", 42)) + " " + fit("", 30)
	return fit(left+"  "+status, w)
}

func (m *Model) cleanupKey(s string) tea.Cmd {
	c := &m.clean
	rows := m.cleanRows()
	switch s {
	case "esc", "left", "q":
		m.setView(0)
	case "up", "k":
		c.cursor = max(0, c.cursor-1)
	case "down", "j":
		c.cursor = min(len(rows)-1, c.cursor+1)
	case "r":
		return m.scanWorktrees()
	case "x", "enter", "backspace", "delete":
		if c.cursor < len(rows) {
			r := rows[c.cursor]
			if r.agent != nil {
				m.askClean(r.agent)
				return nil
			}
			m.askRemoveWorktree(*r.wt)
		}
	case "A":
		m.askCleanSafe(rows)
	}
	return nil
}

// askRemoveWorktree asks before removing a worktree, naming exactly what
// would be lost; its branch always stays.
func (m *Model) askRemoveWorktree(wt fleet.Worktree) {
	name := filepath.Base(wt.Path)
	if a := m.running(wt.Agents); a != nil {
		m.flash(oneLine(a.DisplayName)+" is still running in "+name+"; stop it or mark it done first", true)
		return
	}
	if wt.Checked.IsZero() {
		m.flash(name+" hasn't been looked at yet · a moment", false)
		return
	}
	branch := "its branch stays"
	if wt.Branch != "" {
		branch = wt.Branch + " stays as a branch"
	}
	if wt.Safe() {
		m.confirm = &confirmation{
			question: fmt.Sprintf("Remove worktree %s (%s)?", name, disk(wt.Size)),
			detail:   "everything in it is committed and pushed · " + branch,
			onYes:    func() tea.Cmd { return m.removeWorktree(wt, false) },
		}
		return
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("Delete worktree %s and lose %s?", name, firstNonEmpty(wt.Losses(), wt.Err)),
		detail:   "this can't be undone · " + branch,
		onYes:    func() tea.Cmd { return m.removeWorktree(wt, true) },
	}
}

// askCleanSafe asks before removing everything that can go without losing
// anything: clean, pushed worktrees nothing runs in, and stopped agents'
// temp work.
func (m *Model) askCleanSafe(rows []cleanRow) {
	var wts []fleet.Worktree
	var temp []*fleet.Agent
	var total int64
	for _, r := range rows {
		switch {
		case r.wt != nil && r.wt.Safe() && m.running(r.wt.Agents) == nil:
			wts = append(wts, *r.wt)
			total += r.wt.Size
		case r.agent != nil && r.agent.PID == 0:
			temp = append(temp, r.agent)
			total += r.agent.Temp
		}
	}
	if len(wts)+len(temp) == 0 {
		m.flash("nothing can go without losing work · x on a row removes it anyway, after saying what's lost", false)
		return
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("Remove %s: %d worktrees and %d agents' temp work?", disk(total), len(wts), len(temp)),
		detail:   "only what's committed and pushed, or scratch · branches and conversations stay",
		onYes: func() tea.Cmd {
			cmds := []tea.Cmd{m.cleanTemp(temp)}
			for _, w := range wts {
				cmds = append(cmds, m.removeWorktree(w, false))
			}
			return tea.Batch(cmds...)
		},
	}
}

func (m *Model) removeWorktree(wt fleet.Worktree, force bool) tea.Cmd {
	m.flash("removing "+filepath.Base(wt.Path)+"…", false)
	return func() tea.Msg {
		err := fleet.RemoveWorktree(wt, force)
		return removedMsg{path: wt.Path, freed: wt.Size, err: err}
	}
}

func (m *Model) onRemoved(msg removedMsg) {
	name := filepath.Base(msg.path)
	if msg.err != nil {
		m.flash(msg.err.Error(), true)
		return
	}
	c := &m.clean
	for i, w := range c.wts {
		if w.Path == msg.path {
			c.wts = append(c.wts[:i], c.wts[i+1:]...)
			break
		}
	}
	m.flash("removed "+name+" · freed "+disk(msg.freed), false)
}
