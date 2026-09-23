package ui

import (
	"fmt"
	"path/filepath"
	"sort"
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
		case a.NeedsYou():
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
	// Text sits level with the head and face; the view strip on the legs.
	out[1] = line(robot[1], left1, right1)
	out[2] = line(robot[2], left2, right2)
	var tabs []string
	for i, v := range viewNames {
		if i == m.view {
			tabs = append(tabs, tabOn+" "+v+" "+reset)
		} else {
			tabs = append(tabs, tabOff+" "+v+" "+reset)
		}
	}
	out[3] = "  " + robot[3] + "   " + strings.Join(tabs, " ") + faint("   tab ⇥")
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
		return m.overlayBox(m.listView(), m.helpBody(), min(m.w-4, 124))
	case modeProcs:
		return m.frame(m.procBody(), keysFit(m.w-4, "↑↓", "move", "enter", "go to the agent", "ctrl+x", "SIGTERM", "!", "SIGKILL tree", "tab", "next view", "esc", "back"))
	case modeCwd:
		return m.frame(m.cwdBody(), keysFit(m.w-4, "enter", "apply", "tab", "move / add", "↑↓", "pick", "esc", "cancel"))
	}
	if m.dialog != nil {
		return m.frame(m.dialogBody(m.w-6), "")
	}
	if m.picker != nil {
		return m.overlayBox(m.listView(), m.pickerBody(min(m.w-10, 96)), min(m.w-6, 100))
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
	// Keep the cursor row in view on long lists.
	start := 0
	if cur := m.frameCursor(body); cur >= avail {
		start = cur - avail + 2
	}
	for i := 0; i < avail; i++ {
		if j := start + i; j < len(body) {
			b.WriteString(fit("  "+body[j], m.w))
		}
		b.WriteByte('\n')
	}
	b.WriteString(faint(strings.Repeat("─", m.w)))
	b.WriteByte('\n')
	if d := m.dialog; d != nil && d.confirm != "" {
		hint = paint(cText+bold, d.confirm) + "   " + paint(cOrange, "y") + dim(" yes   ") + paint(cOrange, "n") + dim(" no")
	} else if d != nil && d.asking != "" {
		hint = paint(cOrange, d.asking+" ❯ ") + paint(cText, string(d.input)) + paint(cOrange, "▏")
	}
	b.WriteString(m.statusOr(hint))
	return b.String()
}

// frameCursor finds the highlighted row in a framed body.
func (m *Model) frameCursor(body []string) int {
	for i, l := range body {
		if strings.Contains(l, selBG) {
			return i
		}
	}
	return 0
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
	showing := m.full || m.preview || m.wide()
	if m.zen {
		listW, paneW = 0, m.w // zen is the one agent, full width
		showing = false
	}
	if showing {
		// The list keeps at least a quarter of the screen and 30 columns;
		// if that leaves the pane too narrow to read, there's no split.
		side := m.sideWidth()
		switch {
		case m.full:
			paneW, listW = m.w, 0
		case m.w-side-1 >= minPane:
			listW, paneW = side, m.w-side-1
		case m.preview:
			paneW, listW = m.w, 0
		}
	}
	bodyH = max(3, m.h-len(m.header())-1-len(m.promptLines(m.promptW(listW, paneW))))
	return listW, paneW, bodyH
}

// sideWidth is the list's width in a split: your own share when you've set
// one (alt+← →, dragging the edge, /width), else agtop's; never under a
// quarter of the screen or 30 columns.
func (m *Model) sideWidth() int {
	floor := max((m.w+3)/4, 30)
	if f := m.store.Config.SideWidth; f > 0 {
		return max(floor, min(int(float64(m.w)*f+0.5), m.w/2))
	}
	return max(floor, min(m.w*28/100, 64))
}

// setSideWidth stores the list's share, kept between a quarter and a half.
func (m *Model) setSideWidth(cols int) {
	if m.w <= 0 {
		return
	}
	f := float64(cols) / float64(m.w)
	f = max(0.25, min(f, 0.5))
	m.store.Config.SideWidth = f
	_ = m.store.SaveConfig()
	m.flash(fmt.Sprintf("list width %.0f%%", f*100), false)
}

// minPane is the narrowest pane worth splitting the screen for: a step row
// with its numbers on the right.
const minPane = 84

// promptW keeps the input under the list when a pane sits beside it, so the
// pane's own input is never stacked over ours.
func (m *Model) promptW(listW, paneW int) int {
	if listW > 0 && paneW > 0 {
		return listW
	}
	return m.w
}

// paneH is the pane's height: beside the list it runs past the prompt to the
// bottom of the screen.
func (m *Model) paneH() int {
	listW, paneW, bodyH := m.layout()
	if listW > 0 && paneW > 0 {
		return bodyH + len(m.promptLines(listW))
	}
	return bodyH
}

func (m *Model) listView() string {
	head := m.header()
	listW, paneW, bodyH := m.layout()
	var dock []string
	if paneW == 0 && m.h >= 20+m.dockLines() {
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
	prompt := m.promptLines(m.promptW(listW, paneW))
	split := listW > 0 && paneW > 0
	paneH := bodyH
	if split {
		paneH += len(prompt)
	}
	if bodyH-len(dock) < 5 {
		dock = nil // the list needs a few rows more than the dock does
	}
	bodyH = max(3, bodyH-len(dock))
	m.listTop = len(head) + 1
	m.rowKeys = nil
	var left []string
	m.listW = listW
	if listW > 0 {
		left = append([]string{m.columnHeader(listW)}, m.listLines(listW, bodyH-1)...)
		m.listTop++
	}
	var pane []string
	if paneW > 0 {
		if m.zen && len(m.zenQueue()) == 0 {
			pane = m.zenQuiet(paneW-3, paneH)
		} else if pane = m.agtopPane(paneW-3, paneH); pane == nil {
			// A Claude Code agent's Session: its live screen or a summary,
			// switched with [ ], under the same strip an agtop session has.
			var body []string
			if m.claudeView == 0 {
				body = m.liveLines(paneW - 3)
			}
			if body == nil {
				body = m.previewLines(paneW-3, paneH-1)
			}
			pane = append([]string{m.claudeStrip(paneW - 3)}, body...)
		}
	}
	var b strings.Builder
	m.paneTop = len(head) + 1
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
			b.WriteString(m.side(fit(l, listW), false) + m.divider() + "  " + m.side(fit(p, paneW-3), true))
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
		if split {
			p := ""
			if j := bodyH + i; j < len(pane) {
				p = pane[j]
			}
			b.WriteString(m.side(fit(l, listW), false) + m.divider() + "  " + m.side(fit(p, paneW-3), true))
		} else {
			b.WriteString(fit(l, m.w))
		}
		if i < len(prompt)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Focus: when a session can take the keys, the side without them fades
// back so where you're typing is obvious at a glance; the side with them
// keeps full brightness, an orange marker and an orange box edge.
const fade = "\x1b[2m"

func (m *Model) twoSided() bool {
	return m.listW > 0 && (m.host != nil || (m.live != nil && m.focused() != nil && m.live.key == m.focused().Key))
}

// sessionFocused is whether the keys go to the Session: an agtop
// conversation, or typing into a Claude Code screen.
func (m *Model) sessionFocused() bool { return m.paneFocus || m.embedded }

func (m *Model) side(line string, pane bool) string {
	if !m.twoSided() || pane == m.sessionFocused() {
		return line
	}
	return fade + strings.ReplaceAll(line, reset, reset+fade) + reset
}

// divider leans orange toward the side with focus.
func (m *Model) divider() string {
	if !m.twoSided() || m.sessionFocused() {
		return faint("│")
	}
	return paint(cOrange, "│")
}

// claudeStrip heads a Claude Code agent's Session with its two views, the
// way an agtop session's header carries conversation and overview.
func (m *Model) claudeStrip(w int) string {
	a := m.focused()
	live := a != nil && m.live != nil && m.live.key == a.Key && m.live.ready.Load()
	tab := func(name string, on, avail bool) string {
		switch {
		case on:
			return bgTabOn + paint(cText+bold, " "+name+" ") + reset + bgChrome
		case !avail:
			return faint(" " + name + " ")
		}
		return paint(cSub, " "+name+" ")
	}
	screenOn := m.claudeView == 0 && live
	left := "  " + tab("screen", screenOn, live) + " " + tab("summary", !screenOn, true) + dim("   [ ]")
	right := ""
	switch {
	case m.embedded:
		right = paint(cOrange+bold, "typing into it") + dim(" · ctrl+] comes back")
	case screenOn:
		right = dim("enter types into it · ctrl+f full screen")
	case !live && a != nil && a.Interactive:
		right = dim("open in a terminal")
	case !live:
		right = dim("not running · enter resumes it")
	}
	return onBg(bgChrome, spread(left, right+" ", w), w)
}

// Column widths on the right of a row.
const (
	wAct  = 10
	wCPU  = 6
	wRAM  = 7
	wCost = 8
	wAge  = 5
)

func (m *Model) listLines(w, h int) []string {
	nameCol := m.nameColumn(w)
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

// View tabs are pills: the current one filled orange, the rest a quiet grey.
const (
	tabOn  = "\x1b[1;38;2;24;22;20;48;2;217;119;87m"
	tabOff = "\x1b[38;2;168;162;152;48;2;40;37;34m"
)

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
	case a.Live():
		title = paint(cOrange, "● ") + paint(cText, "working…")
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

// columnHeader names the list's columns; it stays put while the list scrolls.
// colWidths are the list's right-hand columns at width w. A narrow list
// drops the least useful first, so names keep their room: RUNNING and CPU
// under 96 columns, RAM under 64, cost under 44 (the pane header has it).
func colWidths(w int) (act, cpu, ram, cost int) {
	act, cpu, ram, cost = wAct, wCPU, wRAM, wCost
	if w < 96 {
		act, cpu = 0, 0
	}
	if w < 64 {
		ram = 0
	}
	if w < 44 {
		cost = 0
	}
	return
}

func (m *Model) columnHeader(w int) string {
	wAct, wCPU, wRAM, wCost := colWidths(w)
	nameCol := m.nameColumn(w)
	sortBy := m.store.Config.SortBy
	if sortBy == "" {
		sortBy = "name"
	}
	label := func(text, mode string) string {
		if mode == sortBy {
			return text + "▾"
		}
		return text
	}
	col := func(text, mode string, width int) string {
		s := right1(label(text, mode), width)
		if mode == sortBy {
			return paint(cSub+bold, s)
		}
		return faint(s)
	}
	name := label("AGENTS", "name")
	left := "   " + fit(name, nameCol+2)
	if m.twoSided() && !m.sessionFocused() {
		left = paint(cOrange, "▍") + "  " + fit(name, nameCol+2)
	}
	if sortBy == "name" {
		left = "   " + paint(cSub+bold, fit(name, nameCol+2))
	} else {
		left = faint(left)
	}
	left += faint("LATEST")
	if sortBy == "recent" {
		left += paint(cSub+bold, " · by recent activity")
	}
	rightW := wAct + wCPU + wRAM + wCost + wAge + 3
	cols := faint(right1("RUNNING", wAct)) + col("CPU", "cpu", wCPU) + col("RAM", "ram", wRAM) + col("COST", "cost", wCost) + col("TIME", "time", wAge+2) + " "
	gap := w - ansi.StringWidth(left) - rightW
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + cols
}

// headerColumn maps a click on the column header to the sort it selects.
func (m *Model) headerColumn(x int) string {
	w := m.listW
	wAct, wCPU, wRAM, wCost := colWidths(w)
	edges := []struct {
		from int
		mode string
	}{
		{w - 1 - (wAge + 2), "time"},
		{w - 1 - (wAge + 2) - wCost, "cost"},
		{w - 1 - (wAge + 2) - wCost - wRAM, "ram"},
		{w - 1 - (wAge + 2) - wCost - wRAM - wCPU, "cpu"},
		{w - 1 - (wAge + 2) - wCost - wRAM - wCPU - wAct, ""},
	}
	for _, e := range edges {
		if x >= e.from {
			return e.mode
		}
	}
	if x < 3+m.nameColumn(w)+2 {
		return "name"
	}
	return "recent"
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
	if w < 96 {
		// A narrow list is mostly names: give them what the columns leave.
		act, cpu, ram, cost := colWidths(w)
		return max(12, min(widest, w-6-act-cpu-ram-cost-wAge-3))
	}
	return max(20, min(widest, (w-30)*2/5))
}

// agentLine is the first line of a row: marker, name, badges, figures.
func (m *Model) agentLine(a *fleet.Agent, w int, sel bool, nameCol int) string {
	wAct, wCPU, wRAM, wCost := colWidths(w)
	now := m.snap.At
	live := a.Live()
	marker := " "

	switch {
	case a.Checking:
		marker = paint(cSub, "◔")
	case a.JustFinished(now):
		marker = paint(cGreen, "✓")
	case a.NeedsYou():
		marker = paint(cYellow, "●")
	case a.Waiting():
		marker = paint(cYellow, "○")
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
	act := m.activity(a)
	if wAct == 0 {
		act = ""
	}
	cpuCell := func() string {
		if a.PID == 0 {
			return strings.Repeat(" ", wCPU)
		}
		v := right1(fmt.Sprintf("%.0f%%", a.CPU), wCPU)
		switch {
		case a.CPU >= 50:
			return paint(cYellow, v)
		case a.CPU >= 5:
			return paint(cSub, v)
		default:
			return faint(v)
		}
	}
	ramCell := func(active bool) string {
		if a.PID == 0 {
			return strings.Repeat(" ", wRAM)
		}
		v := right1(mem(a.Mem), wRAM)
		switch {
		case a.Mem >= 4<<30:
			return paint(cYellow, v)
		case active:
			return paint(cSub, v)
		default:
			return dim(v)
		}
	}
	var right string
	switch {
	case live || busy:
		right = act + cpuCell() + ramCell(true) + paint(cText, right1(money(a.Spend.Cost), wCost))
	case resident:
		right = act + faint(right1(fmt.Sprintf("%.0f%%", a.CPU), wCPU)) + ramCell(false) + dim(right1(money(a.Spend.Cost), wCost))
	default:
		cost := money(a.Spend.Cost)
		if cost == "–" {
			cost = ""
		}
		right = act + strings.Repeat(" ", wCPU+wRAM) + dim(right1(cost, wCost))
	}
	if live {
		right += faint(right1(dur(a.Elapsed(now)), wAge+2)) + " "
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
	justDone := false
	switch {
	case a.Checking:
		summary, sumColor = "turn ended · checking…", cDim
	case a.JustFinished(now):
		summary, sumColor = oneLine(a.Detail), cDim
		justDone = true
	case a.Waiting():
		summary, sumColor = oneLine(a.Needs), cSub
		if summary == "" {
			summary = oneLine(a.Detail)
		}
	case a.State == "blocked":
		summary, sumColor = oneLine(a.Needs), cYellow
		if summary == "" {
			summary = oneLine(a.Detail)
		}
	case live:
		summary, sumColor = oneLine(a.Detail), cSub
		if p := m.previews[a.Key].p; summary == "" && p.Tool != "" {
			summary = p.Tool + " · " + oneLine(tildify(p.ToolArg))
		}
		if summary == "" && a.Interactive {
			summary = "working in a terminal"
		}
		if summary == "" {
			summary, sumColor = "working…", cDim
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
			if justDone {
				left += "  " + paint(cGreen, "just finished") + faint(" · ") + paint(sumColor, fit(summary, sw-16))
			} else {
				left += "  " + paint(sumColor, fit(summary, sw))
			}
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

// activity is the small column of what an agent is running beside itself:
// subagents (orange, +nested) and background shells.
func (m *Model) activity(a *fleet.Agent) string {
	var parts []string
	if sub := a.Subs; sub.Direct+sub.Nested > 0 {
		v := fmt.Sprintf("↳%d", sub.Direct)
		if sub.Nested > 0 {
			v += fmt.Sprintf("+%d", sub.Nested)
		}
		parts = append(parts, paint(cOrange, v))
	}
	shells := 0
	if a.PID != 0 {
		for _, t := range a.Running {
			if t.Kind == "shell" {
				shells++
			}
		}
	}
	if shells > 0 {
		parts = append(parts, dim(fmt.Sprintf("▸%d", shells)))
	}
	return right1(strings.Join(parts, " "), wAct)
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
	return strings.Join(parts, " ")
}

// promptLines are the input box and two rows of key hints. The box's top
// edge says where the text goes and what enter will do; the agent pane's
// own input is the same box, so the two always read alike.
func (m *Model) promptLines(w int) []string {
	if m.zen {
		return nil // zen answers in the Session's own box
	}
	a := m.selected()
	text := string(m.input)
	b := box{w: w, focused: !m.sessionFocused(), text: m.input, cursor: m.cursorPos(),
		lead: paint(cOrange, "❯ "), maxRows: min(6, max(1, m.h-len(m.header())-1-4-5))}
	switch {
	case m.inKind == inRename && a != nil:
		b.topL = dim("rename ") + paint(cText, oneLine(a.DisplayName)) + dim(" · enter saves")
		b.holder = "a new name · empty resets it"
	case m.inKind == inGroup && a != nil:
		b.topL = dim("group for ") + paint(cText, oneLine(a.DisplayName)) + dim(" · enter saves")
		b.holder = "a group name · empty clears it"
	case (m.inKind == inReply || m.preview) && a != nil && !a.Agtop:
		b.topL = dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 32, "…")) + dim(" · enter sends")
		b.holder = "a message for this agent · esc leaves reply mode"
	case strings.HasPrefix(text, "/"):
		b.topL = dim("command · enter runs it")
	default:
		b.topL = dim("new session in ") + m.dirLabel(m.startDir()) + dim(" · enter starts it")
		b.holder = "describe a task for a new session"
		if len(m.startDirs()) > 1 {
			b.topR = paint(cSub, "ctrl+l") + dim(" folder")
		}
	}
	if a != nil && b.topR == "" {
		sel := dim(tildify(a.Cwd))
		if a.Branch != "" {
			sel += faint(" · " + a.Branch)
		}
		b.footL = sel
	}
	if m.sessionFocused() {
		// The Session has the keys; this box waits, and says how back.
		b.holder = "esc or ← to come back here"
		if m.embedded {
			b.holder = "ctrl+] or click here to come back"
		}
		b.text = nil
	}
	var out []string
	if l := chips(m.images, w); l != "" {
		out = append(out, l)
	}
	out = append(out, b.lines()...)
	row1 := keysFit(w-4, "enter", "open", "ctrl+o", "reply", "F2", "rename", "ctrl+l", "move", "ctrl+t", "pin", "ctrl+x", "stop")
	if a != nil && a.Agtop && !m.paneFocus {
		row1 = keysFit(w-4, "enter · →", "talk to "+ansi.Truncate(oneLine(a.DisplayName), 20, "…"), "F2", "rename", "ctrl+x", "stop")
	}
	row2 := keysFit(w-4, "tab", "views", "→", "session", "ctrl+n", "next needing you", "ctrl+s", "group", "shift+↑↓", "preview size", "?", "all keys")
	if m.inKind == inReply {
		row1 = keysFit(w-4, "enter", "send", "↑↓", "pick another agent", "esc", "leave reply mode")
	}
	if (m.status != "" && m.snap.At.Sub(m.statusAt).Seconds() < 6) || m.confirm != nil {
		row1 = m.statusOr("")
	} else {
		row1 = "  " + row1
	}
	return append(out, fit(row1, w), fit("  "+row2, w))
}

// previewLines is the rich preview: everything about one agent, in ruled
// sections, with the conversation taking whatever height is left.
func (m *Model) previewLines(w, h int) []string {
	a := m.focused()
	if a == nil {
		return []string{"", dim("select an agent to preview it")}
	}
	now := m.snap.At
	p := m.previews[a.Key].p
	section := func(t string) string { return rule(t, "", w) }
	var top, tail []string

	chip := paint(cDim, "finished")
	switch {
	case a.State == "blocked" && !a.Checking:
		chip = paint(cYellow+bold, "needs you")
	case a.Live():
		chip = paint(cOrange+bold, "working")
	case a.Busy():
		chip = paint(cOrange, "background work")
	case a.JustFinished(now):
		chip = paint(cGreen, "just finished")
	case a.PID != 0:
		chip = paint(cSub, "idle")
	}
	top = append(top, paint(cText+bold, oneLine(a.DisplayName))+"   "+chip)
	model := strings.TrimPrefix(p.Model, "claude-")
	if model == "" {
		model = strings.TrimPrefix(a.Spend.Model, "claude-")
	}
	top = append(top, dim(strings.Join(nonEmpty(a.ID, a.Acct.Name, model, "updated "+age(a.Age(now))+" ago"), " · ")))
	loc := paint(cSub, tildify(a.Cwd))
	if a.Branch != "" {
		loc += faint(" · ") + dim(a.Branch)
	}
	top = append(top, loc)
	for _, pr := range a.PRs {
		col := cGreen
		if pr.Checks.Failed > 0 {
			col = cRed
		}
		top = append(top, paint(col, fmt.Sprintf("#%d", pr.Number))+dim(fmt.Sprintf(" %s · checks %d passed, %d failed, %d running", strings.ToLower(pr.State), pr.Checks.Passed, pr.Checks.Failed, pr.Checks.Pending)))
	}

	switch {
	case a.State == "blocked" && a.Needs != "":
		top = append(top, "", section("Needs you"))
		for _, l := range wrap(oneLine(a.Needs), w) {
			top = append(top, paint(cYellow, l))
		}
	case a.Live() && p.Tool != "":
		top = append(top, "", section("Now"))
		top = append(top, paint(cOrange, "● ")+paint(cText+bold, p.Tool)+"  "+dim(ansi.Truncate(oneLine(tildify(p.ToolArg)), w-len(p.Tool)-4, "…")))
	}

	if len(a.TodoItems) > 0 {
		tail = append(tail, "", section(fmt.Sprintf("Todos  %d/%d", a.TodosDone, a.Todos)))
		for i, t := range a.TodoItems {
			if i == 7 {
				tail = append(tail, faint(fmt.Sprintf("+%d more", len(a.TodoItems)-7)))
				break
			}
			switch {
			case t.Done:
				tail = append(tail, paint(cGreen, "☑ ")+faint(fit(t.Label, w-2)))
			case t.Started:
				tail = append(tail, paint(cOrange, "◐ ")+paint(cText, fit(t.Label, w-2)))
			default:
				tail = append(tail, faint("☐ ")+dim(fit(t.Label, w-2)))
			}
		}
	}
	if len(a.Running) > 0 && a.PID != 0 || a.Subs.Spawned > 0 {
		head := "Running"
		if s := a.Subs; s.Spawned > 0 {
			head += fmt.Sprintf("  %d subagents now · %d spawned", s.Direct+s.Nested, s.Spawned)
		}
		tail = append(tail, "", section(head))
		for i, t := range a.Running {
			if i == 5 || a.PID == 0 {
				break
			}
			icon, col := "▸ shell   ", cSub
			if t.Kind == "agent" {
				icon, col = "↳ subagent", cOrange
			} else if t.Kind == "monitor" {
				icon, col = "◎ monitor ", cDim
			}
			since := ""
			if t.StartedAt.Unix() > 0 {
				since = dur(now.Sub(t.StartedAt))
			}
			tail = append(tail, paint(col, icon)+" "+dim(fit(oneLine(tildify(t.Label)), w-20))+faint(right1(since, 7)))
		}
	}
	u := a.Spend.Usage
	tail = append(tail, "", section("Numbers"))
	if p.Context > 0 {
		win := claude.ContextWindow(p.Model)
		pct := float64(p.Context) / float64(win) * 100
		tail = append(tail, dim(fit("context", 10))+ctxBar(pct)+" "+paint(cText, fmt.Sprintf("%.0f%%", pct))+dim(fmt.Sprintf("  %s of %s tokens", tokens(p.Context), tokens(win))))
	}
	tail = append(tail, dim(fit("spent", 10))+paint(cText+bold, money(a.Spend.Cost))+dim(fmt.Sprintf("  in %s · cache read %s · written %s · out %s", tokens(u.Input), tokens(u.CacheRead), tokens(u.CacheWrite5m+u.CacheWrite1h), tokens(u.Output))))
	tail = append(tail, dim(fit("time", 10))+paint(cText, dur(a.Elapsed(now)))+dim(" since "+a.CreatedAt.Local().Format("Mon 15:04")))
	if a.PID != 0 && m.snap.Table != nil {
		tail = append(tail, "", section(fmt.Sprintf("Processes  %d · %s · %.0f%% cpu", a.Procs, mem(a.Mem), a.CPU)))
		nodes := m.snap.Table.Tree(a.PID)
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Footprint > nodes[j].Footprint })
		for i, n := range nodes {
			if i == 4 {
				break
			}
			tail = append(tail, dim(fit(m.shortCmd(n.PID, n.Comm), w-18))+cpuColor(n.CPU, right1(fmt.Sprintf("%.0f%%", n.CPU), 7))+memColor(n.Footprint, right1(mem(n.Footprint), 8)))
		}
	}

	room := h - len(top) - len(tail) - 2
	var conv []string
	if room >= 3 && (len(p.Recent) > 0 || p.First.Text != "") {
		conv = append(conv, "", section("Conversation"))
		asked := func(t string) []string {
			var out []string
			for i, l := range wrap(oneLine(t), w-2) {
				pre := "  "
				if i == 0 {
					pre = paint(cOrange, "› ")
				}
				out = append(out, pre+paint(cText+bold, l))
			}
			return out
		}
		// The message that started it stays on top; the tail fills the rest.
		var first []string
		recent := p.Recent
		if p.First.Text != "" {
			first = asked(p.First.Text)
			if len(first) > 3 {
				first = append(first[:2], ansi.Truncate(first[2], w-3, "")+faint("…"))
			}
			if len(recent) > 0 && recent[0].Role == "user" && recent[0].Text == p.First.Text {
				recent = recent[1:]
			}
		}
		var body []string
		for _, e := range recent {
			switch e.Role {
			case "user":
				body = append(body, asked(e.Text)...)
			case "tool":
				name, arg, _ := strings.Cut(e.Text, "\x00")
				body = append(body, faint("● ")+dim(name)+"  "+faint(ansi.Truncate(oneLine(tildify(arg)), w-len(name)-4, "…")))
			default:
				text := strings.NewReplacer("**", "", "`", "").Replace(oneLine(e.Text))
				lines := wrap(text, w-2)
				if len(lines) > 4 {
					lines = append(lines[:3], ansi.Truncate(lines[3], w-5, "…"))
				}
				for _, l := range lines {
					body = append(body, "  "+paint(cSub, l))
				}
			}
		}
		keep := room - 2 - len(first)
		if keep < 2 {
			first, keep = nil, room-2
		}
		if len(body) > keep {
			body = append([]string{faint("  ⋯")}, body[len(body)-(keep-1):]...)
		}
		conv = append(append(conv, first...), body...)
	}
	out := append(append(top, conv...), tail...)
	if len(out) > h {
		out = out[:h]
	}
	return out
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}

func (m *Model) procBody() []string {
	rows := m.procRows()
	w := m.w - 4
	mc := m.snap.Machine
	out := []string{paint(cText+bold, "Processes") + dim(fmt.Sprintf("  ·  every agent's processes, then the rest of Claude  ·  %s · %.0f%% cpu in total", mem(mc.TotalMem), mc.TotalCPU)), ""}
	num := func(r procRow) string {
		return cpuColor(r.cpu, right(fmt.Sprintf("%.1f%%", r.cpu), 8)) + memColor(r.mem, right(mem(r.mem), 8))
	}
	cmdW := max(10, w-30)
	lastRole, other := fleet.Role(-1), false
	for i, r := range rows {
		var line string
		switch {
		case r.heading:
			out = append(out, "")
			procs := fmt.Sprintf("%d procs", r.n)
			line = "  " + paint(cText+bold, fit(r.label, cmdW-26)) + " " + faint(fit(r.cmd, 24)) + num(r) + dim(right(procs, 10))
		case r.other:
			if !other {
				other = true
				out = append(out, "", rule("Other Claude processes", "", w-2))
			}
			if r.role != lastRole {
				lastRole = r.role
				out = append(out, dim("  "+roleName(r.role)))
			}
			lbl := paint(cSub, fit(r.label, 30))
			if r.role == fleet.RoleOrphan {
				lbl = paint(cYellow, fit(r.label, 30))
			}
			procs := ""
			if r.n > 1 {
				procs = fmt.Sprintf("%d procs", r.n)
			}
			line = "    " + lbl + faint(fit(trimCmd(r.cmd, cmdW-34), cmdW-34)) + num(r) + dim(right(procs, 10))
		default:
			tree := strings.Repeat("  ", min(r.depth, 8))
			line = "  " + faint(fit(fmt.Sprintf("%d", r.pid), 7)) + dim(fit(tree+trimCmd(r.cmd, cmdW), cmdW-5)) + num(r)
		}
		if i == m.procCursor {
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		}
		out = append(out, line)
	}
	if len(rows) == 0 {
		out = append(out, dim("No Claude processes are running."))
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
			{"↑ ↓", "move"}, {"enter", "open the agent · fold a section"},
			{"enter · →", "into the session · type to its agent"}, {"esc · ←", "back to Agents"}, {"ctrl+n", "next agent needing you"},
			{"ctrl+]", "stop typing into a Claude Code screen"}, {"[ ]", "switch the Session view"}, {"ctrl+f", "a Claude Code agent full screen"},
			{"tab", "next view"}, {"shift+↑ ↓", "taller or shorter preview"},
		}},
		{"Manage", [][2]string{
			{"ctrl+o", "reply without opening"}, {"F2", "rename"}, {"ctrl+l", "move to another folder"},
			{"ctrl+t", "pin"}, {"ctrl+x", "stop · twice to delete"}, {"ctrl+y", "open its pull request"},
		}},
		{"Typing", [][2]string{
			{"ctrl · alt + ← →", "move a word"}, {"ctrl · alt + ⌫", "delete a word"},
			{"cmd+⌫ · ctrl+u · ctrl+k", "clear to line start or end"}, {"home · end", "line start and end"},
			{"shift+enter · ctrl+j", "new line"},
		}},
	}
	right := []group{
		{"Views & sorting", [][2]string{
			{"tab", "agents · processes · accounts · coding agents · settings"},
			{"ctrl+s", "group by status, repo, account…"}, {"click a header", "sort by that column"},
		}},
		{"New sessions", [][2]string{
			{"type + enter", "start one"}, {"ctrl+l", "while typing: choose its folder"},
		}},
		{"Commands", [][2]string{
			{"/done /stop /rm /kill", "done, stop, delete, kill"}, {"/cd /add-dir", "move or grant a folder"},
			{"/sort /by", "sort rows · group sections"}, {"/rename /group", "name or group the agent"},
			{"/account /hibernate", "switch account · stop idle agents"}, {"/agtop", "move an agent to agtop mode"},
			{"/native /quit", "native view · quit"},
		}},
		{"Quit", [][2]string{{"esc esc · ctrl+q", "quit"}, {"ctrl+c", "clear the text, twice to quit"}}},
	}
	col := func(gs []group) []string {
		var out []string
		for i, g := range gs {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, paint(cSub+bold, g.title))
			for _, r := range g.rows {
				out = append(out, paint(cOrange, fit(r[0], 22))+dim(r[1]))
			}
		}
		return out
	}
	l, r := col(left), col(right)
	out := []string{paint(cText+bold, "Keys") + faint("   any key closes"), ""}
	if m.w < 110 {
		return append(append(append(out, l...), ""), r...)
	}
	for i := 0; i < max(len(l), len(r)); i++ {
		a, b := "", ""
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out = append(out, fit(a, 58)+"  "+b)
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
