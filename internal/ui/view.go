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
	blocked, working, done int
	today                  float64
}

func (m *Model) tally() tally {
	var t tally
	for _, a := range m.snap.Agents {
		switch {
		case a.State == "blocked":
			t.blocked++
		case a.Live():
			t.working++
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
		return m.frame(m.helpBody(), "any key to go back")
	case modeProcs:
		return m.frame(m.procBody(), keys("tab", "agent / machine", "enter", "jump to agent", "ctrl+x", "SIGTERM", "!", "SIGKILL tree", "esc", "back"))
	case modeCwd:
		return m.frame(m.cwdBody(), keys("enter", "apply", "tab", "move / add", "↑↓", "pick", "esc", "cancel"))
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

// keys renders "key label" pairs with the key brighter than its label.
func keys(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, paint(cSub, pairs[i])+" "+dim(pairs[i+1]))
	}
	return strings.Join(parts, faint("  ·  "))
}

func (m *Model) listView() string {
	head := m.header()
	listW, paneW := m.w, 0
	if m.preview {
		if m.w >= 120 && !m.full {
			paneW = m.w * 55 / 100
			listW = m.w - paneW - 1
		} else {
			paneW, listW = m.w, 0
		}
	}
	bodyH := m.h - len(head) - 1 - 4
	if bodyH < 3 {
		bodyH = 3
	}
	m.listTop = len(head) + 1
	m.rowKeys = nil
	var left []string
	if listW > 0 {
		left = m.listLines(listW, bodyH)
	}
	var pane []string
	if paneW > 0 {
		pane = m.previewLines(paneW-3, bodyH)
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
	b.WriteString(m.promptBlock())
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
		case lineCard:
			if focus == nil || focus.Key != l.agent.Key || m.preview {
				continue
			}
			for _, c := range m.cardLines(l.agent, w) {
				emit(c, l.agent.Key, l.agent.Key == m.sel)
			}
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

// cardLines are the details a focused row opens up to: what it is doing,
// what it said last, how full its context is, and what it has cost.
func (m *Model) cardLines(a *fleet.Agent, w int) []string {
	p := m.previews[a.Key].p
	inner := w - 8
	var out []string
	add := func(s string) { out = append(out, "      "+s) }
	if a.Live() && p.Tool != "" {
		add(paint(cOrange, "● ") + paint(cText, p.Tool) + "  " + dim(fit(oneLine(tildify(p.ToolArg)), inner-len(p.Tool)-4)))
	}
	text := p.Text
	if !a.Live() && strings.TrimSpace(a.Detail) != "" && a.Detail != "stopped" {
		text = a.Detail
	}
	if text != "" {
		lines := wrap(oneLine(text), inner)
		for i, l := range lines {
			if i == 3 {
				add(faint("…  tab for the full preview"))
				break
			}
			add(paint(cSub, l))
		}
	}
	var meta []string
	if p.Context > 0 {
		win := claude.ContextWindow(p.Model)
		pct := float64(p.Context) / float64(win) * 100
		meta = append(meta, dim("context ")+ctxBar(pct)+dim(fmt.Sprintf(" %.0f%% of %s", pct, tokens(win))))
	}
	u := a.Spend.Usage
	if a.Spend.Cost > 0 {
		meta = append(meta, dim("spent ")+paint(cText, money(a.Spend.Cost))+dim(fmt.Sprintf(" · %s out · %s cache read", tokens(u.Output), tokens(u.CacheRead))))
	}
	if a.PID != 0 && a.Procs > 0 {
		meta = append(meta, dim(fmt.Sprintf("%d processes · %s", a.Procs, mem(a.Mem))))
	}
	if !a.Live() {
		if el := a.Elapsed(m.snap.At); el > 0 {
			meta = append(meta, dim("ran "+dur(el)))
		}
		if c := m.context(a); c != "" {
			meta = append(meta, faint(c))
		}
	}
	if len(meta) > 0 {
		add(strings.Join(meta, faint("   ·   ")))
	}
	if len(out) == 0 {
		add(faint("loading…"))
	}
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
		if l.kind != lineAgent || l.agent.Live() {
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
	if a.Interactive && !live {
		marker = faint("○")
	}
	switch {
	case a.State == "blocked":
		marker = paint(cYellow, "●")
	case live:
		marker = paint(cOrange, spinner[(m.tick+len(a.ID))%len(spinner)])
	case a.Done:
		marker = paint(cGreen, "✓")
	}

	var right string
	if live {
		cpu, ram := "", ""
		if a.PID != 0 {
			cpu = cpuColor(a.CPU, right1(fmt.Sprintf("%.0f%%", a.CPU), wCPU))
			ram = memColor(a.Mem, right1(mem(a.Mem), wRAM))
		} else {
			cpu, ram = strings.Repeat(" ", wCPU), strings.Repeat(" ", wRAM)
		}
		right = dim(cpu) + ram + paint(cText, right1(money(a.Spend.Cost), wCost))
	} else {
		resident := ""
		if a.Worker != nil && a.Mem > 0 {
			resident = paint(cYellow, right1("● "+mem(a.Mem), wCPU+wRAM))
		} else {
			resident = strings.Repeat(" ", wCPU+wRAM)
		}
		cost := money(a.Spend.Cost)
		if cost == "–" {
			cost = ""
		}
		right = resident + dim(right1(cost, wCost))
	}
	if live {
		right += strings.Repeat(" ", wAge) + " "
	} else {
		right += faint(right1(age(a.Age(now)), wAge)) + " "
	}

	nameColor := cSub
	switch {
	case live || sel:
		nameColor = cText + bold
	case a.Pinned:
		nameColor = cText
	case a.Done:
		nameColor = cDim
	}
	name := oneLine(a.DisplayName)
	badges := m.badges(a)
	summary := ""
	if !live && !m.expanded[a.Key] {
		summary = oneLine(a.Detail)
		if summary == "stopped" || summary == "" {
			summary = ""
		}
	}
	room := w - 3 - ansi.StringWidth(right)
	left := paint(nameColor, name)
	if badges != "" {
		left += " " + badges
	}
	switch {
	case live:
		if ctx := m.context(a); ctx != "" && ctx != m.sharedContext() {
			left += "   " + faint(ctx)
		}
	case summary != "":
		left = fit(left, nameCol)
		if sw := room - nameCol - 2; sw > 8 {
			left += "  " + dim(fit(summary, sw))
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
	if !a.Live() && m.expanded[a.Key] {
		text = oneLine(a.Detail) + dim("  ·  "+tildify(a.Cwd))
	}
	tail := ""
	if a.Live() {
		tail = faint(dur(a.Elapsed(m.snap.At)) + " running")
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
	if a.Children > 0 {
		parts = append(parts, faint(fmt.Sprintf("⧉%d", a.Children)))
	}
	if a.Interactive {
		parts = append(parts, faint("terminal"))
	}
	return strings.Join(parts, " ")
}

func (m *Model) promptBlock() string {
	ruleLine := faint(strings.Repeat("─", m.w))
	label := paint(cOrange, "❯ ")
	placeholder := "describe a task for a new session"
	a := m.selected()
	switch {
	case m.inKind == inRename:
		label, placeholder = paint(cOrange, "rename ❯ "), "new name · enter to save · empty resets it"
	case m.inKind == inGroup:
		label, placeholder = paint(cOrange, "group ❯ "), "group name · empty clears it"
	case m.preview && a != nil:
		label = dim("reply to ") + paint(cOrange, ansi.Truncate(oneLine(a.DisplayName), 32, "…")+" ❯ ")
		placeholder = "a message for this agent"
	}
	text := string(m.input)
	var line string
	if text == "" {
		line = "  " + label + faint(placeholder)
	} else {
		line = "  " + label + paint(cText, text) + paint(cOrange, "▏")
	}
	hint := keys("enter", "open", "tab", "preview", "ctrl+f", "done", "ctrl+x", "stop", "ctrl+p", "processes", "ctrl+s", "group", "?", "all keys")
	if m.preview {
		hint = keys("enter", "send · empty opens", "←", "close preview", "ctrl+p", "processes", "ctrl+l", "move", "?", "all keys")
	}
	return ruleLine + "\n" + fit(line, m.w) + "\n" + ruleLine + "\n" + m.statusOr(hint)
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
	rows := [][2]string{
		{"↑ ↓  enter", "move · open the agent in Claude Code (← inside it comes back; ctrl+] always does)"},
		{"type + enter", "start a new session; with the preview open, send it to the selected agent"},
		{"ctrl+r", "rename"},
		{"ctrl+x", "stop a running agent; on a stopped one, press twice to delete"},
		{"ctrl+t", "pin to the top"},
		{"ctrl+e", "put in a group (by hand)"},
		{"ctrl+s", "group by: status → repo → account → your groups"},
		{"ctrl+o", "expand the selected row"},
		{"tab  → ←", "preview pane"},
		{"ctrl+f", "move to Done / back (nothing is merged or deleted)"},
		{"ctrl+p", "processes: CPU and RAM for everything an agent started; kill from there"},
		{"ctrl+a", "accounts: usage per account, switch, move an agent to another account"},
		{"ctrl+l", "change repo: move the conversation to another folder, or add one"},
		{"ctrl+c ctrl+c", "exit"},
		{"/done /stop /rm /kill", "the same actions as commands"},
		{"/cd <path>  /add-dir <path>", "move the conversation, or grant another folder"},
		{"/account <name>  /by <mode>", "switch account · group by status, repo, account or group"},
		{"/hibernate <minutes>", "stop finished agents still in memory after that long; 0 turns it off"},
		{"/native", "open the native agents view once"},
	}
	out := []string{paint(cText+bold, "Shortcuts"), ""}
	for _, r := range rows {
		out = append(out, "  "+paint(cOrange, fit(r[0], 30))+r[1])
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
