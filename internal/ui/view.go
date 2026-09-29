package ui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
	"github.com/0xdeafcafe/agtop/internal/theme"
)

func (m *Model) View() tea.View {
	defer uiBusy("frame")()
	frame := m.lastFrame
	if !m.sameFrame || frame == "" {
		m.drawing = true
		frame = m.render()
		m.drawing, m.kindMemo = false, kindMemo{}
		m.lastFrame = frame
	}
	m.sameFrame = false
	v := tea.NewView(frame)
	v.AltScreen = true
	v.WindowTitle = m.title()
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true // to know when a notification is worth sending
	return v
}

// title counts the agents waiting on you, so a tab or dock shows it.
func (m *Model) title() string {
	n := 0
	for _, a := range m.snap.Agents {
		if a.NeedsYou() || a.Halted() && !a.Seen {
			n++
		}
	}
	if n == 0 {
		return "agtop"
	}
	if n == 1 {
		return "(1) agtop · 1 agent needs you"
	}
	return fmt.Sprintf("(%d) agtop · %d agents need you", n, n)
}

type tally struct {
	blocked, working, busy, done int
	today                        float64
}

func (m *Model) tally() tally {
	var t tally
	agents := m.snap.Agents
	if m.hosted != "" {
		agents = m.fleetAgents
	}
	for _, a := range agents {
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
	case t.working > 0:
		return moodWorking
	}
	if h := m.snap.At.Hour(); h >= 23 || h < 7 {
		return moodSleepy
	}
	return moodIdle
}

var (
	mdPlain        = strings.NewReplacer("**", "", "`", "", "__", "")
	mdPlainNoUnder = strings.NewReplacer("**", "", "`", "")
)

// headH is the header's height: clanker's, or three lines when the screen
// is too narrow for him beside the text.
func (m *Model) headH() int {
	if m.w < narrowHead {
		return 3
	}
	return 4
}

// topH is the rows above the body: the header and the row under it. Zen
// has none; it's only what needs you.
func (m *Model) topH() int {
	if m.zen {
		return 0
	}
	return m.headH() + 1
}

// narrowHead is the width below which the header drops clanker's body for
// his face and keeps its lines whole.
const narrowHead = 60

func (m *Model) header() []string {
	t := m.tally()
	md := m.mood(t)
	robot := clanker(m.clkState(md, t))
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
	if mc := m.snap.Machine; mc.Orphans > 0 {
		counts = append(counts, paint(cYellow, fmt.Sprintf("%d orphaned · %s", mc.Orphans, mem(mc.OrphanMem)))+dim(" · Machine › Processes to end"))
	}
	left1 := paint(cText+bold, "agtop") + "   " + strings.Join(counts, "   ")

	// Which provider, account and profile new sessions start on.
	acct := m.startTag()
	left2 := acct + dim(" · "+tildify(m.launchDir))
	if m.w < narrowHead {
		// Only his face fits, so it's always him, never the monogram.
		var g clkGrid
		g.sprite(m.clkState(md, t))
		return m.narrowHeader(g.lines(), counts, acct)
	}

	// The right is the top bar you build in /statusline; what's left of
	// the width after clanker and the counts is its room.
	x := &barCtx{m: m, t: t}
	room := func(r, l string) int { return m.w - cellw.String("  "+r+"   "+l) - 4 }
	right1 := m.barLine(barTop, 0, x, room(robot[1], left1))
	right2 := m.barLine(barTop, 1, x, room(robot[2], left2))
	if !m.loaded {
		right2 = dim("costing transcripts…   ") + right2
	}

	line := func(r, l, rt string) string {
		body := "  " + r + "   " + l
		gap := m.w - cellw.String(body) - cellw.String(rt) - 2
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
	// < > (or , .) go between the places; in a Session's box they're
	// text, so it's ctrl+\\ there. The hint goes first when it won't fit.
	places := "< >"
	if m.hosted != "" || m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil {
		places = "ctrl+\\"
	}
	strip := "  " + robot[3] + "   " + strings.Join(m.tabs(), " ")
	out[3] = withTabHint(strip, places, "places", "", m.w)
	return out
}

// withTabHint is a strip of tabs with a quiet hint after it, the keys
// that change tab and what they go through, then rest. The hint is
// dropped first when the row hasn't the room for it.
func withTabHint(strip, keys, what, rest string, w int) string {
	hint := dim("   "+keys) + faint(" "+what)
	if cellw.String(strip)+cellw.String(hint)+cellw.String(rest) > w {
		return strip + rest
	}
	return strip + hint + rest
}

// pages are the pages of the place you're in, the one showing bright; [
// and ] go through them. In Agents it says whether Zen is on.
func (m *Model) pages() string {
	var names []string
	cur, hint := 0, "[ ]"
	switch {
	case m.dialog != nil:
		for _, p := range m.settingsPages() {
			names = append(names, p.name)
		}
		cur = m.dialog.page
	case m.mode == modeProcs || m.mode == modeCleanup:
		names, cur = machinePages, m.machinePage
	case m.mode == modeEff:
		names, cur = effPages, m.eff.page
	case m.mode == modeWall || m.mode == modeWork:
		names, cur = workPages, m.work.page
	case m.zen:
		return "   " + paint(cYellow, "zen") + faint(" ctrl+z")
	case m.hosted != "":
		return "" // no zen in hosted
	default:
		return faint("   ctrl+z zen")
	}
	out := make([]string, len(names))
	for i, n := range names {
		if i == cur {
			out[i] = paint(cText+bold, n)
		} else {
			out[i] = dim(n)
		}
	}
	full := "   " + strings.Join(out, dim(" · ")) + faint("  "+hint)
	// Where the row hasn't the room for every page, the one showing and
	// where it is among them.
	if cellw.String(ansi.Strip(full)) > m.w {
		return "   " + faint("‹ ") + paint(cText+bold, names[cur]) + faint(fmt.Sprintf(" %d/%d › ", cur+1, len(names))) + faint(hint)
	}
	return full
}

func (m *Model) tabs() []string {
	var tabs []string
	for i, v := range viewNames {
		if i == m.view {
			tabs = append(tabs, tabOn+" "+v+" "+reset)
		} else {
			tabs = append(tabs, tabOff+" "+v+" "+reset)
		}
	}
	return tabs
}

// narrowHeader is the header on a narrow screen: clanker's face instead of
// all of him, the folder shortened from the left, and as many tabs as fit
// around the current one, so no line is cut off.
func (m *Model) narrowHeader(robot, counts []string, acct string) []string {
	face := robot[clkFace] // his eyes and brow
	indent := strings.Repeat(" ", cellw.String(face)+3)
	// Whole counts or none: the last ones (finished, orphans) go first.
	title := paint(cText+bold, "agtop")
	for n := len(counts); n >= 0; n-- {
		t := strings.Join(append([]string{paint(cText+bold, "agtop")}, counts[:n]...), "  ")
		if cellw.String(t) <= m.w-cellw.String(face)-4 || n == 0 {
			title = t
			break
		}
	}
	out := []string{
		"  " + face + "  " + fit(title, m.w-cellw.String(face)-4),
		"  " + indent + acct + dim(" · "+shortPath(tildify(m.launchDir), m.w-cellw.String(indent)-cellw.String(ansi.Strip(acct))-5)),
	}
	tabs := m.tabs()
	room := m.w - cellw.String(indent) - 4
	width := func(ts []string) int { return cellw.String(strings.Join(ts, " ")) }
	start := 0
	for start < m.view && width(tabs[start:m.view+1]) > room-2 {
		start++
	}
	end := m.view + 1
	for end < len(tabs) && width(tabs[start:end+1]) <= room-2 {
		end++
	}
	line := strings.Join(tabs[start:end], " ")
	if start > 0 {
		line = faint("‹ ") + line
	}
	if end < len(tabs) {
		line += faint(" ›")
	}
	return append(out, "  "+indent+line)
}

// shortPath keeps a path's last folders, as many as fit in w.
func shortPath(p string, w int) string {
	if cellw.String(p) <= w {
		return p
	}
	parts := strings.Split(p, "/")
	s := parts[len(parts)-1]
	for i := len(parts) - 2; i >= 0; i-- {
		next := parts[i] + "/" + s
		if cellw.String(next)+2 > w {
			break
		}
		s = next
	}
	return fit("…/"+s, w)
}

// resetIn says when a usage window resets: the clock time and how long
// until then for the 5-hour window, the day and time for the 7-day one.
func resetIn(at, now time.Time, week bool) string {
	if at.IsZero() {
		return ""
	}
	if !at.After(now) {
		return faint(" reset") // read before a reset that has since passed
	}
	return faint(" ↻" + roughly(at.Sub(now)))
}

// roughly is a duration to one unit: 40m, 3h, 5d.
func roughly(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(1, int(d.Round(time.Minute).Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Round(time.Hour).Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Round(24*time.Hour).Hours()/24))
}

// usageMeter is one plan window: a bar of what's used, coloured by how it
// is going, with a tick for how far through the window we are, so being
// ahead of pace shows at a glance; then the percentage and when it resets.
func usageMeter(label string, pct float64, resets time.Time, window time.Duration, now time.Time) string {
	const w = 10
	pace := -1.0 // share of the window gone, when we know when it ends
	if !resets.IsZero() && resets.After(now) {
		pace = 1 - float64(resets.Sub(now))/float64(window)
	}
	c := cGreen
	switch {
	case pct >= 80:
		c = cRed
	case pct >= 50 || (pace >= 0 && pct/100 > pace+0.1):
		c = cYellow // high, or burning faster than the window allows
	}
	fill := min(w, max(0, int(pct/100*w+0.5)))
	if pct > 0 && fill == 0 {
		fill = 1
	}
	tick := -1
	if pace >= 0 {
		tick = min(w-1, int(pace*w))
	}
	var b strings.Builder
	for i := range w {
		switch {
		case i == tick && i < fill:
			b.WriteString(paint(cText, "╋"))
		case i == tick:
			b.WriteString(paint(cSub, "┼"))
		case i < fill:
			b.WriteString(paint(c, "━"))
		default:
			b.WriteString(faint("─"))
		}
	}
	return dim(label+" ") + b.String() + " " + paint(c, fmt.Sprintf("%.0f%%", pct)) + resetIn(resets, now, window > 24*time.Hour)
}

// activeUsage is the current account's plan usage, with when each window
// resets, quiet unless it is high. It doesn't say whose: the header does,
// beside it.
func (m *Model) activeUsage() string {
	for _, av := range m.snap.Accounts {
		if !av.Current {
			continue
		}
		u := av.Quota
		if q, ok := m.startQuota(); ok {
			u = q // new sessions run another agent: its account's
		}
		var parts []string
		for _, w := range u.Windows {
			parts = append(parts, usageMeter(w.Label, w.Percent, w.ResetsAt, w.Span, m.snap.At))
		}
		if len(parts) == 0 {
			return ""
		}
		s := strings.Join(parts, "   ")
		if !u.FetchedAt.IsZero() && m.snap.At.Sub(u.FetchedAt) > 3*claude.UsageEvery {
			when := u.FetchedAt.Local().Format("15:04")
			if m.snap.At.Sub(u.FetchedAt) > 20*time.Hour {
				when = u.FetchedAt.Local().Format("Mon 15:04")
			}
			s += faint(" as of " + when)
		}
		return s
	}
	return ""
}

func (m *Model) render() string {
	if m.w == 0 {
		return ""
	}
	if m.bar != nil {
		return m.overlayBar(m.renderScreen())
	}
	if m.confirm != nil && m.confirm.modal {
		return m.confirmModal(m.renderScreen())
	}
	return m.renderScreen()
}

func (m *Model) renderScreen() string {
	switch m.mode {
	case modeHelp:
		return m.overlayBox(m.listView(), m.helpBody(), min(m.w-4, 50))
	case modeProcs:
		if m.snap.Machine.Orphans > 0 {
			return m.frame(m.procBody(), keysFit(m.w-4, "↑↓", "move", "X", "end all orphans", "x", "end / SIGTERM", "!", "SIGKILL tree", "enter", "go to the agent", "[ ]", "Cleanup", "esc", "back"))
		}
		return m.frame(m.procBody(), keysFit(m.w-4, "↑↓", "move", "enter", "go to the agent", "ctrl+x", "SIGTERM", "!", "SIGKILL tree", "[ ]", "Cleanup", "esc", "back"))
	case modeCleanup:
		return m.frame(m.cleanupBody(), keysFit(m.w-4, "↑↓", "move", "x", "remove", "A", "remove all that's safe", "r", "check again", "[ ]", "Processes", "esc", "back"))
	case modeCwd:
		return m.frame(m.cwdBody(), keysFit(m.w-4, "enter", "apply", "tab", "move / add", "↑↓", "pick", "esc", "cancel"))
	case modeEff:
		return m.frame(m.effBody(), m.effHint())
	case modeWork:
		if m.work.page == workProjects {
			return m.frame(m.projectsBody(), m.projectsHint())
		}
		return m.frame(m.workBody(), m.workHint())
	case modeWall:
		return m.frame(m.wallBody(m.w-4, m.wallH()), m.wallHint())
	}
	if m.dialog != nil {
		return m.frame(m.dialogBody(m.w-6), "")
	}
	if m.picker != nil {
		return m.overlayBox(m.listView(), m.pickerBody(min(m.w-10, 96)), min(m.w-6, 100))
	}
	if m.sheet != nil {
		return m.sheetView(m.listView())
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
	b.WriteString(fit(m.pages(), m.w))
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
	if m.confirm != nil && !m.confirm.modal {
		return m.confirmLine(m.w)
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

// confirmLine is the question being asked and its keys, in w cells. The
// keys are what it waits on, so when it's tight the detail goes first,
// then the keys come before the question.
func (m *Model) confirmLine(w int) string {
	c := m.confirm
	keys := "   " + c.keys()
	q := paint(cText+bold, c.question)
	for _, s := range []string{"  " + q + "  " + dim(c.detail) + keys, "  " + q + keys} {
		if cellw.String(s) <= w {
			return fit(s, w)
		}
	}
	return fit(" "+keys[1:]+"   "+q, w)
}

// keys are the keys a confirmation waits on, and what each does.
func (c *confirmation) keys() string {
	keys := paint(cOrange, "y") + dim(" "+cmp.Or(c.yesText, "yes"))
	if c.onBang != nil && c.bangText != "" {
		keys += "   " + paint(cOrange, "!") + dim(" "+c.bangText)
	}
	if c.onNo != nil {
		return keys + "   " + paint(cOrange, "n") + dim(" "+c.noText) + "   " + paint(cOrange, "esc") + dim(" cancel")
	}
	return keys + "   " + paint(cOrange, "n") + dim(" cancel")
}

// confirmModal draws the question being asked in a box over base: the
// question, its detail, then its keys.
func (m *Model) confirmModal(base string) string {
	c := m.confirm
	bw := min(m.w-4, 72)
	body := []string{paint(cText+bold, c.question)}
	if c.detail != "" {
		for _, l := range wrap(c.detail, bw-4) {
			body = append(body, dim(l))
		}
	}
	body = append(body, "", c.keys())
	lines := strings.Split(base, "\n")
	for y := range lines {
		lines[y] = faint(ansi.Strip(fit(lines[y], m.w)))
	}
	box := edgedBox(body, bw, cYellow)
	return strings.Join(pasteAt(lines, box, max(1, (len(lines)-len(box))/2), (m.w-bw)/2), "\n")
}

// keysFit drops the least important pairs (those before the last) until the
// line fits w, so a hint never runs off the screen.
func keysFit(w int, pairs ...string) string {
	for len(pairs) > 2 {
		if s := keys(pairs...); cellw.String(s) <= w {
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
	listW, paneW = m.widths()
	bodyH = max(3, m.h-m.topH()-m.promptH(m.promptW(listW, paneW)))
	return listW, paneW, bodyH
}

// widths is how the screen splits between the list and the pane, without
// the height, which needs the prompt drawn and so the picker, and #view's
// picker asks which layout is on.
func (m *Model) widths() (listW, paneW int) {
	if m.hostedAlone() {
		return 0, m.w // the one session, full width
	}
	if m.hosted != "" {
		// Hosted's list, beside the session when there's room for both.
		if side := m.sideWidth(); m.w-side-1 >= minPane {
			return side, m.w - side - 1
		}
		return m.w, 0
	}
	listW = m.w
	showing := m.full || m.preview || m.autoSplit()
	if m.zenFull() {
		listW, paneW = 0, m.w // zen is the one agent, full width
		showing = false
	}
	if showing {
		// The list keeps at least a quarter of the screen and 30 columns;
		// if that leaves the pane too narrow to read, there's no split.
		side := m.sideWidth()
		switch {
		case m.chatAlone():
			paneW, listW = m.w, 0
		case m.w-side-1 >= minPane:
			listW, paneW = side, m.w-side-1
		case m.preview:
			paneW, listW = m.w, 0
		}
	}
	return listW, paneW
}

// sideWidth is the list's width in a split: your own share when you've set
// one (shift+← →, dragging the edge, /width), else agtop's; never under a
// quarter of the screen or 30 columns. Past those ends, see stepSplit.
func (m *Model) sideWidth() int {
	floor := max((m.w+3)/4, 30)
	side := max(floor, min(m.w*28/100, 64))
	if f := m.store.Config.SideWidth; f > 0 {
		// A width you dragged to wins.
		return max(floor, min(int(float64(m.w)*f+0.5), m.w*3/4))
	}
	// The Session never takes more than its content can use; the spare
	// room goes to Agents.
	return max(side, m.w-1-maxPane)
}

// maxPane is the widest a Session gets: its rows cap at 124 columns, plus
// the pane's margins.
const maxPane = 128

// setSideWidth stores the list's share, kept between a quarter and a half.
func (m *Model) setSideWidth(cols int) {
	if m.w <= 0 {
		return
	}
	cols = min(cols, m.w-1-minPane) // never so wide the split goes away
	f := float64(cols) / float64(m.w)
	f = max(0.25, min(f, 0.75))
	m.store.Config.SideWidth = f
	m.flash(fmt.Sprintf("list width %.0f%% · release to keep", f*100), false)
}

// stepSplit is shift+← and shift+→ (alt too): they move the list's edge,
// and pushed past either end, the screen goes to the Session alone or the
// list alone. It says whether it took the key.
func (m *Model) stepSplit(grow bool) (tea.Cmd, bool) {
	floor := max((m.w+3)/4, 30)
	if m.w-floor-1 < minPane || m.zenFull() || m.hosted != "" {
		return nil, false // too narrow for a split to step through
	}
	ceil := min(m.w*3/4, m.w-1-minPane)
	switch {
	case m.listW == 0:
		if grow {
			return m.splitAgain(), true
		}
	case !m.chatOpen():
		if !grow {
			return m.splitAgain(), true
		}
	case grow && m.sideWidth()+2 > ceil:
		m.listOnly()
	case !grow && m.sideWidth()-2 < floor:
		return m.sessionOnly(), true
	default:
		d := 2
		if !grow {
			d = -2
		}
		m.setSideWidth(m.sideWidth() + d)
	}
	return nil, true
}

// chatOpen is whether a Session is on screen, beside the list or alone.
func (m *Model) chatOpen() bool { return m.full || m.preview || m.autoSplit() }

// chatAlone is whether the Session has the whole screen: pushed there, or
// opened from Agents alone after you last had one that way.
func (m *Model) chatAlone() bool {
	return m.full || m.preview && m.store.Config.ListOnly && m.store.Config.ChatFull
}

// listOnly hides the Session and gives the screen to Agents, and keeps it
// that way, next time too: a Session opens from there as you last had one.
func (m *Model) listOnly() {
	m.store.Config.View, m.store.Config.ListOnly = "list", true
	_ = m.store.SaveConfig()
	m.preview, m.full, m.paneFocus = false, false, false
	m.flash("just Agents, next time too · shift+← or #view split puts the Session beside them", false)
}

// splitAgain puts Agents and the Session side by side, when there's room,
// and keeps them that way, next time too.
func (m *Model) splitAgain() tea.Cmd {
	if !m.canSplit() {
		m.flash("too narrow for Agents and the Session side by side", true)
		return nil
	}
	if !m.chatOpen() {
		m.preview = !m.wide()
	}
	m.store.Config.SetView("split")
	m.full = false
	_ = m.store.SaveConfig()
	m.flash("Agents and the Session side by side, next time too", false)
	return m.loadPreview()
}

// canSplit is whether the screen is wide enough for Agents and the Session
// side by side.
func (m *Model) canSplit() bool { return m.w-max((m.w+3)/4, 30)-1 >= minPane }

// splitHint is the key back to the split, for the hint row, while one side
// has the screen and there's room for both.
func (m *Model) splitHint(back string) []string {
	if m.zen || m.hosted != "" || !m.canSplit() {
		return nil
	}
	return []string{back + " · #view split", "back to side by side"}
}

// viewNow is which of #view's layouts is on screen.
func (m *Model) viewNow() string {
	l, p := m.widths()
	switch {
	case l == 0:
		return "agent"
	case p == 0:
		return "list"
	}
	return "split"
}

// sessionOnly gives the screen to the picked agent's Session, and keeps it
// that way: Sessions open alone from then on, and agtop opens on one.
func (m *Model) sessionOnly() tea.Cmd {
	if m.selected() == nil {
		return nil
	}
	m.preview, m.full = true, true
	m.store.Config.SetView("agent")
	_ = m.store.SaveConfig()
	m.flash("just the Session, next time too · esc for Agents · shift+→ or #view split for both side by side", false)
	return m.loadPreview()
}

// dragSplit follows the list's edge as it's dragged; dropped against
// either side of the screen, that side's pane goes.
func (m *Model) dragSplit(x int) {
	switch {
	case x >= m.w-2:
		if m.chatOpen() {
			m.listOnly()
		}
	case x <= 1:
		if !m.chatAlone() {
			m.sessionOnly()
		}
	default:
		if !m.chatOpen() {
			m.preview = !m.wide()
		}
		m.full = false
		m.store.Config.SetView("split")
		m.setSideWidth(x)
	}
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
		return bodyH + m.promptH(listW)
	}
	return bodyH
}

func (m *Model) listView() string {
	var head []string
	if !m.zen {
		head = append(m.header(), m.pages())
	}
	listW, paneW, bodyH := m.layout()
	over := m.pickerOverCard() // as the Prompt was measured
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
	m.listTop = len(head)
	m.rowKeys = nil
	var left []string
	m.listW = listW
	if listW > 0 {
		// Getting started sits at the foot of the list while there's room.
		var card []string
		if m.showCard() {
			if card = m.startedLines(listW); bodyH-len(card) < 8 {
				card = nil
			}
		}
		m.cardShown = card != nil
		if card != nil && over {
			// The # picker takes Getting started's rows, so the list
			// doesn't jump when it opens.
			if pick := m.fleetSlashLines(listW); pick != nil {
				card = append(make([]string, max(0, len(card)-len(pick))), pick...)
			}
		}
		left = append([]string{m.columnHeader(listW)}, m.listLines(listW, bodyH-1-len(card))...)
		if card != nil {
			for len(left) < bodyH-len(card) {
				left = append(left, "")
			}
			left = append(left[:bodyH-len(card)], card...)
		}
		m.listTop++
	}
	var pane []string
	if paneW > 0 {
		sw := paneW - 3
		if m.peek.on {
			pane = m.zenPeekLines(paneW-3, paneH)
		} else if m.zen && len(m.zenQueue()) == 0 {
			pane = m.zenQuiet(paneW-3, paneH)
		} else if pane = m.agtopPane(sw, paneH); pane == nil {
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
			if f := m.focused(); m.zenFull() && f != nil {
				pane = append([]string{m.zenBar(f, paneW-3)}, pane...)
			}
		}
	}
	var b strings.Builder
	b.Grow(m.frameLen + m.frameLen/8)
	m.paneTop = len(head)
	fadeTurn()
	for _, l := range head {
		fitTo(&b, l, m.w, "")
		b.WriteByte('\n')
	}
	// Rows beside each other: the side without the keys fades back.
	listFade, paneFade := "", ""
	if m.twoSided() {
		if m.sessionFocused() {
			listFade = fade
		} else {
			paneFade = fade
		}
	}
	div := m.divider()
	split2 := func(l, p string) {
		if listFade != "" {
			l = fadeRow(l)
		}
		b.WriteString(listFade)
		fitTo(&b, l, listW, listFade)
		if listFade != "" {
			b.WriteString(reset)
		}
		b.WriteString(div)
		b.WriteString("  ")
		m.paneRow(&b, p, paneW-3, paneFade)
	}
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
			split2(l, p)
		case listW > 0:
			fitTo(&b, l, m.w, "")
		default:
			b.WriteString("  ")
			fitTo(&b, p, m.w-2, "")
		}
		b.WriteByte('\n')
	}
	for _, l := range dock {
		fitTo(&b, l, m.w, "")
		b.WriteByte('\n')
	}
	m.promptBoxY = len(head) + bodyH + len(dock) + m.promptBoxIdx
	for i, l := range prompt {
		if split {
			p := ""
			if j := bodyH + i; j < len(pane) {
				p = pane[j]
			}
			split2(l, p)
		} else {
			fitTo(&b, l, m.w, "")
		}
		if i < len(prompt)-1 {
			b.WriteByte('\n')
		}
	}
	m.frameLen = b.Len()
	if len(prompt) == 0 {
		// No Prompt under the body (zen): a last newline would scroll the
		// screen and lose the top row.
		return strings.TrimSuffix(b.String(), "\n")
	}
	return b.String()
}

// Focus: when a session can take the keys, the side without them fades
// back so where you're typing is obvious at a glance; the side with them
// keeps full brightness, an orange marker and an orange box edge.
//
// agtop fades it itself, every colour fadeBy of the way to the background,
// not with the terminal's faint, which some (Terminal.app) take so far on
// a theme's own greys that the quiet parts are lost.
const fadeBy = 0.4

var (
	fade  string            // the text colour on the faded side, after every reset
	faded map[string]string // an escape code as it is on the faded side
)

// fadedRows are the rows fadeRow faded this frame and the last, by row:
// most rows are the same frame to frame. fadedFor is the fade they were
// made with.
var fadedRows, fadedRowsOld map[string]string
var fadedFor string

// fadeTurn starts a frame's fading: what the frame before faded stays at
// hand, the rest goes.
func fadeTurn() {
	if fadedFor != fade {
		clear(fadedRows)
		clear(fadedRowsOld)
		fadedFor = fade
	}
	fadedRowsOld, fadedRows = fadedRows, fadedRowsOld
	if fadedRows == nil {
		fadedRows = map[string]string{}
	}
	clear(fadedRows)
}

// fadeRow is s with its text colours faded back.
func fadeRow(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	if f, ok := fadedRows[s]; ok {
		return f
	}
	f, ok := fadedRowsOld[s]
	if !ok {
		f = fadeRowNow(s)
	}
	if fadedRows != nil {
		fadedRows[s] = f
	}
	return f
}

func fadeRowNow(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := i + 2
		for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';' || s[j] == ':') {
			j++
		}
		if j == len(s) || s[j] != 'm' {
			b.WriteString(s[:j])
			s = s[j:]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(fadeCode(s[i : j+1]))
		s = s[j+1:]
	}
}

// fadeCode is one style code on the faded side: its text colour faded,
// and the terminal's own text (a reset, or 39) in the faded text colour.
func fadeCode(code string) string {
	if f, ok := faded[code]; ok {
		return f
	}
	ps := strings.Split(code[2:len(code)-1], ";")
	var out []string
	reset := false
	for i := 0; i < len(ps); i++ {
		switch n := ps[i]; {
		case n == "" || n == "0":
			out, reset = append(out, "0"), true
		case n == "39":
			reset = true
		case n == "2":
			// faint is the terminal's; the fade is already here
		case (n == "38" || n == "48") && i+4 < len(ps) && ps[i+1] == "2":
			if n == "38" {
				c := theme.Mix(theme.RGB{R: atoi8(ps[i+2]), G: atoi8(ps[i+3]), B: atoi8(ps[i+4])}, painted.BG, fadeBy)
				out = append(out, fmt.Sprintf("38;2;%d;%d;%d", c.R, c.G, c.B))
			} else {
				out = append(out, ps[i:i+5]...)
			}
			i += 4
		default:
			out = append(out, n)
		}
	}
	f := "\x1b[" + strings.Join(out, ";") + "m"
	if len(out) == 0 {
		f = ""
	}
	if reset {
		f += fade
	}
	faded[code] = f
	return f
}

func atoi8(s string) uint8 {
	n, _ := strconv.Atoi(s)
	return uint8(min(255, max(0, n)))
}

func (m *Model) twoSided() bool {
	return m.listW > 0 && (m.host != nil || (m.live != nil && m.focused() != nil && m.live.key == m.focused().Key))
}

// paneRow writes a Session row w wide, faded when bg (fade) is set.
func (m *Model) paneRow(b *strings.Builder, p string, w int, bg string) {
	if bg != "" {
		p = fadeRow(p)
		b.WriteString(bg)
	}
	fitTo(b, p, w, bg)
	if bg != "" {
		b.WriteString(reset)
	}
}

// sessionFocused is whether the keys go to the Session: an agtop
// conversation, or typing into a Claude Code screen.
func (m *Model) sessionFocused() bool { return m.paneFocus || m.embedded }

// divider leans orange toward the side with focus.
// divider is the edge between Agents and the Session. It stays a quiet
// line (the focused side shows focus itself), a shade brighter when the
// mouse is on it; the pointer turns into a resize arrow there too.
func (m *Model) divider() string {
	if m.dragging || m.divHover {
		return paint(cSub, "│")
	}
	return faint("│")
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
	left := "  " + tab("screen", screenOn, live) + " " + tab("summary", !screenOn, true)
	right := ""
	switch {
	case m.embedded:
		right = paint(cOrange+bold, "typing into it") + dim(" · ctrl+] comes back")
	case screenOn:
		right = dim("typing goes into it · ctrl+] leaves · ctrl+f full screen")
	case !live && a != nil && a.Interactive:
		right = dim(a.Where())
	case !live:
		right = dim("not running · enter resumes it")
	}
	left = withTabHint(left, "[ ]", "views", "", w-cellw.String(right+" ")-2)
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
	two := stacked(w, nameCol)
	var all, keys []string
	var cont []bool // a stacked row's second line, which the list never starts on
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
		cont = append(cont, false)
	}
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			key := sectionKey(l.title)
			emit(m.sectionLine(l, w), key, key == m.sel)
		case lineProject:
			emit(m.projectLine(l, w), "", false)
		case lineTree:
			emit(m.treeLine(l, w), "", false)
		case lineBlank:
			emit("", "", false)
		case lineAgent:
			sel := l.agent.Key == m.sel
			emit(m.agentLine(l.agent, w, sel, nameCol, two), l.agent.Key, sel)
			if two {
				emit(m.agentSub(l.agent, w), l.agent.Key, sel)
				cont[len(cont)-1] = true
			}
		}
	}
	if len(m.order) == 0 {
		all = append(all, "", dim("  No agents yet. Describe a task below to start one."))
		keys = append(keys, "", "")
		cont = append(cont, false, false)
	}
	// Scrolling up shows the whole row above; down and to the end, a row
	// cut in half at the top gives up its second line instead.
	if selTop >= 0 {
		if selTop-1 < m.scroll {
			m.scroll = max(0, selTop-1)
			if cont[m.scroll] {
				m.scroll--
			}
		}
		if selBottom+1 >= m.scroll+h {
			m.scroll = selBottom + 2 - h
		}
	}
	if m.scroll > len(all)-h {
		m.scroll = max(0, len(all)-h)
	}
	if m.scroll < len(cont) && cont[m.scroll] {
		m.scroll++
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
const tabOn = "\x1b[1;38;2;24;22;20;48;2;217;119;87m"

var tabOff, hoverBG string

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
		room := w - cellw.String(head) - 6
		if room >= 20 && l.peek != "" { // less is a word or two cut off
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
	tw := cellw.String(title)
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
	text = mdPlain.Replace(oneLine(text))
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
		win := agent.ContextWindow(agent.Kind(a.Kind), p.Model)
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
		return dim(s)
	}
	name := label("AGENTS", "name")
	left := "   " + fit(name, nameCol+2)
	if m.twoSided() && !m.sessionFocused() {
		left = paint(cOrange, "▍") + "  " + fit(name, nameCol+2)
	}
	if sortBy == "name" {
		left = "   " + paint(cSub+bold, fit(name, nameCol+2))
	} else {
		left = dim(left)
	}
	if !stacked(w, nameCol) {
		left += dim("LATEST")
	}
	if sortBy == "recent" {
		left += paint(cSub+bold, " · by recent activity")
	}
	rightW := wAct + wCPU + wRAM + wCost + wAge + 3
	left = m.listToggles(left, w-rightW)
	cols := dim(right1("RUNNING", wAct)) + col("CPU", "cpu", wCPU) + col("RAM", "ram", wRAM) + col("COST", "cost", wCost) + col("TIME", "time", wAge+2) + " "
	gap := w - cellw.String(left) - rightW
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + cols
}

// listToggles adds to the column header how the list is arranged, each
// part a click away: the grouping (ctrl+s goes to the next), and whether
// sections are split by project (ctrl+p). Only where there's room.
func (m *Model) listToggles(left string, room int) string {
	m.headHits = m.headHits[:0]
	if m.activeSidebar() != nil {
		return left
	}
	group := dim("by ") + paint(cSub, m.groupLabel(m.store.Config.GroupBy))
	split := faint("□ projects")
	if m.splitProjects() {
		split = paint(cOrange, "▣ ") + paint(cSub, "projects")
	}
	at := cellw.String(left) + 3
	gw, sw := cellw.String(group), cellw.String(split)
	if at+gw+3+sw+1 > room {
		if at+sw+1 > room {
			return left
		}
		m.headHits = append(m.headHits, headHit{at, at + sw, "split"})
		return left + "   " + split
	}
	m.headHits = append(m.headHits, headHit{at, at + gw, "group"}, headHit{at + gw + 3, at + gw + 3 + sw, "split"})
	return left + "   " + group + faint(" · ") + split
}

// headHit is a stretch of the column header a click toggles.
type headHit struct {
	from, to int
	what     string
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
		n := cellw.String(oneLine(l.agent.DisplayName))
		if b := cellw.String(ansi.Strip(m.badges(l.agent))); b > 0 {
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

// stacked is when the list is too narrow for a readable summary beside each
// name: rows take two lines then, the summary hung under the name.
func stacked(w, nameCol int) bool {
	act, cpu, ram, cost := colWidths(w)
	return w-3-(act+cpu+ram+cost+wAge+3)-nameCol-2 < 20
}

// agentSub is a stacked row's second line: its summary, or where it works
// when it has nothing to say, hung from the name above so the two read as one.
func (m *Model) agentSub(a *fleet.Agent, w int) string {
	summary, col, justDone := m.rowSummary(a)
	room := w - 6
	text := paint(col, fit(summary, room))
	switch {
	case justDone:
		text = paint(cGreen, "just finished") + faint(" · ") + paint(col, fit(summary, room-16))
	case summary == "":
		text = faint(fit(m.context(a), room))
	}
	return "   " + faint("╰ ") + text
}

// agentLine is the first line of a row: marker, name, badges, figures, and
// the summary too unless the row is stacked.
func (m *Model) agentLine(a *fleet.Agent, w int, sel bool, nameCol int, stacked bool) string {
	wAct, wCPU, wRAM, wCost := colWidths(w)
	now := m.snap.At
	live := a.Live()
	marker := " "

	switch {
	case a.Checking:
		marker = paint(cSub, "◔")
	case a.Halted():
		marker = paint(cRed, "✗")
	case a.YourTurn(now):
		marker = paint(cGreen, "◆")
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
		marker = dim("◦")
	default:
		marker = faint("·") // stopped: every row has a marker, so none reads as missing
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
			return dim(v)
		default:
			return faint(v)
		}
	}
	var right string
	switch {
	case live || busy:
		right = act + cpuCell() + ramCell(true) + costCell(a.Spend.Cost, wCost)
	case resident:
		right = act + faint(right1(fmt.Sprintf("%.0f%%", a.CPU), wCPU)) + ramCell(false) + faint(right1(moneyShort(a.Spend.Cost), wCost))
	default:
		cost := moneyShort(a.Spend.Cost)
		if cost == "–" {
			cost = ""
		}
		right = act + blanks(wCPU+wRAM) + faint(right1(cost, wCost))
	}
	if live {
		right += dim(right1(dur(a.Elapsed(now)), wAge+2)) + " "
	} else {
		right += faint(right1(age(a.Age(now)), wAge+2)) + " "
	}

	// Bold is for what wants you: the selection and an agent that needs you.
	// Working rows are bright, not bold, so six of them don't drown one.
	nameColor := cSub
	switch {
	case sel || a.NeedsYou() || a.Waiting() || a.Halted() || a.YourTurn(now):
		nameColor = cText + bold
	case live || busy:
		nameColor = cText
	case a.Pinned:
		nameColor = cText
	default:
		nameColor = cDim // idle, stopped and done step back behind the working
	}
	name := oneLine(a.DisplayName)
	badges := m.badges(a)
	if ps := m.pluginStatus(a.Key); ps != "" {
		badges = strings.TrimSpace(badges + " " + ps)
	}
	summary, sumColor, justDone := m.rowSummary(a)
	room := w - 3 - cellw.String(right)
	left := paint(nameColor, name)
	if badges != "" {
		left += " " + badges
	}
	if summary != "" && !stacked {
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

// rowSummary is the latest thing an agent said or is doing, the colour to
// say it in, and whether it has only just finished.
func (m *Model) rowSummary(a *fleet.Agent) (summary, sumColor string, justDone bool) {
	now := m.snap.At
	live, busy := a.Live(), a.Busy()
	sumColor = cDim
	switch {
	case a.Checking:
		summary, sumColor = "turn ended · checking…", cDim
	case a.Halted():
		summary, sumColor = "stopped · "+oneLine(a.HaltReason()), cRed
	case a.YourTurn(now) && !a.JustFinished(now):
		summary, sumColor = oneLine(a.Detail), cSub
		if summary == "" {
			summary = "finished its turn"
		}
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
		summary, sumColor = oneLine(a.Detail), cDim
		if p := m.previews[a.Key].p; summary == "" && p.Doing != "" {
			summary = oneLine(p.Doing)
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
		sumColor = cFaint // resting: its last words are there when you look
	}
	if summary == "stopped" {
		summary = ""
	}
	summary = mdPlain.Replace(summary)
	return
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
	for _, k := range []string{"shell", "agent", "monitor", "task"} {
		if n := kinds[k]; n > 0 {
			name := map[string]string{"shell": "shell", "agent": "subagent", "monitor": "monitor", "task": "task"}[k]
			if n > 1 {
				name += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, name))
		}
	}
	if n := a.Subs.Direct + a.Subs.Nested; len(parts) == 0 && n > 0 {
		name := "subagent"
		if n > 1 {
			name += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, name))
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

// costCell is a working agent's spend: quiet, yellow from $100 the way cpu
// is from 50%.
func costCell(cost float64, w int) string {
	v := right1(moneyShort(cost), w)
	if cost >= 100 {
		return paint(cYellow, v)
	}
	return dim(v)
}

func (m *Model) badges(a *fleet.Agent) string {
	var parts []string
	if b := rowBadge(a); b != "" {
		// Which agent, when it isn't the built-in one.
		parts = append(parts, b)
	}
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

// noPrompt is when there's no Prompt box: zen answers in the Session's own
// box, and a Session filling a narrow screen has its own box too, so there
// are never two boxes on screen at once.
func (m *Model) noPrompt() bool {
	if m.zenFull() || m.hostedAlone() {
		return true
	}
	l, _ := m.widths()
	return m.host != nil && l == 0 && (m.preview || m.full) && m.mode == modeList
}

// promptBoxAt is the Prompt's box at width w, before its labels.
func (m *Model) promptBoxAt(w int) box {
	b := box{w: w, focused: !m.sessionFocused(), text: m.input, cursor: m.cursorPos(), anchor: m.anchor - 1,
		lead: paint(cOrange, "❯ "), maxRows: min(6, max(1, m.h-m.topH()-4-5)), top: m.promptTop}
	if m.sessionFocused() {
		b.text = nil
	}
	return b
}

// promptH is len(m.promptLines(w)), without drawing them.
func (m *Model) promptH(w int) int {
	if m.noPrompt() {
		return 0
	}
	n := 2 + m.promptBoxAt(w).rows() + 1
	if len(m.images) > 0 && w > 0 {
		n++ // the chips
	}
	if cmds, _ := m.promptPicker(); len(cmds) > 0 && !m.pickerOverCard() {
		n += min(len(cmds), 6) + 1 // the picker
	}
	return n
}

// pickerOverCard is whether the Prompt's picker is drawn over Getting
// started, the rows at the foot of the list, rather than above the box.
func (m *Model) pickerOverCard() bool {
	return m.cardShown && m.listW > 0 && m.showCard()
}

// promptLines are the input box and a row of key hints. The box's top
// edge says where the text goes and what enter will do; the agent pane's
// own input is the same box, so the two always read alike.
func (m *Model) promptLines(w int) []string {
	if m.noPrompt() {
		return nil
	}
	a := m.selected()
	text := string(m.input)
	b := m.promptBoxAt(w).scrolled()
	m.promptTop = b.top
	// An empty box with nothing asked of it shows no cursor: the list has
	// the keys until you type, rename, or reply.
	b.idle = len(m.input) == 0 && m.inKind == inPrompt
	if m.inKind == inPrompt {
		b.footR = m.boxNote("")
	}
	switch {
	case m.inKind == inRename && a != nil:
		b.topL = dim("rename ") + paint(cText, oneLine(a.DisplayName)) + dim(" · enter saves")
		b.holder = "a new name · empty resets it"
	case m.inKind == inGroup && a != nil:
		b.topL = dim("group for ") + paint(cText, oneLine(a.DisplayName)) + dim(" · enter saves")
		b.holder = "a group name · empty clears it"
	case m.inKind == inReply && a != nil && !a.Agtop:
		b.topL = dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 32, "…")) + dim(" · enter sends")
		b.holder = draftsHolder("a message for this agent", " · esc leaves reply mode")
	case typingHash(text):
		b.topL = dim("agtop command · enter runs it")
	default:
		dirs := m.startDirs()
		dir := pickDir(dirs, m.dirIdx)
		b.topL = dim("new session in ") + m.dirLabel(dir) + dim(" · enter starts it")
		b.holder = draftsHolder("describe a task for a new session", "")
		// What it starts as, so a model or profile is never a surprise;
		// the folder key goes first when there's no room for both.
		b.topR = m.startWith(dir, false)
		if folder := paint(cSub, "ctrl+l") + dim(" folder"); len(dirs) > 1 && cellw.String(b.topL+b.topR+folder)+12 <= w {
			b.topR = folder + faint(" · ") + b.topR
		}
	}
	if m.sessionFocused() {
		// The Session has the keys; this box waits, and says how back.
		b.holder = "esc or ← to come back here"
		if m.embedded {
			b.holder = "ctrl+] or click here to come back"
		}
	}
	var out []string
	if !m.pickerOverCard() {
		out = m.fleetSlashLines(w)
	}
	if l := chips(m.images, w, -1, false); l != "" {
		out = append(out, l)
	}
	m.promptBox, m.promptBoxIdx = b, len(out)
	out = append(out, b.lines()...)
	// One row of keys for what you can do right now; ? has the rest.
	var hint string
	switch {
	case m.inKind == inReply:
		hint = keysFit(w-4, "enter", "send", "↑↓", "another agent", "esc", "done", keySaveDraft, "keep as draft", "?", "guide")
	case m.inKind != inPrompt:
		hint = keysFit(w-4, "enter", "save", "esc", "cancel")
		if m.inKind == inRename {
			hint = keysFit(w-4, "enter", "save", "tab · ↑↓", "save, rename the next", "esc", "cancel")
		}
	case typingHash(text):
		hint = keysFit(w-4, "enter", "run it", "esc", "clear", "?", "guide")
	case len(m.input) > 0:
		hint = keysFit(w-4, "enter", "start it", keySaveDraft, "keep as draft", "ctrl+l", "folder", "esc", "clear", "?", "guide")
	case m.peeking():
		back := "esc"
		if from := m.agentByKey(m.peekFrom); from != nil {
			back = "back to " + oneLine(from.DisplayName)
		}
		hint = keysFit(w-4, "↑↓", "pick", "enter · tab", "open it", "esc", back, "shift+← · #view split", "side by side", "?", "guide")
	default:
		var pairs []string
		switch {
		case a != nil && a.Agtop && m.store.Config.EnterOn == "open":
			pairs = []string{"enter · tab", "talk to it", "ctrl+r", "rename", "ctrl+k", "go anywhere", "ctrl+n", "next needing you"}
		case a != nil && m.store.Config.EnterOn == "open":
			pairs = []string{"enter · tab", "open", "ctrl+r", "rename", "ctrl+k", "go anywhere", "ctrl+o", "reply", "ctrl+n", "next needing you"}
		case a != nil && a.Agtop:
			pairs = []string{"enter", "rename", "⌘↓ · tab", "talk to it", "ctrl+k", "go anywhere", "ctrl+n", "next needing you", "tab", "its Session"}
		default:
			pairs = []string{"enter", "rename", "⌘↓ · tab", "open", "ctrl+k", "go anywhere", "ctrl+o", "reply", "ctrl+n", "next needing you", "tab", "its Session"}
		}
		if a != nil && (a.Halted() || a.YourTurn(m.snap.At)) {
			pairs = append([]string{"alt+g", a.ContinueText()}, pairs...)
		}
		if m.newer.Version != "" && !m.updating {
			pairs = append([]string{"#update", "new agtop"}, pairs...)
		}
		if m.hosted != "" {
			// Hosted's list: no command bar, and the way back to the session.
			var kept []string
			for i := 0; i+1 < len(pairs); i += 2 {
				if pairs[i] != "ctrl+k" {
					kept = append(kept, pairs[i], pairs[i+1])
				}
			}
			pairs = append([]string{"esc · ctrl+6", "hide Agents"}, kept...)
		}
		// With one side on screen, how to have both is kept in view.
		l, p := m.widths()
		back := "shift+←"
		if l == 0 {
			back = "shift+→"
		}
		if v := m.splitHint(back); v != nil && (l == 0 || p == 0) {
			pairs = append(pairs[:2:2], append(v, pairs[2:]...)...)
		}
		hint = keysFit(w-4, append(pairs, "?", "guide")...)
	}
	if (m.status != "" && m.snap.At.Sub(m.statusAt).Seconds() < 6) || m.confirm != nil && !m.confirm.modal {
		hint = strings.TrimRight(m.statusOr(""), " ") // it pads to the screen, not this box
		if m.confirm != nil && !m.confirm.modal {
			hint = m.confirmLine(w)
		}
	} else {
		hint = "  " + hint
	}
	return append(out, fit(hint, w))
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
	model := modelWord(a.Kind, firstNonEmpty(p.Model, a.Spend.Model))
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
		win := agent.ContextWindow(agent.Kind(a.Kind), p.Model)
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
				text := mdPlainNoUnder.Replace(oneLine(e.Text))
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
	cur := m.procIndex(rows)
	// Past this the eye can't follow a row from its name to its numbers.
	w := min(m.w-4, 170)
	mc := m.snap.Machine
	// Every row ends in the same three columns; the rest is the left.
	const cpuW, memW, procW = 9, 8, 8
	left := max(20, w-2-cpuW-memW-procW)
	cols := func(r procRow, procs bool) string {
		p := ""
		if procs && r.n > 1 {
			p = fmt.Sprintf("%d", r.n)
		}
		return cpuColor(r.cpu, right(fmt.Sprintf("%.1f%%", r.cpu), cpuW)) + memColor(r.mem, right(mem(r.mem), memW)) + dim(right(p, procW))
	}
	var procs int
	for _, r := range rows {
		if r.heading || r.other {
			procs += r.n
		}
	}
	out := []string{
		paint(cText+bold, "Processes") + dim(fmt.Sprintf("  ·  %s ram · %.0f%% cpu · %d processes", mem(mc.TotalMem), mc.TotalCPU, procs)),
		"",
		"  " + faint(fit("agent", left-2)+right("cpu", cpuW)+right("mem", memW)+right("procs", procW)),
	}
	nameW := min(48, left*3/5)
	lastRole, other, wasBusy := fleet.Role(-1), false, false
	for i, r := range rows {
		var line string
		switch {
		case r.heading:
			// A busy agent stands apart with what it runs; the idle ones
			// sit together as a table.
			if r.busy || wasBusy {
				out = append(out, "")
			}
			wasBusy = r.busy
			name := paint(cText, fit(r.label, nameW))
			if r.busy {
				name = paint(cText+bold, fit(r.label, nameW))
			}
			line = "  " + name + "  " + faint(fit(r.cmd, left-nameW-4)) + cols(r, true)
		case r.other && r.role == fleet.RoleOrphan:
			if lastRole != fleet.RoleOrphan {
				lastRole = fleet.RoleOrphan
				head := paint(cYellow+bold, fmt.Sprintf("%d orphaned", mc.Orphans)) + paint(cYellow, fmt.Sprintf(" · holding %s", mem(mc.OrphanMem)))
				out = append(out, "", head+" "+faint(strings.Repeat("─", max(0, w-2-cellw.String(head)-1))),
					dim("  The sessions that started these have ended; they'll run until you end them. ")+paint(cOrange, "x")+dim(" ends one, ")+paint(cOrange, "X")+dim(" ends them all."))
			}
			lbl := fit(r.label+" · "+age(m.snap.At.Sub(r.start))+" old", 30)
			line = "  " + paint(cYellow, lbl) + paint(cText, fit(trimCmd(r.cmd, left-32), left-32)) + cols(r, true)
		case r.other:
			if !other {
				other = true
				out = append(out, "", "", rule("Not agents", "", w-2))
			}
			if r.role != lastRole {
				lastRole = r.role
				out = append(out, dim("  "+roleName(r.role)))
			}
			line = "    " + paint(cSub, fit(r.label, 30)) + faint(fit(trimCmd(r.cmd, left-34), left-34)) + cols(r, true)
		case r.role == fleet.RoleOrphan:
			line = "  " + strings.Repeat(" ", 30) + faint("└ "+fit(trimCmd(r.cmd, left-34), left-34)) + cols(r, false)
		default:
			tree := strings.Repeat("  ", min(r.depth, 8))
			pid := faint(right(fmt.Sprintf("%d", r.pid), 7))
			c := paint(cSub, r.cmd)
			if r.cpu >= 1 || r.mem >= 256<<20 {
				c = paint(cText, r.cmd)
			}
			line = "  " + pid + "  " + faint(tree) + fit(c, left-11-len(tree)) + cols(r, false)
		}
		if i == cur {
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
		return "Orphaned — the session that started these has ended"
	default:
		return "Other Claude Code"
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

// helpPages are the guide's tabs: a key and what it does.
var helpPages = []struct {
	name string
	rows [][2]string
}{
	{"✦ Start", [][2]string{
		{"enter", "start an agent"},
		{"ctrl+l", "pick its folder"},
		{"#", "agtop commands"},
		{"/", "Claude commands"},
		{"⌘z · ctrl+/", "undo in a box, a cleared one too"},
		{"alt+s", "keep what's typed as a draft, the box cleared"},
		{"alt+p", "bring back the latest draft · again for older"},
		{"#drafts", "drafts, sent and cleared · ctrl+r in a Session"},
	}},
	{"▤ Agents", [][2]string{
		{"↑↓", "pick one"},
		{"⌘↓ · tab", "open its Session"},
		{"enter", "rename or open it, as you chose"},
		{"ctrl+r", "rename it · tab the next"},
		{"ctrl+n", "next one needing you"},
		{"#done", "put it away"},
		{"ctrl+x", "stop it"},
		{"ctrl+p", "split the list by project"},
	}},
	{"◈ Around", [][2]string{
		{"ctrl+z", "zen"},
		{"alt+w", "the default profile or provider"},
		{"< > · ctrl+\\", "Agents · Efficiency · Machine · Settings"},
		{"[ ]", "a Session's views, with nothing typed"},
		{"[ ]", "a place's pages, or a sheet's tabs"},
		{"tab", "in Agents, between the list and the Session"},
		{"shift+← →", "resize · past the end, one side alone"},
		{"ctrl+6", "hide or show Agents beside a Session"},
		{"#tips", "Getting started again"},
		{"esc esc", "quit"},
	}},
}

// helpBody is the guide: three tabs of keys.
func (m *Model) helpBody() []string {
	var tabs []string
	for i, p := range helpPages {
		if i == m.helpPage {
			tabs = append(tabs, tabOn+" "+p.name+" "+reset)
		} else {
			tabs = append(tabs, tabOff+" "+p.name+" "+reset)
		}
	}
	out := []string{strings.Join(tabs, " "), ""}
	page := helpPages[m.helpPage]
	for _, r := range keyRows(page.rows) {
		out = append(out, r, "")
	}
	// Every tab as tall as the tallest, so the box stays put.
	most := 0
	for _, p := range helpPages {
		most = max(most, len(p.rows))
	}
	for range most - len(page.rows) {
		out = append(out, "", "")
	}
	return append(out, faint("[ ] next · any key closes"))
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
