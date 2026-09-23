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

// clanker's face follows the fleet's mood.
func (m *Model) face() (string, string) {
	var working, blocked int
	var today float64
	for _, a := range m.snap.Agents {
		switch {
		case a.State == "blocked":
			blocked++
		case a.Live():
			working++
		}
	}
	for _, av := range m.snap.Accounts {
		today += av.Today
	}
	switch {
	case blocked > 0:
		return "°□°", "!"
	case today >= 500 && m.tick%6 < 2:
		return "$_$", " "
	case working > 0:
		if m.tick%7 == 0 {
			return "-_-", " "
		}
		if m.tick%2 == 0 {
			return "◉_◉", " "
		}
		return "◉‿◉", " "
	default:
		if m.tick%2 == 0 {
			return "-_-", "z"
		}
		return "-_-", "Z"
	}
}

func (m *Model) clanker() [4]string {
	eyes, side := m.face()
	o := func(s string) string { return paint(cOrange, s) }
	return [4]string{
		o("   ╻    ") + " ",
		o(" ┌─┴─┐  ") + " ",
		o(" │") + paint(cWhite+bold, eyes) + o("│") + " " + paint(cYellow, side) + " ",
		o(" └┬─┬┘  ") + " ",
	}
}

func (m *Model) header() []string {
	robot := m.clanker()
	var working, blocked, done int
	for _, a := range m.snap.Agents {
		switch {
		case a.State == "blocked":
			blocked++
		case a.Live():
			working++
		default:
			done++
		}
	}
	cli := ""
	for _, a := range m.snap.Agents {
		if a.CLIVersion != "" {
			cli = a.CLIVersion
			break
		}
	}
	acct := m.store.Config.ActiveAccount()
	title := paint(cWhite+bold, "agtop "+m.version) + dim(" · clanker wrangler")
	if cli != "" {
		title += dim(" · Claude Code v" + cli)
	}
	where := acct.Name + " · " + tildify(m.launchDir)
	counts := fmt.Sprintf("%d awaiting input · %d working · %d completed", blocked, working, done)
	var today float64
	for _, av := range m.snap.Accounts {
		today += av.Today
	}
	mc := m.snap.Machine
	metrics := dim("cpu ") + fmt.Sprintf("%.0f%%", mc.TotalCPU) + dim("  ram ") + mem(mc.TotalMem) +
		dim("  today ") + money(today)
	if mc.Spares > 0 {
		metrics += dim(fmt.Sprintf("  spares %d idle · %s", mc.Spares, mem(mc.SpareMem)))
	}
	if !m.loaded {
		metrics += dim("  costing transcripts…")
	}
	lines := []string{
		robot[0] + title,
		robot[1] + where,
		robot[2] + counts + "   " + metrics,
		robot[3] + m.usageLine(),
	}
	return lines
}

func (m *Model) usageLine() string {
	var parts []string
	for _, av := range m.snap.Accounts {
		mark := dim("○ ")
		name := dim(av.Name)
		if av.Current {
			mark, name = paint(cOrange, "● "), paint(cOrange, av.Name)
		}
		u := av.Usage
		s := mark + name
		if u.FiveHour.Present {
			s += dim(" 5h ") + bar(u.FiveHour.Percent) + fmt.Sprintf(" %.0f%%", u.FiveHour.Percent)
			if !u.FiveHour.ResetsAt.IsZero() {
				s += dim(" resets " + u.FiveHour.ResetsAt.Local().Format("15:04"))
			}
		}
		if u.SevenDay.Present {
			s += dim(" · 7d ") + fmt.Sprintf("%.0f%%", u.SevenDay.Percent)
		}
		if !u.FetchedAt.IsZero() && m.snap.At.Sub(u.FetchedAt) > time.Hour {
			s = ansi.Strip(s)
			s = dim(s + " (as of " + u.FetchedAt.Local().Format("Mon 15:04") + ")")
		}
		parts = append(parts, s)
	}
	out := strings.Join(parts, "   ")
	if len(m.snap.Accounts) > 1 {
		out += dim("   ctrl+a switch")
	}
	return out
}

// cols decides which extra columns fit, widest terminal first.
type cols struct {
	name, detail, badge  int
	cpu, ram, cost, time bool
}

func (m *Model) layout(w int, compact bool) cols {
	c := cols{name: 38, badge: 7, cpu: !compact, ram: !compact, cost: true, time: !compact}
	if compact {
		c.name = 30
	}
	fixed := func() int {
		n := 2 + c.name + 2 + c.badge + 5
		if c.cpu {
			n += 7
		}
		if c.ram {
			n += 7
		}
		if c.cost {
			n += 8
		}
		if c.time {
			n += 7
		}
		return n
	}
	for _, drop := range []*bool{&c.time, &c.cpu, &c.ram, &c.cost} {
		if w-fixed() >= 24 {
			break
		}
		*drop = false
	}
	if w-fixed() < 16 {
		c.name = max(16, w-fixed()-16+c.name)
	}
	c.detail = max(8, w-fixed())
	return c
}

func (m *Model) colHeader(c cols) string {
	s := strings.Repeat(" ", 2+c.name+2+c.detail+c.badge)
	if c.cpu {
		s += right("CPU", 7)
	}
	if c.ram {
		s += right("RAM", 7)
	}
	if c.cost {
		s += right("COST", 8)
	}
	if c.time {
		s += right("TIME", 7)
	}
	return faint(s + right("", 5))
}

func (m *Model) row(a *fleet.Agent, c cols, selected bool) string {
	now := m.snap.At
	glyph := dim("∙")
	switch {
	case a.State == "blocked":
		glyph = paint(cYellow, "●")
	case a.Live():
		glyph = paint(cOrange, spinner[m.tick%len(spinner)])
	case a.Done:
		glyph = paint(cGreen, "✓")
	}
	name := fit(oneLine(a.DisplayName), c.name)
	if selected {
		glyph = paint(cOrange, "›")
		name = paint(cWhite+bold, name)
	}
	detail := oneLine(a.Detail)
	if detail == "" {
		detail = oneLine(a.Intent)
	}
	detailColor := ""
	switch {
	case a.State == "blocked" && a.Needs != "":
		detail, detailColor = oneLine(a.Needs), cYellow
	case a.State == "stopped" || detail == "stopped":
		detailColor = cDim
	}
	d := fit(detail, c.detail)
	if detailColor != "" {
		d = paint(detailColor, d)
	}
	s := glyph + " " + name + "  " + d + m.badge(a, c.badge)
	if c.cpu {
		v := "–"
		if a.Worker != nil {
			v = fmt.Sprintf("%.1f%%", a.CPU)
		}
		s += cpuColor(a.CPU, right(v, 7))
	}
	if c.ram {
		s += memColor(a.Mem, right(mem(a.Mem), 7))
	}
	if c.cost {
		v := money(a.Spend.Cost)
		if !a.Spend.Ready {
			v = "…"
		}
		s += right(v, 8)
	}
	if c.time {
		s += dim(right(dur(a.Elapsed(now)), 7))
	}
	return s + right(age(a.Age(now)), 5)
}

func (m *Model) badge(a *fleet.Agent, w int) string {
	var b string
	switch len(a.PRs) {
	case 0:
	case 1:
		pr := a.PRs[0]
		col := cGreen
		switch pr.State {
		case "MERGED":
			col = cBlue
		case "CLOSED":
			col = cRed
		case "DRAFT":
			col = cDim
		}
		if pr.Checks.Failed > 0 && pr.State != "MERGED" && pr.State != "CLOSED" {
			col = cYellow
		}
		b = paint(col, fmt.Sprintf("#%d", pr.Number))
	default:
		b = fmt.Sprintf("%d PRs", len(a.PRs))
	}
	if a.Children > 0 {
		if b != "" {
			b += " "
		}
		if a.Children > 1 {
			b += fmt.Sprintf("%d ⧉", a.Children)
		} else {
			b += "⧉"
		}
	}
	return right(b, w)
}

func (m *Model) render() string {
	if m.w == 0 {
		return ""
	}
	switch m.mode {
	case modeHelp:
		return m.frame(m.helpBody(), "any key to go back")
	case modeProcs:
		return m.frame(m.procBody(), "")
	case modeAccounts:
		return m.frame(m.acctBody(), "")
	case modeCwd:
		return m.frame(m.cwdBody(), "")
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
			b.WriteString(fit(body[i], m.w))
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
		s := paint(cWhite+bold, c.question) + "  " + dim(c.detail) + "   " + paint(cOrange, "[y]") + " yes"
		if c.onBang != nil && c.bangText != "" {
			s += "  " + paint(cOrange, "[!]") + " " + c.bangText
		}
		return fit(" "+s+"  "+paint(cOrange, "[n]")+" cancel", m.w)
	}
	if m.status != "" && m.snap.At.Sub(m.statusAt).Seconds() < 6 {
		c := cDim
		if m.statusErr {
			c = cRed
		}
		return fit("  "+paint(c, m.status), m.w)
	}
	return fit("  "+dim(hint), m.w)
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
		pane = m.previewLines(paneW-2, bodyH)
	}
	var b strings.Builder
	for _, l := range head {
		b.WriteString(fit(l, m.w))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for i := 0; i < bodyH; i++ {
		switch {
		case listW > 0 && paneW > 0:
			l, p := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(pane) {
				p = pane[i]
			}
			b.WriteString(fit(l, listW) + faint("│") + " " + fit(p, paneW-2))
		case listW > 0:
			if i < len(left) {
				b.WriteString(fit(left[i], m.w))
			}
		default:
			if i < len(pane) {
				b.WriteString(" " + fit(pane[i], m.w-1))
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.promptBlock())
	return b.String()
}

func (m *Model) listLines(w, h int) []string {
	c := m.layout(w, m.preview)
	var all []string
	selLine := 0
	all = append(all, m.colHeader(c))
	for _, l := range m.lines {
		switch {
		case l.agent != nil:
			sel := l.agent.Key == m.sel
			if sel {
				selLine = len(all)
			}
			r := m.row(l.agent, c, sel)
			if m.armed == l.agent.Key {
				r = paint(cRed, "×") + r[strings.IndexByte(r, ' '):]
			}
			all = append(all, r)
		case l.extra != "":
			all = append(all, "    "+dim(l.extra))
		case l.header != "":
			name, meta, _ := strings.Cut(l.header, "\x00")
			hl := dim(name)
			if meta != "" {
				hl = fit(hl, w-ansi.StringWidth(meta)-1) + " " + faint(meta)
			}
			all = append(all, hl)
		default:
			all = append(all, "")
		}
	}
	if len(m.order) == 0 {
		all = append(all, dim("  No background agents yet. Describe a task below to start one."))
	}
	if selLine < m.scroll+1 {
		m.scroll = max(0, selLine-1)
	}
	if selLine >= m.scroll+h-1 {
		m.scroll = selLine - h + 2
	}
	if m.scroll > len(all)-h {
		m.scroll = max(0, len(all)-h)
	}
	end := min(len(all), m.scroll+h)
	out := all[m.scroll:end]
	if end < len(all) {
		out[len(out)-1] = dim(fmt.Sprintf("… %d more", len(all)-end))
	}
	return out
}

func (m *Model) promptBlock() string {
	rule := faint(strings.Repeat("─", m.w))
	label := "❯ "
	placeholder := "describe a task for a new session"
	a := m.selected()
	switch {
	case m.inKind == inRename:
		label, placeholder = paint(cOrange, "rename ❯ "), "new name — enter to save, empty to reset"
	case m.inKind == inGroup:
		label, placeholder = paint(cOrange, "group ❯ "), "group name — empty to clear"
	case m.preview && a != nil:
		label = paint(cOrange, "reply to "+fit(a.DisplayName, 24)+" ❯ ")
		placeholder = "type a message for this agent"
	}
	text := string(m.input)
	var line string
	if text == "" {
		line = label + dim(placeholder)
	} else {
		line = label + text + paint(cOrange, "▏")
	}
	hint := "enter to open · ctrl+r rename · ctrl+x stop · ctrl+t pin · ctrl+s group by · tab preview · ctrl+f done · ctrl+p processes · ctrl+a accounts · ctrl+l move · / commands · ? for shortcuts"
	if m.preview {
		hint = "type + enter send to this agent · enter open it · ← close preview · ctrl+p processes · ctrl+l move · ctrl+f done"
	}
	return rule + "\n" + fit(line, m.w) + "\n" + rule + "\n" + m.statusOr(hint)
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
	add(paint(cWhite+bold, oneLine(a.DisplayName)))
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
	add(label("SPEND") + paint(cWhite+bold, money(a.Spend.Cost)) + dim(fmt.Sprintf("  in %s · cache read %s · written %s · out %s",
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
	var out []string
	title := "Processes · "
	if m.procMachine {
		title += "whole machine"
	} else if a := m.selected(); a != nil {
		title += a.DisplayName
	}
	out = append(out, paint(cWhite+bold, title)+dim("   tab agent/machine · enter jump to agent · ctrl+x SIGTERM · ! SIGKILL tree · esc back"), "")
	out = append(out, faint(fit("   PID    WHAT", 52)+right("CPU", 8)+right("RAM", 8)+right("PROCS", 7)+"   COMMAND"))
	lastRole := fleet.Role(-1)
	for i, r := range rows {
		if m.procMachine && r.role != lastRole {
			lastRole = r.role
			out = append(out, dim(roleName(r.role)))
		}
		cur := "  "
		if i == m.procCursor {
			cur = paint(cOrange, "› ")
		}
		what := strings.Repeat("  ", min(r.depth, 6)) + r.label
		lbl := fit(what, 44)
		if r.role == fleet.RoleOrphan {
			lbl = paint(cYellow, lbl)
		}
		line := cur + fit(fmt.Sprintf("%-6d", r.pid), 7) + lbl +
			cpuColor(r.cpu, right(fmt.Sprintf("%.1f%%", r.cpu), 8)) + memColor(r.mem, right(mem(r.mem), 8)) + right(fmt.Sprintf("%d", r.n), 7) +
			"   " + dim(trimCmd(r.cmd, max(10, m.w-80)))
		out = append(out, line)
	}
	if len(rows) == 0 {
		out = append(out, dim("  This agent has no running process. tab shows the whole machine."))
	}
	if m.procMachine {
		mc := m.snap.Machine
		out = append(out, "", dim(fmt.Sprintf("total %s · %.1f%% cpu across %d rows", mem(mc.TotalMem), mc.TotalCPU, len(rows))))
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
	out := []string{paint(cWhite+bold, "Accounts") + dim("   enter use for new sessions · m move selected agent here · l sign in · n add · esc back"), ""}
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
	out := []string{paint(cWhite+bold, "Change repo") + dim(" · "+name+" · now in "+from), ""}
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
	out := []string{paint(cWhite+bold, "Shortcuts"), ""}
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
	return trimCmd(strings.Join(args, " "), 200)
}
