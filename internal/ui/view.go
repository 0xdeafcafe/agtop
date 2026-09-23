package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "agtop"
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

// face is clanker's mood: the fleet at a glance.
func (m *Model) face(t tally) (string, string) {
	switch {
	case t.blocked > 0:
		return "°□°", cYellow
	case t.today >= 500 && m.tick%6 < 2:
		return "$_$", cOrange
	case t.working > 0:
		if m.tick%9 == 0 {
			return "-_-", cOrange
		}
		return "◉_◉", cOrange
	default:
		return "-_-", cDim
	}
}

func (m *Model) header() []string {
	t := m.tally()
	eyes, col := m.face(t)
	robot := [3]string{
		paint(col, "┌─┴─┐"),
		paint(col, "│") + paint(cText, eyes) + paint(col, "│"),
		paint(col, "└┬─┬┘"),
	}

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

	line := func(r, l, rt string) string {
		body := "  " + r + "  " + l
		gap := m.w - ansi.StringWidth(body) - ansi.StringWidth(rt) - 2
		if gap < 2 {
			return fit(body, m.w)
		}
		return body + strings.Repeat(" ", gap) + rt
	}
	return []string{
		line(robot[0], left1, right1),
		line(robot[1], left2, right2),
		"  " + robot[2],
	}
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
	case modeAccounts:
		return m.frame(m.acctBody(), keys("enter", "use for new sessions", "m", "move selected agent here", "l", "sign in", "n", "add", "esc", "back"))
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
		if m.w >= 120 {
			paneW = m.w * 45 / 100
			listW = m.w - paneW - 1
		} else {
			paneW, listW = m.w, 0
		}
	}
	bodyH := m.h - len(head) - 1 - 4
	if bodyH < 3 {
		bodyH = 3
	}
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
	var all []string
	selTop, selBottom := -1, -1
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			meta := l.meta
			if (l.title == "Working" || l.title == "Needs you") && m.sharedContext() != "" {
				meta += "  ·  " + m.sharedContext()
			}
			all = append(all, "  "+rule(l.title, meta, w-4))
		case lineEarlier:
			all = append(all, "  "+rule("Earlier", l.meta+" · ↓ to show", w-4))
		case lineBlank:
			all = append(all, "")
		case lineAgent, lineSub:
			sel := l.agent.Key == m.sel
			var s string
			if l.kind == lineAgent {
				s = m.agentLine(l.agent, w, sel, nameCol)
			} else {
				s = m.subLine(l.agent, w)
			}
			if sel {
				if selTop < 0 {
					selTop = len(all)
				}
				selBottom = len(all)
				s = paint(cOrange, "▍") + s[1:]
				s = highlight(s, w)
			}
			all = append(all, s)
		}
	}
	if len(m.order) == 0 {
		all = append(all, "", dim("  No background agents yet. Describe a task below to start one."))
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
	if end < len(all) && len(out) > 0 {
		out[len(out)-1] = faint(fmt.Sprintf("  ↓ %d more lines", len(all)-end))
	}
	return out
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
		if a.Worker != nil {
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
		if !a.Live() {
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
	a := m.selected()
	if a == nil {
		return []string{dim("nothing selected")}
	}
	now := m.snap.At
	e := m.previews[a.Key]
	p := e.p
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	label := func(k string) string { return dim(fit(k, 7)) }
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
		for i, l := range wrap(a.Needs, w-7) {
			if i == 0 {
				add(label("NEEDS") + paint(cYellow, l))
			} else {
				add("       " + paint(cYellow, l))
			}
		}
	}
	now2 := oneLine(a.Detail)
	if p.Tool != "" && a.Live() {
		now2 = paint(cOrange, "● ") + p.Tool + "  " + oneLine(p.ToolArg)
	}
	add(label("NOW") + fit(now2, w-7))
	if p.Text != "" {
		lines := wrap(p.Text, w-7)
		limit := max(3, h/3)
		for i, l := range lines {
			if i >= limit {
				add("       " + dim("…"))
				break
			}
			if i == 0 {
				add(label("LAST") + l)
			} else {
				add("       " + l)
			}
		}
	}
	add("")
	u := a.Spend.Usage
	add(label("SPEND") + paint(cText+bold, money(a.Spend.Cost)) + dim(fmt.Sprintf("  in %s · cache read %s · written %s · out %s",
		tokens(u.Input), tokens(u.CacheRead), tokens(u.CacheWrite5m+u.CacheWrite1h), tokens(u.Output))))
	add(label("TIME") + dur(a.Elapsed(now)) + dim(" since "+a.CreatedAt.Local().Format("Mon 15:04")))
	for _, pr := range a.PRs {
		add(label("PR") + fmt.Sprintf("#%d %s", pr.Number, strings.ToLower(pr.State)) +
			dim(fmt.Sprintf(" · checks %d✓ %d✗ %d…", pr.Checks.Passed, pr.Checks.Failed, pr.Checks.Pending)))
	}
	if a.Worker != nil && m.snap.Table != nil {
		add("", label("TREE")+dim(fmt.Sprintf("%d processes · %s · %.1f%% cpu", a.Procs, mem(a.Mem), a.CPU)))
		nodes := m.snap.Table.Tree(a.Worker.PID)
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

func (m *Model) acctBody() []string {
	out := []string{paint(cText+bold, "Accounts") + dim("   enter use for new sessions · m move selected agent here · l sign in · n add · esc back"), ""}
	out = append(out, faint(fit("    ACCOUNT", 18)+fit("PLAN", 22)+fit("5-HOUR", 34)+fit("7-DAY", 22)+right("AGENTS", 8)+right("TODAY", 10)+right("ALL", 10)))
	for i, av := range m.snap.Accounts {
		cur := "  "
		if i == m.acctCursor {
			cur = paint(cOrange, "› ")
		}
		dot := dim("○ ")
		if av.Current {
			dot = paint(cOrange, "● ")
		}
		u := av.Usage
		plan := u.Plan
		if plan == "" {
			plan = "—"
		}
		five, seven := dim("no reading"), dim("—")
		if u.FiveHour.Present {
			five = bar(u.FiveHour.Percent) + fmt.Sprintf(" %3.0f%%", u.FiveHour.Percent)
			if !u.FiveHour.ResetsAt.IsZero() {
				five += dim(" " + u.FiveHour.ResetsAt.Local().Format("15:04"))
			}
		}
		if u.SevenDay.Present {
			seven = bar(u.SevenDay.Percent) + fmt.Sprintf(" %3.0f%%", u.SevenDay.Percent)
		}
		out = append(out, cur+dot+fit(av.Name, 14)+fit(plan, 22)+fit(five, 34)+fit(seven, 22)+
			right(fmt.Sprintf("%d/%d", av.Live, av.Agents), 8)+right(money(av.Today), 10)+right(money(av.Spend), 10))
		asOf := ""
		if !u.FetchedAt.IsZero() {
			asOf = "  usage as of " + u.FetchedAt.Local().Format("Mon 15:04")
		}
		out = append(out, "      "+dim(tildify(av.ConfigDir)+"  "+u.Email+asOf))
	}
	if m.inKind == inNewAccount {
		out = append(out, "", paint(cOrange, "new account ❯ ")+string(m.input)+paint(cOrange, "▏"),
			dim("  a name, optionally followed by a folder (default ~/.claude-<name>); sign-in opens next"))
	}
	if a := m.selected(); a != nil {
		out = append(out, "", dim("selected agent: ")+a.DisplayName+dim(" on "+a.Acct.Name))
	}
	return out
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
