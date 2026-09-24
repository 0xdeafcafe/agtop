package ui

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/statusline"
)

// --- /statusline ---

// statusSheet builds status lines: pick what shows and in what order, see
// it drawn with this session's numbers, save. Three of them, one per tab:
//
//   - Claude Code's, under its prompt. Saving sets the account's
//     settings.json statusLine to `agtop statusline`, which draws the
//     layout kept in agtop's config.
//   - the agent header, at the top of an agent's Session in agtop, and
//   - the top bar, at the top right of agtop's window: both agtop's own,
//     drawn from what agtop knows (bars.go), and shown live as you build
//     them.
type statusSheet struct {
	acct   claude.Account
	a      *fleet.Agent
	c      *hostConn
	tab    int
	claude statusline.Layout
	bars   statusline.Bars
	was    string // the Claude Code layout as opened, to tell if it changed
	cur    int
	in     statusline.Input
	// current is settings.json's statusLine command now.
	current string
	err     string
	// rnd draws Claude Code's line many times a second without running
	// git, or your own command, each time.
	rnd statusline.Renderer
}

const (
	stAgent = iota
	stTop
	stClaude
	statusTabs
)

var statusTabNames = []string{"Agent header", "Top bar", "Claude Code"}

func (m *Model) openStatusLine(c *hostConn, a *fleet.Agent) {
	bars := m.bars
	if bars.Top.Lines == nil {
		bars.Top = statusline.DefaultTop()
	}
	if bars.Agent.Lines == nil {
		bars.Agent = statusline.DefaultAgent()
	}
	st := &statusSheet{acct: a.Acct, a: a, c: c, claude: statusline.Load().Clone(), in: previewInput(c, a),
		bars: statusline.Bars{Top: bars.Top.Clone(), Agent: bars.Agent.Clone()}}
	if s, err := claude.LoadSettings(a.Acct); err == nil {
		st.current = s.String("statusLine.command")
	}
	// Taken before your own line is folded in, so saving adopts it.
	b, _ := json.Marshal(trimmed(st.claude))
	st.was = string(b)
	// A status line of your own isn't lost: it becomes a segment, kept
	// where it was, on the first line.
	if st.current != "" && !statusline.Ours(st.current) && st.claude.Custom != st.current {
		st.claude.Custom = st.current
		if !st.claude.Shown("custom") {
			st.claude.Lines = append([][]string{{"custom"}}, st.claude.Lines...)
		}
	}
	for t := range statusTabs {
		st.tab = t
		st.pad()
	}
	st.tab = stAgent
	m.sheet = st
}

// openTopBar is #statusline from the list: the same sheet, on the top
// bar, with the selected agent (if any) for the other tabs' previews.
func (m *Model) openTopBar(a *fleet.Agent) {
	c := m.host
	if a == nil {
		a = &fleet.Agent{Acct: m.store.Config.ActiveAccount(), Cwd: m.launchDir}
	}
	if c == nil || c.key != a.Key {
		c = &hostConn{key: a.Key, sess: convo.New(), open: map[string]bool{}}
	}
	m.openStatusLine(c, a)
	if st, ok := m.sheet.(*statusSheet); ok {
		st.tab = stTop
	}
}

// width asks for room to show the agent header as wide as the pane it
// sits on.
func (st *statusSheet) width(m *Model) int {
	if st.tab != stAgent {
		return 0
	}
	_, paneW, _ := m.layout()
	return max(112, paneW+10) // the panel's border and padding, and the well's
}

// lay is the layout of the tab showing.
func (st *statusSheet) lay() *statusline.Layout {
	switch st.tab {
	case stTop:
		return &st.bars.Top
	case stAgent:
		return &st.bars.Agent
	}
	return &st.claude
}

func (st *statusSheet) maxLines() int {
	if st.tab == stClaude {
		return statusline.MaxLines
	}
	return statusline.BarLines
}

// pad gives the layout every line it can have, empty ones included, so
// there's somewhere to move a segment to.
func (st *statusSheet) pad() {
	l := st.lay()
	for len(l.Lines) < st.maxLines() {
		l.Lines = append(l.Lines, nil)
	}
	l.Lines = l.Lines[:st.maxLines()]
}

// segInfo is a segment as the builder lists it.
type segInfo struct{ id, name, about string }

func (st *statusSheet) segs() []segInfo {
	var out []segInfo
	if st.tab == stClaude {
		for _, s := range statusline.Segments {
			if s.ID == "custom" && st.claude.Custom == "" {
				continue
			}
			out = append(out, segInfo{s.ID, s.Name, s.About})
		}
		return out
	}
	for _, s := range barSegs(st.which()) {
		out = append(out, segInfo{s.id, s.name, s.about})
	}
	return out
}

func (st *statusSheet) which() int {
	if st.tab == stTop {
		return barTop
	}
	return barAgent
}

func (st *statusSheet) ctx(m *Model) *barCtx {
	return &barCtx{m: m, t: m.tally(), a: st.a, c: st.c}
}

// previewInput is this session as Claude Code would describe it to the
// status line, with made-up numbers where there aren't any yet.
func previewInput(c *hostConn, a *fleet.Agent) statusline.Input {
	var in statusline.Input
	in.SessionID = firstNonEmpty(c.sess.Info.SessionID, a.SessionID, "3f2a9c1e-0000")
	in.Cwd = firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	in.Workspace.CurrentDir = in.Cwd
	in.Model.DisplayName = firstNonEmpty(c.sess.Model, c.sess.Info.Model, "Opus")
	in.Effort.Level = firstNonEmpty(c.sess.Info.Effort, "high")
	in.Version = "2.1"
	in.Cost.USD = c.sess.Info.CostUSD
	if in.Cost.USD == 0 {
		in.Cost.USD = 1.27
	}
	in.Cost.DurationMS = 23 * 60 * 1000
	if len(c.sess.Turns) > 0 && !c.sess.Turns[0].Start.IsZero() {
		in.Cost.DurationMS = float64(time.Since(c.sess.Turns[0].Start).Milliseconds())
	}
	in.Cost.LinesAdded, in.Cost.LinesRemoved = 48, 12
	in.Context.Size = 200_000
	if strings.Contains(strings.ToLower(in.Model.DisplayName+c.sess.Info.Model), "1m") {
		in.Context.Size = 1_000_000
	}
	in.Context.Input = c.sess.Context
	if in.Context.Input == 0 {
		in.Context.Input = in.Context.Size * 34 / 100
	}
	return in
}

// slot is a row of the builder: a segment on a line, or not shown
// (line -1).
type slot struct {
	line int
	id   string
}

// slots are the segments line by line, then the ones not shown.
func (st *statusSheet) slots() []slot {
	var out []slot
	l := st.lay()
	for i, ln := range l.Lines {
		for _, id := range ln {
			out = append(out, slot{i, id})
		}
	}
	for _, s := range st.segs() {
		if !l.Shown(s.id) {
			out = append(out, slot{-1, s.id})
		}
	}
	return out
}

// place puts id at pos on line (removing it from wherever it was) and
// moves the cursor with it.
func (st *statusSheet) place(id string, line, pos int) {
	l := st.lay()
	for i, ln := range l.Lines {
		l.Lines[i] = slices.DeleteFunc(ln, func(s string) bool { return s == id })
	}
	if line >= 0 {
		ln := l.Lines[line]
		l.Lines[line] = slices.Insert(ln, max(0, min(pos, len(ln))), id)
	}
	for i, sl := range st.slots() {
		if sl.id == id {
			st.cur = i
		}
	}
}

func (st *statusSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c", "q":
		m.sheet = nil
		return nil
	case "tab":
		st.tab, st.cur, st.err = (st.tab+1)%statusTabs, 0, ""
		return nil
	case "shift+tab":
		st.tab, st.cur, st.err = (st.tab+statusTabs-1)%statusTabs, 0, ""
		return nil
	case "enter", "ctrl+s":
		return st.save(m)
	}
	l := st.lay()
	slots := st.slots()
	if len(slots) == 0 {
		return nil
	}
	st.cur = max(0, min(st.cur, len(slots)-1))
	sl := slots[st.cur]
	at := -1
	if sl.line >= 0 {
		at = slices.Index(l.Lines[sl.line], sl.id)
	}
	switch s {
	case "up", "k":
		st.cur = roundMove(st.cur, -1, len(slots))
	case "down", "j":
		st.cur = roundMove(st.cur, 1, len(slots))
	case "space", "x":
		if sl.line >= 0 {
			st.place(sl.id, -1, 0)
			return nil
		}
		// Onto the last line in use.
		line := 0
		for i, ln := range l.Lines {
			if len(ln) > 0 {
				line = i
			}
		}
		st.place(sl.id, line, len(l.Lines[line]))
	case "shift+up", "alt+up", "K", "[":
		switch {
		case sl.line < 0:
		case at > 0:
			st.place(sl.id, sl.line, at-1)
		case sl.line > 0:
			st.place(sl.id, sl.line-1, len(l.Lines[sl.line-1]))
		}
	case "shift+down", "alt+down", "J", "]":
		switch {
		case sl.line < 0:
		case at < len(l.Lines[sl.line])-1:
			st.place(sl.id, sl.line, at+1)
		case sl.line < st.maxLines()-1:
			st.place(sl.id, sl.line+1, 0)
		}
	case "1", "2", "3":
		if line := int(s[0] - '1'); line < st.maxLines() {
			st.place(sl.id, line, len(l.Lines[line]))
		}
	case "s":
		i := slices.Index(statusline.Seps, l.Sep)
		l.Sep = statusline.Seps[(i+1)%len(statusline.Seps)]
	case "c":
		if st.tab == stClaude {
			l.Plain = !l.Plain
		}
	case "r":
		switch st.tab {
		case stTop:
			*l = statusline.DefaultTop()
		case stAgent:
			*l = statusline.DefaultAgent()
		default:
			custom := l.Custom
			*l = statusline.Default()
			l.Custom = custom
		}
		st.pad()
	case "d":
		if st.tab == stClaude {
			return st.turnOff(m)
		}
	}
	return nil
}

// trimmed is a layout as saved: no empty lines at the end.
func trimmed(l statusline.Layout) statusline.Layout {
	l = l.Clone()
	for len(l.Lines) > 1 && len(l.Lines[len(l.Lines)-1]) == 0 {
		l.Lines = l.Lines[:len(l.Lines)-1]
	}
	return l
}

// save keeps all three: agtop's own lines at once, and Claude Code's, in
// settings.json too, only when it was changed.
func (st *statusSheet) save(m *Model) tea.Cmd {
	bars := statusline.Bars{Top: trimmed(st.bars.Top), Agent: trimmed(st.bars.Agent)}
	if err := statusline.SaveBars(bars); err != nil {
		st.err = err.Error()
		return nil
	}
	m.bars = bars
	msg := "status lines saved"
	l := trimmed(st.claude)
	if !l.Shown("custom") {
		l.Custom = "" // let go of it only when it's taken out
	}
	if b, _ := json.Marshal(trimmed(st.claude)); string(b) != st.was {
		if err := statusline.Save(l); err != nil {
			st.err = err.Error()
			return nil
		}
		set, err := claude.LoadSettings(st.acct)
		if err == nil {
			err = set.Set("statusLine", map[string]any{"type": "command", "command": statusline.Command(), "padding": 0})
		}
		if err == nil {
			err = set.Save()
		}
		if err != nil {
			st.err = "couldn't save settings.json: " + err.Error()
			return nil
		}
		msg += " · Claude Code sessions for " + st.acct.Name + " show theirs from their next redraw"
	}
	m.sheet = nil
	m.flash(msg, false)
	return nil
}

func (st *statusSheet) turnOff(m *Model) tea.Cmd {
	switch {
	case st.current == "":
		m.flash("there's no status line to turn off", false)
		return nil
	case !statusline.Ours(st.current):
		st.err = "that status line is your own command, not agtop's: it's left alone"
		return nil
	}
	set, err := claude.LoadSettings(st.acct)
	if err == nil {
		err = set.Set("statusLine", nil)
	}
	if err == nil {
		err = set.Save()
	}
	if err != nil {
		st.err = err.Error()
		return nil
	}
	m.sheet = nil
	m.flash("status line turned off for "+st.acct.Name, false)
	return nil
}

// sample is one segment on its own, as the line would show it now.
func (st *statusSheet) sample(m *Model, id string) string {
	if st.tab == stClaude {
		l := statusline.Layout{Lines: [][]string{{id}}, Plain: st.claude.Plain, Custom: st.claude.Custom}
		s, _, _ := strings.Cut(st.rnd.Render(st.in, l, st.acct.ConfigDir, time.Now()), "\n")
		return s
	}
	if s, ok := findBarSeg(st.which(), id); ok {
		return s.draw(st.ctx(m))
	}
	return ""
}

func (st *statusSheet) body(m *Model, w, h int) []string {
	about := map[int]string{
		stAgent:  "the top of an agent's Session: right of its name, and under it",
		stTop:    "the top right of agtop, about every agent at once",
		stClaude: "what Claude Code shows under its prompt · " + st.acct.Name,
	}[st.tab]
	out := []string{sheetTitle("Status lines", about, w), "", "  " + sheetTabs(statusTabNames, st.tab), ""}
	l := st.lay()

	// What the real header, drawn just before this, had no room for.
	var dropped map[string]bool
	if st.tab != stClaude {
		dropped = m.barDropped(st.which())
	}
	// The preview is the real thing, set into the screen's own ground as
	// it will sit there, and live behind the sheet too.
	cw := w - 4
	if st.tab == stClaude {
		line := st.rnd.Render(st.in, *l, st.acct.ConfigDir, time.Now())
		if line == "" {
			line = faint("(nothing to show: space adds a segment)")
		}
		out = append(out, dim("  preview, under Claude Code's prompt, with this session's numbers"))
		well := []string{faint(strings.Repeat("─", cw-2)), paint(cText, "❯ ")}
		for _, ln := range strings.Split(line, "\n") {
			well = append(well, ansi.Truncate(ln, cw-2, "…"))
		}
		out = append(out, cutout(well, cw)...)
		out = append(out, "")
		switch {
		case st.current == "":
			out = append(out, dim("  Claude Code has no status line for this account yet."))
		case statusline.Ours(st.current):
			out = append(out, dim("  Claude Code draws this now: saving updates it."))
		default:
			out = append(out, dim("  Your own status line is kept, as the segment Your own line: move it, or take it out."))
		}
	} else {
		where := "  preview · the agent header, live on the real one too"
		var well []string
		if st.tab == stTop {
			where = "  preview · agtop's top, live on the real one too"
			// The header as it's drawn at this width.
			wasW := m.w
			m.w = cw - 2
			well = m.header()
			m.w = wasW
		} else {
			pw := cw - 2
			_, paneW, _ := m.layout()
			if paneW > 0 {
				pw = min(pw, paneW) // as wide as it really is, when that fits
			}
			well = m.paneHeader(st.a, st.c, pw)
			if paneW == 0 {
				dropped = m.barDropped(barAgent) // no pane behind: the preview's
			}
			well = append(well, "", faint("  ⏺ the conversation goes on here…"))
		}
		out = append(out, dim(where))
		out = append(out, cutout(well, cw)...)
		out = append(out, "")
		note := "  When there isn't room, the segments last on a line go first, and it ends ⋯. What's wrong always shows."
		if len(dropped) > 0 {
			note = paint(cYellow, "  ■") + dim(" hasn't room at this width: it's left out, and the line ends ⋯. What's wrong always shows.")
		}
		out = append(out, dim(note))
	}
	out = append(out, "")

	slots := st.slots()
	st.cur = max(0, min(st.cur, len(slots)-1))
	listH := max(4, h-len(out)-5)
	var list []string
	selAt, i := 0, 0
	infos := st.segs()
	row := func(sl slot) {
		var seg segInfo
		for _, s := range infos {
			if s.id == sl.id {
				seg = s
			}
		}
		mark := paint(cGreen, "■")
		if sl.line < 0 {
			mark = dim("□")
		}
		noRoom := sl.line >= 0 && dropped[sl.id]
		if noRoom {
			mark = paint(cYellow, "■")
		}
		if i == st.cur {
			selAt = len(list)
		}
		sample := st.sample(m, sl.id)
		if sample == "" {
			sample = faint("(nothing right now)")
		}
		about := seg.about
		if sl.id == "custom" && st.tab == stClaude {
			about = st.claude.Custom
		}
		if noRoom {
			about = paint(cYellow, "no room ⋯ move it earlier, or widen the window")
		}
		text := "  " + mark + " " + paint(cText, fit(seg.name, 16)) + fit(sample, 24) + "  " + faint(about)
		list = append(list, sheetRow(ansi.Truncate(text, w-3, "…"), i == st.cur, w))
		i++
	}
	lineName := func(n int) string {
		switch {
		case st.tab == stAgent && n == 0:
			return "Line 1 · right of the name"
		case st.tab == stAgent:
			return "Line 2 · under the name"
		}
		return "Line " + strconv.Itoa(n+1)
	}
	for n, ln := range l.Lines {
		list = append(list, paint(cSub+bold, "  "+lineName(n)))
		if len(ln) == 0 {
			list = append(list, faint("      empty · shift+↓ or "+strconv.Itoa(n+1)+" moves a segment here"))
		}
		for _, id := range ln {
			row(slot{n, id})
		}
	}
	list = append(list, paint(cSub+bold, "  Not shown"))
	for _, sl := range slots[i:] {
		row(sl)
	}
	from, to := window(len(list), selAt, listH)
	out = append(out, list[from:to]...)
	out = append(out, "")
	if st.err != "" {
		out = append(out, paint(cRed, "  "+st.err))
	}
	sepName := strings.TrimSpace(l.Sep)
	if sepName == "" {
		sepName = "space"
	}
	lines := "1 2"
	if st.maxLines() == 3 {
		lines = "1 2 3"
	}
	// Most needed first: what doesn't fit is left off the end.
	pairs := []string{"space", "show/hide", "shift+↑↓", "move", "tab", "next", "enter", "save", "esc", "cancel", lines, "to line", "s", "separator " + sepName}
	if st.tab == stClaude {
		colour := "on"
		if l.Plain {
			colour = "off"
		}
		pairs = append(pairs, "c", "colour "+colour, "d", "off")
	}
	return append(out, keysFit(w, append(pairs, "r", "reset")...))
}

// cutout sets lines into a well of the terminal's own background, cw wide
// and indented to line up with the sheet's text, so what's in it looks as
// it will on screen rather than on the sheet's panel.
func cutout(lines []string, cw int) []string {
	const ground = "\x1b[49m"
	row := func(l string) string { return "  " + onBg(ground, " "+l, cw) }
	out := []string{row("")}
	for _, l := range lines {
		out = append(out, row(l))
	}
	return append(out, row(""))
}
