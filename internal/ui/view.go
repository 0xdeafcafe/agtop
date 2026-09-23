package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "agtop"
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

type tally struct {
	blocked, working, busy, done int
	today                        float64
}

func (m *Model) tally() tally {
	var t tally
	for _, a := range m.snap.Agents {
		switch {
		case a.State == "blocked":
			t.blocked++
		case a.Live():
			t.working++
		case a.Busy():
			t.busy++
		default:
			t.done++
		}
	}
	for _, av := range m.snap.Accounts {
		t.today += av.Today
	}
	return t
}

// mood is clanker's reading of the fleet.
func (m *Model) mood(t tally) mood {
	switch {
	case t.blocked > 0:
		return moodNeedsYou
	case t.today >= 500 && m.tick%8 < 2:
		return moodSpendy
	case t.working > 0:
		return moodWorking
	}
	if h := m.snap.At.Hour(); h >= 23 || h < 7 {
		return moodSleepy
	}
	return moodIdle
}

func (m *Model) header() []string {
	t := m.tally()
	robot := clanker(m.mood(t), m.tick)
	var counts []string
	if t.blocked > 0 {
		counts = append(counts, paint(cYellow+bold, fmt.Sprintf("● %d needs you", t.blocked)))
	}
	if t.working > 0 {
		counts = append(counts, paint(cOrange, fmt.Sprintf("✻ %d working", t.working)))
	}
	if t.busy > 0 {
		counts = append(counts, paint(cSub, fmt.Sprintf("◌ %d in background", t.busy)))
	}
	counts = append(counts, dim(fmt.Sprintf("%d finished", t.done)))
	left1 := paint(cText+bold, "agtop") + "   " + strings.Join(counts, "   ")

	acct := m.store.Config.ActiveAccount()
	left2 := dim(acct.Name + " · " + tildify(m.launchDir))

	right1 := paint(cText, money(t.today)) + dim(" today")
	if u := m.activeUsage(); u != "" {
		right1 += dim("   ") + u
	}
	mc := m.snap.Machine
	right2 := dim(fmt.Sprintf("%s ram · %.0f%% cpu", mem(mc.TotalMem), mc.TotalCPU))
	if !m.loaded {
		right2 = dim("costing transcripts…   ") + right2
	}

	pad := strings.Repeat(" ", ansi.StringWidth(robot[0]))
	line := func(r, l, rt string) string {
		body := "  " + r + "   " + l
		gap := m.w - ansi.StringWidth(body) - ansi.StringWidth(rt) - 2
		if gap < 2 {
			return fit(body, m.w)
		}
		return body + strings.Repeat(" ", gap) + rt
	}
	out := make([]string, len(robot))
	for i, r := range robot {
		out[i] = "  " + r
	}
	// Text sits level with the head and face.
	out[1] = line(robot[1], left1, right1)
	out[2] = line(robot[2], left2, right2)
	_ = pad
	return out
}

// activeUsage is the current account's plan usage, quiet unless it is high.
func (m *Model) activeUsage() string {
	for _, av := range m.snap.Accounts {
		if !av.Current {
			continue
		}
		u := av.Usage
		if !u.FiveHour.Present {
			return ""
		}
		pct := func(label string, p float64) string {
			c := cSub
			switch {
			case p >= 80:
				c = cRed
			case p >= 50:
				c = cYellow
			}
			return dim(label+" ") + paint(c, fmt.Sprintf("%.0f%%", p))
		}
		s := pct("5h", u.FiveHour.Percent)
		if u.SevenDay.Present {
			s += dim(" · ") + pct("7d", u.SevenDay.Percent)
		}
		if !u.FetchedAt.IsZero() && m.snap.At.Sub(u.FetchedAt) > time.Hour {
			s += faint(" as of " + u.FetchedAt.Local().Format("15:04"))
		}
		if len(m.snap.Accounts) > 1 {
			s = dim(av.Name+" ") + s
		}
		return s
	}
	return ""
}

func (m *Model) render() string {
	if m.w == 0 {
		return ""
	}
	switch m.mode {
	case modeHelp:
		return m.overlayBox(m.listView(), m.helpBody(), min(m.w-6, 116))
	case modeProcs:
		return m.frame(m.procBody(), keysFit(m.w-4, "tab", "agent / machine", "enter", "jump to agent", "ctrl+x", "SIGTERM", "!", "SIGKILL tree", "esc", "back"))
	case modeCwd:
		return m.frame(m.cwdBody(), keysFit(m.w-4, "enter", "apply", "tab", "move / add", "↑↓", "pick", "esc", "cancel"))
	}
	return m.listView()
}

func (m *Model) frame(body []string, hint string) string {
	head := m.header()
	var b strings.Builder
	for _, l := range head {
		b.WriteString(fit(l, m.w))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	avail := m.h - len(head) - 3
	for i := 0; i < avail; i++ {
		if i < len(body) {
			b.WriteString(fit("  "+body[i], m.w))
		}
		b.WriteByte('\n')
	}
	b.WriteString(faint(strings.Repeat("─", m.w)))
	b.WriteByte('\n')
	b.WriteString(m.statusOr(hint))
	return b.String()
}

func (m *Model) statusOr(hint string) string {
	if m.confirm != nil {
		c := m.confirm
		s := paint(cText+bold, c.question) + "  " + dim(c.detail) + "   " + paint(cOrange, "y") + dim(" yes")
		if c.onBang != nil && c.bangText != "" {
			s += "   " + paint(cOrange, "!") + dim(" "+c.bangText)
		}
		return fit("  "+s+"   "+paint(cOrange, "n")+dim(" cancel"), m.w)
	}
	if m.status != "" && m.snap.At.Sub(m.statusAt).Seconds() < 6 {
		c := cSub
		if m.statusErr {
			c = cRed
		}
		return fit("  "+paint(c, m.status), m.w)
	}
	return fit("  "+hint, m.w)
}

// keysFit drops the least important pairs (those before the last) until the
// line fits w, so a hint never runs off the screen.
func keysFit(w int, pairs ...string) string {
	for len(pairs) > 2 {
		if s := keys(pairs...); ansi.StringWidth(s) <= w {
			return s
		}
		pairs = append(pairs[:len(pairs)-4], pairs[len(pairs)-2:]...)
	}
	return keys(pairs...)
}

// keys renders "key label" pairs with the key brighter than its label.
func keys(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, paint(cSub, pairs[i])+" "+dim(pairs[i+1]))
	}
	return strings.Join(parts, faint("  ·  "))
}

// layout splits the screen between the list and the preview pane and says
// how many rows the body gets under the header and above the prompt.
func (m *Model) layout() (listW, paneW, bodyH int) {
	listW = m.w
	if m.preview {
		if m.w >= 120 && !m.full {
			paneW = m.w * 55 / 100
			listW = m.w - paneW - 1
		} else {
			paneW, listW = m.w, 0
		}
	}
	bodyH = max(3, m.h-len(m.header())-1-len(m.promptLines()))
	return listW, paneW, bodyH
}

func (m *Model) listView() string {
	head := m.header()
	prompt := m.promptLines()
	var dock []string
	if !m.preview && m.h >= 20+m.dockLines() {
		if f := m.focused(); f != nil {
			for _, l := range m.cardLines(f, m.w-4) {
				dock = append(dock, "  "+l)
			}
		} else {
			dock = append(dock, "", faint("  select an agent to see what it is doing"))
		}
		for len(dock) < 4+m.dockLines() {
			dock = append(dock, "")
		}
	}
	listW, paneW, bodyH := m.layout()
	bodyH = max(3, bodyH-len(dock))
	m.listTop = len(head) + 1
	m.rowKeys = nil
	var left []string
	if listW > 0 {
		left = m.listLines(listW, bodyH)
	}
	var pane []string
	if paneW > 0 {
		if pane = m.liveLines(paneW - 3); pane == nil {
			pane = m.previewLines(paneW-3, bodyH)
		}
	}
	var b strings.Builder
	for _, l := range head {
		b.WriteString(fit(l, m.w))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for i := 0; i < bodyH; i++ {
		l, p := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(pane) {
			p = pane[i]
		}
		switch {
		case listW > 0 && paneW > 0:
			b.WriteString(fit(l, listW) + faint("│") + "  " + fit(p, paneW-3))
		case listW > 0:
			b.WriteString(fit(l, m.w))
		default:
			b.WriteString("  " + fit(p, m.w-2))
		}
		b.WriteByte('\n')
	}
	for _, l := range dock {
		b.WriteString(fit(l, m.w))
		b.WriteByte('\n')
	}
	for i, l := range prompt {
		b.WriteString(fit(l, m.w))
		if i < len(prompt)-1 {
			b.WriteByte('\n')
		}
	}
	if m.dialog != nil {
		return m.overlay(b.String())
	}
	return b.String()
}

// Column widths on the right of a row.
const (
	wCPU  = 6
	wRAM  = 7
	wCost = 8
	wAge  = 5
)

func (m *Model) listLines(w, h int) []string {
	nameCol := m.nameColumn(w)
	focus := m.focused()
	var all, keys []string
	selTop, selBottom := -1, -1
	emit := func(line, key string, sel bool) {
		if sel {
			if selTop < 0 {
				selTop = len(all)
			}
			selBottom = len(all)
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		} else if key != "" && key == m.hover {
			line = hoverLine(line, w)
		}
		all = append(all, line)
		keys = append(keys, key)
	}
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			key := sectionKey(l.title)
			emit(m.sectionLine(l, w), key, key == m.sel)
		case lineBlank:
			emit("", "", false)
		case lineAgent:
			emit(m.agentLine(l.agent, w, l.agent.Key == m.sel, nameCol), l.agent.Key, l.agent.Key == m.sel)
		case lineSub:
			emit(m.subLine(l.agent, w), l.agent.Key, l.agent.Key == m.sel)
		case lineTask:
			emit(m.taskLine(l, w), l.agent.Key, false)
		case lineCard:
			_ = focus
		}
	}
	if len(m.order) == 0 {
		all = append(all, "", dim("  No agents yet. Describe a task below to start one."))
		keys = append(keys, "", "")
	}
	if selTop >= 0 {
		if selTop-1 < m.scroll {
			m.scroll = max(0, selTop-1)
		}
		if selBottom+1 >= m.scroll+h {
			m.scroll = selBottom + 2 - h
		}
	}
	if m.scroll > len(all)-h {
		m.scroll = max(0, len(all)-h)
	}
	end := min(len(all), m.scroll+h)
	out := append([]string(nil), all[m.scroll:end]...)
	m.rowKeys = keys[m.scroll:end]
	if end < len(all) && len(out) > 0 {
		out[len(out)-1] = faint(fmt.Sprintf("  ↓ %d more lines", len(all)-end))
		m.rowKeys[len(out)-1] = ""
	}
	return out
}

const hoverBG = "\x1b[48;2;33;31;29m"

func hoverLine(line string, w int) string {
	line = fit(line, w)
	return hoverBG + strings.ReplaceAll(line, reset, reset+hoverBG) + reset
}

func (m *Model) sectionLine(l listLine, w int) string {
	arrow := faint("▾ ")
	if l.folded {
		arrow = faint("▸ ")
	}
	meta := l.meta
	if (l.title == "Working" || l.title == "Needs you") && m.sharedContext() != "" {
		meta += "  ·  " + m.sharedContext()
	}
	if l.folded {
		head := arrow + paint(cSub+bold, l.title) + "  " + dim(meta)
		room := w - ansi.StringWidth(head) - 6
		if room > 10 && l.peek != "" {
			head += "   " + faint(fit(l.peek, room))
		}
		return "  " + head
	}
	return "  " + arrow + rule(l.title, meta, w-6)
}

// cardLines draw the focused row's details as a box: what it is doing now
// in the title, its latest words inside, and its numbers in a footer.
func (m *Model) cardLines(a *fleet.Agent, w int) []string {
	bodyLines := m.dockLines()
	p := m.previews[a.Key].p
	inner := w - 6
	edge := func(s string) string { return paint(cDim, s) }
	line := func(s string) string { return edge("│") + panel("  "+fit(s, inner)+"  ") + edge("│") }

	title := ""
	switch {
	case a.Live() && p.Tool != "":
		arg := oneLine(tildify(p.ToolArg))
		title = paint(cOrange, "● ") + paint(cText+bold, p.Tool) + "  " + paint(cSub, ansi.Truncate(arg, max(10, inner-len(p.Tool)-8), "…"))
	case a.State == "blocked":
		title = paint(cYellow+bold, "waiting on you")
	case a.Busy():
		title = paint(cOrange, "◌ ") + paint(cText, "background work still running")
	case a.PID != 0:
		title = dim("idle · still in memory")
	default:
		title = dim("finished " + age(a.Age(m.snap.At)) + " ago")
	}
	tw := ansi.StringWidth(title)
	top := edge("╭─ ") + title + edge(" "+strings.Repeat("─", max(0, w-tw-5))+"╮")

	var body []string
	text := p.Text
	if !a.Live() && strings.TrimSpace(a.Detail) != "" && a.Detail != "stopped" {
		text = a.Detail
	}
	if a.State == "blocked" && a.Needs != "" {
		text = a.Needs
	}
	if text == "" {
		text = "…"
	}
	text = strings.NewReplacer("**", "", "`", "", "__", "").Replace(oneLine(text))
	var lines []string
	for _, l := range wrap(text, inner) {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	for i, l := range lines {
		if i == bodyLines-1 && len(lines) > bodyLines {
			body = append(body, paint(cText, ansi.Truncate(l, inner-24, "…"))+faint("   tab for the full preview"))
			break
		}
		body = append(body, paint(cText, l))
	}

	var cells []string
	if p.Context > 0 {
		win := claude.ContextWindow(p.Model)
		pct := float64(p.Context) / float64(win) * 100
		cells = append(cells, dim("context ")+ctxBar(pct)+" "+paint(cText, fmt.Sprintf("%.0f%%", pct))+dim(" of "+tokens(win)))
	}
	u := a.Spend.Usage
	if a.Spend.Cost > 0 {
		cells = append(cells, dim("spent ")+paint(cText, money(a.Spend.Cost)))
		cells = append(cells, dim("out ")+paint(cSub, tokens(u.Output))+dim("  cache ")+paint(cSub, tokens(u.CacheRead)))
	}
	if a.PID != 0 && a.Procs > 0 {
		cells = append(cells, dim(fmt.Sprintf("%d procs ", a.Procs))+paint(cSub, mem(a.Mem)))
	}
	if !a.Live() {
		if el := a.Elapsed(m.snap.At); el > 0 {
			cells = append(cells, dim("ran ")+paint(cSub, dur(el)))
		}
	}
	if c := m.context(a); c != "" {
		cells = append(cells, faint(c))
	}

	for len(body) < bodyLines {
		body = append(body, "")
	}
	out := []string{top}
	for _, l := range body[:bodyLines] {
		out = append(out, line(l))
	}
	out = append(out, edge("├"+strings.Repeat("─", w-2)+"┤"))
	out = append(out, line(strings.Join(cells, edge("   │   "))))
	out = append(out, edge("╰"+strings.Repeat("─", w-2)+"╯"))
	return out
}

func ctxBar(pct float64) string {
	n := min(8, max(0, int(pct/12.5+0.5)))
	c := cSub
	switch {
	case pct >= 80:
		c = cRed
	case pct >= 50:
		c = cYellow
	}
	return paint(c, strings.Repeat("━", n)) + faint(strings.Repeat("━", 8-n))
}

// nameColumn is where summaries start: wide enough for most names, never
// more than two fifths of the row.
func (m *Model) nameColumn(w int) int {
	widest := 0
	for _, l := range m.lines {
		if l.kind != lineAgent {
			continue
		}
		n := ansi.StringWidth(oneLine(l.agent.DisplayName))
		if b := ansi.StringWidth(ansi.Strip(m.badges(l.agent))); b > 0 {
			n += b + 1
		}
		widest = max(widest, n)
	}
	return max(20, min(widest, (w-30)*2/5))
}

// agentLine is the first line of a row: marker, name, badges, figures.
func (m *Model) agentLine(a *fleet.Agent, w int, sel bool, nameCol int) string {
	now := m.snap.At
	live := a.Live()
	marker := " "

	switch {
	case a.Checking:
		marker = paint(cSub, "◔")
	case a.JustFinished(now):
		marker = paint(cGreen, "✓")
	case a.State == "blocked":
		marker = paint(cYellow, "●")
	case live:
		marker = paint(cOrange, spinner[(m.tick+len(a.ID))%len(spinner)])
	case a.Busy():
		marker = paint(cOrange, "◌")
	case a.Done:
		marker = paint(cGreen, "✓")
	case a.PID != 0:
		marker = faint("◦")
	}

	busy := a.Busy()
	resident := !live && a.PID != 0
	var right string
	if live || resident {
		cpu, ram := strings.Repeat(" ", wCPU), strings.Repeat(" ", wRAM)
		if a.PID != 0 {
			cpu = right1(fmt.Sprintf("%.0f%%", a.CPU), wCPU)
			ram = right1(mem(a.Mem), wRAM)
		}
		if live || busy {
			right = dim(cpuColor(a.CPU, cpu)) + memColor(a.Mem, ram) + paint(cText, right1(money(a.Spend.Cost), wCost))
		} else {
			right = faint(cpu) + dim(ram) + dim(right1(money(a.Spend.Cost), wCost))
		}
	} else {
		cost := money(a.Spend.Cost)
		if cost == "–" {
			cost = ""
		}
		right = strings.Repeat(" ", wCPU+wRAM) + dim(right1(cost, wCost))
	}
	if live {
		right += faint(right1(dur(a.Elapsed(now)), wAge+2)) + " "
	} else if busy {
		right += faint(right1(age(a.Age(now)), wAge+2)) + " "
	} else {
		right += faint(right1(age(a.Age(now)), wAge+2)) + " "
	}

	nameColor := cSub
	switch {
	case live || sel:
		nameColor = cText + bold
	case busy:
		nameColor = cText
	case a.Pinned:
		nameColor = cText
	case a.Done:
		nameColor = cDim
	}
	name := oneLine(a.DisplayName)
	badges := m.badges(a)
	summary, sumColor := "", cDim
	switch {
	case a.Checking:
		summary, sumColor = "turn ended · checking…", cDim
	case a.JustFinished(now):
		summary, sumColor = "just finished · "+oneLine(a.Detail), cGreen
	case a.State == "blocked":
		summary, sumColor = oneLine(a.Needs), cYellow
		if summary == "" {
			summary = oneLine(a.Detail)
		}
	case live:
		summary, sumColor = oneLine(a.Detail), cSub
		if summary == "" && a.Interactive {
			summary = "working in a terminal"
		}
	case busy:
		summary = backgroundText(a)
	default:
		if f := m.focused(); m.preview || f == nil || f.Key != a.Key {
			summary = oneLine(a.Detail)
		}
	}
	if summary == "stopped" {
		summary = ""
	}
	room := w - 3 - ansi.StringWidth(right)
	left := paint(nameColor, name)
	if badges != "" {
		left += " " + badges
	}
	if summary != "" {
		left = fit(left, nameCol)
		if sw := room - nameCol - 2; sw > 8 {
			left += "  " + paint(sumColor, fit(summary, sw))
		}
	}
	return " " + marker + " " + fit(left, room) + right
}

// sharedContext is the repository and branch every live agent shares, shown
// once in the section header instead of on each row.
func (m *Model) sharedContext() string {
	shared := ""
	for _, a := range m.order {
		if !a.Live() || a.Interactive {
			continue
		}
		c := m.context(a)
		if shared == "" {
			shared = c
		} else if c != shared {
			return ""
		}
	}
	return shared
}

// dirLabel names a folder the way the list does: repository · branch.
func (m *Model) dirLabel(dir string) string {
	for _, a := range m.snap.Agents {
		if a.Cwd == dir && a.Repo == dir {
			return paint(cText, m.context(a))
		}
	}
	return paint(cText, tildify(dir))
}

// context is where a live agent works: repository and branch.
func (m *Model) context(a *fleet.Agent) string {
	if a.Repo == "" {
		if strings.Contains(a.Cwd, "/var/folders/") || strings.HasPrefix(a.Cwd, "/tmp/") {
			return "tmp/" + filepath.Base(a.Cwd)
		}
		return tildify(a.Cwd)
	}
	s := filepath.Base(a.Repo)
	if a.Branch != "" {
		s += " · " + a.Branch
	}
	return s
}

// taskLine is a subagent, shell or monitor nested under the agent running it.
func (m *Model) taskLine(l listLine, w int) string {
	t := l.task
	branch := "├"
	if l.last {
		branch = "└"
	}
	icon, kind, col := "▸", "shell", cSub
	switch t.Kind {
	case "agent":
		icon, kind, col = "↳", "subagent", cOrange
	case "monitor":
		icon, kind, col = "◎", "monitor", cDim
	}
	label := oneLine(tildify(t.Label))
	if l.more > 0 {
		label += faint(fmt.Sprintf("   +%d more", l.more))
	}
	since := ""
	if !t.StartedAt.IsZero() && t.StartedAt.Unix() > 0 {
		since = dur(m.snap.At.Sub(t.StartedAt))
	}
	room := w - 22 - wAge - 4
	return "    " + faint(branch+" ") + paint(col, icon+" "+fit(kind, 9)) + " " + dim(fit(label, room)) + faint(right1(since, wAge+2))
}

// backgroundText says what a finished agent is still waiting on.
func backgroundText(a *fleet.Agent) string {
	kinds := map[string]int{}
	var first string
	for _, b := range a.Background {
		k, label, _ := strings.Cut(b, "\x00")
		kinds[k]++
		if first == "" {
			first = oneLine(label)
		}
	}
	var parts []string
	for _, k := range []string{"shell", "agent", "monitor"} {
		if n := kinds[k]; n > 0 {
			name := map[string]string{"shell": "shell", "agent": "subagent", "monitor": "monitor"}[k]
			if n > 1 {
				name += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, name))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d tasks", a.InFlight))
	}
	s := "background · " + strings.Join(parts, ", ") + " running"
	if first != "" {
		s += " · " + first
	}
	return s
}

// subLine is the second line of a live row: what the agent is doing.
func (m *Model) subLine(a *fleet.Agent, w int) string {
	text, col := oneLine(a.Detail), cSub
	if text == "" {
		text = oneLine(a.Intent)
	}
	if text == "" && a.Interactive {
		text, col = "working in a terminal", cDim
	}
	if a.State == "blocked" && a.Needs != "" {
		text, col = oneLine(a.Needs), cYellow
	}
	if a.Busy() {
		text, col = backgroundText(a), cSub
	}
	if !a.Live() && !a.Busy() && m.expanded[a.Key] {
		text = oneLine(a.Detail) + dim("  ·  "+tildify(a.Cwd))
	}
	tail := ""
	switch {
	case a.Live():
		tail = faint(dur(a.Elapsed(m.snap.At)) + " running")
	case a.Busy():
		tail = faint("turn ended " + age(a.Age(m.snap.At)) + " ago")
	}
	room := w - 5 - ansi.StringWidth(tail) - 3
	return "    " + paint(col, fit(text, room)) + "  " + tail
}

func right1(s string, w int) string { return right(s, w) }

func (m *Model) badges(a *fleet.Agent) string {
	var parts []string
	for i, pr := range a.PRs {
		if i == 2 {
			parts = append(parts, dim(fmt.Sprintf("+%d", len(a.PRs)-2)))
			break
		}
		col := cGreen
		switch pr.State {
		case "MERGED":
			col = cBlue
		case "CLOSED":
			col = cDim
		case "DRAFT":
			col = cSub
		}
		if pr.Checks.Failed > 0 && pr.State != "MERGED" && pr.State != "CLOSED" {
			col = cRed
		}
		parts = append(parts, paint(col, fmt.Sprintf("#%d", pr.Number)))
	}
	if a.Subagents > 0 {
		label := "subagent"
		if a.Subagents > 1 {
			label += "s"
		}
		parts = append(parts, paint(cOrange, fmt.Sprintf("↳%d", a.Subagents))+faint(" "+label))
	}
	if a.Todos > 0 && (a.Live() || a.Busy()) {
		parts = append(parts, faint(fmt.Sprintf("☐ %d/%d", a.TodosDone, a.Todos)))
	}
	if a.Children > 0 {
		parts = append(parts, faint(fmt.Sprintf("◫%d", a.Children)))
	}
	if a.Interactive {
		parts = append(parts, faint("terminal"))
	}
	return strings.Join(parts, " ")
}

// promptLines are the input box: a rule carrying where a new session will
// start, the input wrapped over up to six lines, a rule, and the key hints.
func (m *Model) promptLines() []string {
	label := paint(cOrange, "❯ ")
	placeholder := "describe a task for a new session"
	a := m.selected()
	switch {
	case m.inKind == inRename:
		label, placeholder = paint(cOrange, "rename ❯ "), "new name · enter to save · empty resets it"
	case m.inKind == inGroup:
		label, placeholder = paint(cOrange, "group ❯ "), "group name · empty clears it"
	case (m.inKind == inReply || m.preview) && a != nil:
		label = paint(cOrange+bold, "reply ") + dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 32, "…")) + paint(cOrange, " ❯ ")
		placeholder = "a message for this agent · enter sends · esc leaves reply mode"
	}
	text := string(m.input)
	top := faint(strings.Repeat("─", m.w))
	sel := ""
	if a != nil {
		sel = faint("── ") + dim(tildify(a.Cwd))
		if a.Branch != "" {
			sel += faint(" · " + a.Branch)
		}
		sel += " "
	}
	where := ""
	if m.inKind == inPrompt && !(m.preview && a != nil) && !strings.HasPrefix(text, "/") {
		where = dim("start in ") + m.dirLabel(m.startDir())
		if n := len(m.startDirs()); n > 1 {
			where += faint(fmt.Sprintf("  %d/%d  ", ((m.dirIdx%n)+n)%n+1, n)) + paint(cSub, "ctrl+n") + faint(" next")
		}
		where = " " + where + " " + faint("───")
	}
	if gap := m.w - ansi.StringWidth(sel) - ansi.StringWidth(where); gap >= 3 {
		top = sel + faint(strings.Repeat("─", gap)) + where
	} else if gap := m.w - ansi.StringWidth(where); where != "" && gap >= 3 {
		top = faint(strings.Repeat("─", gap)) + where
	}
	out := []string{top}
	lw := ansi.StringWidth(label)
	if text == "" {
		out = append(out, "  "+label+faint(placeholder))
	} else {
		lines := strings.Split(ansi.Wrap(text, max(10, m.w-lw-4), ""), "\n")
		if len(lines) > 6 {
			lines = append([]string{faint("…")}, lines[len(lines)-5:]...)
		}
		for i, l := range lines {
			pre := strings.Repeat(" ", lw)
			if i == 0 {
				pre = label
			}
			if i == len(lines)-1 {
				l = paint(cText, l) + paint(cOrange, "▏")
			} else {
				l = paint(cText, l)
			}
			out = append(out, "  "+pre+l)
		}
	}
	out = append(out, faint(strings.Repeat("─", m.w)))
	hint := keysFit(m.w-4, "enter", "open", "ctrl+o", "reply", "tab", "preview", "ctrl+f", "done", "ctrl+x", "stop", "ctrl+p", "processes", "?", "all keys")
	if m.inKind == inReply {
		hint = keysFit(m.w-4, "enter", "send", "↑↓", "pick another agent", "esc", "leave reply mode", "?", "all keys")
	}
	if m.preview {
		hint = keysFit(m.w-4, "enter", "send · empty opens", "←", "close preview", "ctrl+p", "processes", "ctrl+l", "move", "?", "all keys")
	}
	return append(out, m.statusOr(hint))
}

func (m *Model) previewLines(w, h int) []string {
	a := m.focused()
	if a == nil {
		return []string{dim("select an agent to preview it")}
	}
	now := m.snap.At
	e := m.previews[a.Key]
	p := e.p
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	label := func(k string) string { return dim(fit(k, 9)) }
	model := p.Model
	if model == "" {
		model = a.Spend.Model
	}
	add(paint(cText+bold, oneLine(a.DisplayName)))
	add(dim(a.ID + " · " + a.Acct.Name + " · " + strings.TrimPrefix(model, "claude-")))
	loc := tildify(a.Cwd)
	if a.Branch != "" {
		loc += dim(" · " + a.Branch)
	}
	add(loc, "")
	st := a.State
	switch {
	case a.State == "blocked":
		st = paint(cYellow, "awaiting input")
	case a.Live():
		st = paint(cOrange, "working")
	}
	add(label("STATE") + st + dim(" · updated "+age(a.Age(now))+" ago"))
	if a.State == "blocked" && a.Needs != "" {
		for i, l := range wrap(a.Needs, w-9) {
			if i == 0 {
				add(label("NEEDS") + paint(cYellow, l))
			} else {
				add("         " + paint(cYellow, l))
			}
		}
	}
	now2 := oneLine(a.Detail)
	if p.Tool != "" && a.Live() {
		now2 = paint(cOrange, "● ") + p.Tool + "  " + oneLine(p.ToolArg)
	}
	add(label("NOW") + fit(now2, w-9))
	if p.Text != "" {
		lines := wrap(p.Text, w-9)
		limit := max(3, h/3)
		if m.full {
			limit = max(6, h-18)
		}
		for i, l := range lines {
			if i >= limit {
				add("         " + dim("…"))
				break
			}
			if i == 0 {
				add(label("LAST") + l)
			} else {
				add("         " + l)
			}
		}
	}
	add("")
	u := a.Spend.Usage
	add(label("SPEND") + paint(cText+bold, money(a.Spend.Cost)) + dim(fmt.Sprintf("  in %s · cache read %s · written %s · out %s",
		tokens(u.Input), tokens(u.CacheRead), tokens(u.CacheWrite5m+u.CacheWrite1h), tokens(u.Output))))
	if p.Context > 0 {
		win := claude.ContextWindow(p.Model)
		pct := float64(p.Context) / float64(win) * 100
		add(label("CONTEXT") + ctxBar(pct) + fmt.Sprintf(" %.0f%%", pct) + dim(fmt.Sprintf("  %s of %s tokens", tokens(p.Context), tokens(win))))
	}
	add(label("TIME") + dur(a.Elapsed(now)) + dim(" since "+a.CreatedAt.Local().Format("Mon 15:04")))
	for _, pr := range a.PRs {
		add(label("PR") + fmt.Sprintf("#%d %s", pr.Number, strings.ToLower(pr.State)) +
			dim(fmt.Sprintf(" · checks %d✓ %d✗ %d…", pr.Checks.Passed, pr.Checks.Failed, pr.Checks.Pending)))
	}
	if a.PID != 0 && m.snap.Table != nil {
		add("", label("TREE")+dim(fmt.Sprintf("%d processes · %s · %.1f%% cpu", a.Procs, mem(a.Mem), a.CPU)))
		nodes := m.snap.Table.Tree(a.PID)
		shown := 0
		for _, n := range nodes {
			if shown >= 6 {
				add(dim(fmt.Sprintf("       └ %d more", len(nodes)-shown)))
				break
			}
			ind := strings.Repeat(" ", min(n.Depth, 4))
			add("       " + fit(ind+m.shortCmd(n.PID, n.Comm), w-24) + cpuColor(n.CPU, right(fmt.Sprintf("%.1f%%", n.CPU), 8)) + memColor(n.Footprint, right(mem(n.Footprint), 8)))
			shown++
		}
	} else if a.Live() {
		add("", label("TREE")+dim("no process found"))
	}
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func (m *Model) procBody() []string {
	rows := m.procRows()
	w := m.w - 4
	title := "Processes"
	sub := "whole machine"
	if !m.procMachine {
		if a := m.selected(); a != nil {
			sub = a.DisplayName
		}
	}
	out := []string{paint(cText+bold, title) + dim("  ·  "+sub), ""}
	num := func(r procRow) string {
		return cpuColor(r.cpu, right(fmt.Sprintf("%.1f%%", r.cpu), 8)) + memColor(r.mem, right(mem(r.mem), 8))
	}
	lastRole := fleet.Role(-1)
	for i, r := range rows {
		var line string
		if m.procMachine {
			if r.role != lastRole {
				if lastRole != -1 {
					out = append(out, "")
				}
				lastRole = r.role
				out = append(out, rule(roleName(r.role), "", w))
			}
			lbl := paint(cText, fit(r.label, 34))
			if r.role == fleet.RoleOrphan {
				lbl = paint(cYellow, fit(r.label, 34))
			}
			procs := ""
			if r.n > 1 {
				procs = fmt.Sprintf("%d procs", r.n)
			}
			line = "   " + faint(fit(fmt.Sprintf("%d", r.pid), 7)) + lbl + num(r) + dim(right(procs, 10)) + "   " + dim(trimCmd(r.cmd, max(10, w-72)))
		} else {
			cmd := strings.Repeat("  ", min(r.depth, 8)) + r.cmd
			line = "   " + faint(fit(fmt.Sprintf("%d", r.pid), 7)) + paint(cSub, fit(trimCmd(cmd, w-30), w-30)) + num(r)
		}
		if i == m.procCursor {
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		}
		out = append(out, line)
	}
	if len(rows) == 0 {
		out = append(out, dim("This agent has no running process.  tab shows the whole machine."))
	}
	if m.procMachine {
		mc := m.snap.Machine
		out = append(out, "", dim(fmt.Sprintf("%s · %.0f%% cpu in total", mem(mc.TotalMem), mc.TotalCPU)))
	}
	return out
}

func roleName(r fleet.Role) string {
	switch r {
	case fleet.RoleDaemon:
		return "Daemon"
	case fleet.RoleView:
		return "Agent views"
	case fleet.RoleWorker:
		return "Agents"
	case fleet.RoleSpare:
		return "Spares — pre-warmed, ready for the next session"
	case fleet.RoleOrphan:
		return "Leftovers — the session that started these has ended"
	default:
		return "Other Claude processes"
	}
}

func (m *Model) cwdBody() []string {
	var name, from string
	for _, a := range m.snap.Agents {
		if a.Key == m.cwdFor {
			name, from = a.DisplayName, tildify(a.Cwd)
		}
	}
	out := []string{paint(cText+bold, "Change repo") + dim(" · "+name+" · now in "+from), ""}
	out = append(out, "  "+dim("Folder ❯ ")+string(m.input)+paint(cOrange, "▏"), "")
	for i, c := range m.cwdChoices() {
		if i >= max(4, m.h-20) {
			break
		}
		cur := "    "
		if i == m.cwdCursor {
			cur = paint(cOrange, "  › ")
		}
		out = append(out, cur+tildify(c))
	}
	mv, ad := "( )", "( )"
	if m.cwdMove {
		mv = "(•)"
	} else {
		ad = "(•)"
	}
	out = append(out, "",
		"  "+dim("Mode   ")+paint(cOrange, mv)+" Move: relaunch this conversation there  "+dim("stops it, resumes it in the new folder, tells Claude"),
		"         "+paint(cOrange, ad)+" Add: also allow this folder             "+dim("keeps the working folder, adds this one"),
		"", dim("  enter apply · tab mode · ↑↓ pick · esc cancel"))
	return out
}

func (m *Model) helpBody() []string {
	type group struct {
		title string
		rows  [][2]string
	}
	left := []group{
		{"Move & open", [][2]string{
			{"↑ ↓", "move"}, {"enter", "open the agent · fold a section"}, {"← →", "fold · unfold · back"},
			{"tab", "preview · again for full screen"}, {"shift+↑ ↓", "taller or shorter preview"}, {"ctrl+]", "leave an open agent"},
		}},
		{"Manage", [][2]string{
			{"ctrl+o", "reply without opening"}, {"ctrl+r", "rename"}, {"ctrl+t", "pin"}, {"ctrl+f", "done / back"},
			{"ctrl+x", "stop · twice to delete"}, {"ctrl+e", "put in a group"}, {"ctrl+l", "move to another folder"},
		}},
	}
	right := []group{
		{"Views", [][2]string{
			{"ctrl+s", "group by status, repo, account…"}, {"ctrl+p", "processes · CPU and RAM"},
			{"ctrl+a", "accounts"}, {"ctrl+g", "coding agents & settings"},
		}},
		{"New sessions", [][2]string{
			{"type + enter", "start one"}, {"ctrl+n ctrl+b", "choose its folder"}, {"with preview", "enter sends a reply"},
		}},
		{"Commands", [][2]string{
			{"/done /stop /rm", "same as the keys"}, {"/cd /add-dir", "move or grant a folder"}, {"/native", "open the native view"},
		}},
	}
	col := func(gs []group) []string {
		var out []string
		for i, g := range gs {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, paint(cSub+bold, g.title))
			for _, r := range g.rows {
				out = append(out, paint(cOrange, fit(r[0], 18))+dim(r[1]))
			}
		}
		return out
	}
	l, r := col(left), col(right)
	out := []string{paint(cText+bold, "Keys") + faint("   any key closes"), ""}
	for i := 0; i < max(len(l), len(r)); i++ {
		a, b := "", ""
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out = append(out, fit(a, 52)+"  "+b)
	}
	return out
}

// shortCmd is a process's command with the home folder and binary paths trimmed.
func (m *Model) shortCmd(pid int, comm string) string {
	args := proc.Args(pid)
	if len(args) == 0 {
		return comm
	}
	args[0] = filepath.Base(args[0])
	cmd := oneLine(strings.Join(args, " "))
	return strings.TrimRight(ansi.Strip(trimCmd(cmd, 200)), " ")
}
