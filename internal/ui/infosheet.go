package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- /status, /usage, /stats ---

// infoSheet is Claude Code's /status, /usage and /stats done agtop's way,
// as tabs of one sheet: the agent and what it runs on, the plan's limits
// and what's been spent, the account's history, and where its settings
// are. It reads live data every frame, so it follows the agent while it's
// open.
type infoSheet struct {
	conn   string
	tab    int
	scroll [infoTabs]int
	cur    int // the Settings tab's row

	stats    claude.Stats
	statsErr error
	settings []settingsLink // the Settings tab's rows, read when it opens
}

const (
	infoStatus = iota
	infoContext
	infoUsage
	infoHistory
	infoSettings
	infoTabs
)

var infoTabNames = [infoTabs]string{"Status", "Context", "Usage", "History", "Settings"}

// openInfo opens the sheet on one of its tabs.
func (m *Model) openInfo(c *hostConn, tab int) {
	k := &infoSheet{conn: c.key, tab: tab}
	if a := m.agentByKey(c.key); a != nil {
		k.stats, k.statsErr = claude.LoadStats(a.Acct)
		k.settings = m.settingsLinks(c, a)
	}
	m.sheet = k
	// A fresh count of the context, from a host that can.
	if cl := c.client; cl != nil && c.sess.Info.Proto >= 2 {
		go func() { _ = cl.AskContext() }()
	}
}

func (k *infoSheet) width(*Model) int { return 100 }

func (m *Model) sheetConn(key string) *hostConn {
	if m.host != nil && m.host.key == key {
		return m.host
	}
	return nil
}

// accountView is the fleet's view of the account a runs on.
func (m *Model) accountView(a *fleet.Agent) (fleet.AccountView, bool) {
	for _, av := range m.snap.Accounts {
		if av.ConfigDir == a.Acct.ConfigDir || av.ConfigDir == "" && a.Acct.ConfigDir == "" {
			return av, true
		}
	}
	return fleet.AccountView{Account: a.Acct}, false
}

// infoRow is a label and its value, lined up.
func infoRow(label, value string, w int) string {
	if value == "" {
		value = faint("–")
	}
	return "  " + dim(fit(label, 14)) + " " + ansi.Truncate(value, max(0, w-18), "…")
}

func infoHead(s string) string { return paint(cSub+bold, "  "+s) }

func (k *infoSheet) key(m *Model, _ tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c", "q":
		m.sheet = nil
	case "tab", "right":
		k.tab = (k.tab + 1) % infoTabs
	case "shift+tab", "left":
		k.tab = (k.tab + infoTabs - 1) % infoTabs
	case "up":
		if k.tab == infoSettings {
			k.cur = roundMove(k.cur, -1, len(k.settings))
		} else {
			k.scroll[k.tab] = max(0, k.scroll[k.tab]-1)
		}
	case "down":
		if k.tab == infoSettings {
			k.cur = roundMove(k.cur, 1, len(k.settings))
		} else {
			k.scroll[k.tab]++
		}
	case "pgup":
		k.scroll[k.tab] = max(0, k.scroll[k.tab]-10)
	case "pgdown":
		k.scroll[k.tab] += 10
	case "enter":
		if k.tab == infoSettings && k.cur < len(k.settings) {
			return k.settings[k.cur].open(m)
		}
	}
	return nil
}

func (k *infoSheet) body(m *Model, w, h int) []string {
	c, a := m.sheetConn(k.conn), m.agentByKey(k.conn)
	name := "Status"
	if a != nil {
		name = firstNonEmpty(a.DisplayName, a.Name, "this agent")
	}
	out := []string{sheetTitle(name, "the agent, its account, and Claude Code", w), "", sheetTabs(infoTabNames[:], k.tab), ""}
	if c == nil || a == nil {
		return append(out, dim("  this agent's Session is closed"), "", keysFit(w, "esc", "close"))
	}
	var lines []string
	switch k.tab {
	case infoStatus:
		lines = statusLines(m, c, a, w)
	case infoContext:
		lines = contextLines(c, w)
	case infoUsage:
		lines = usageLines(m, c, a, w)
	case infoHistory:
		lines = k.historyLines(a, w, h-len(out)-3)
	case infoSettings:
		lines = k.settingsLines(w)
	}
	// Long tabs scroll; the tab row and keys stay put.
	room := max(3, h-len(out)-2)
	k.scroll[k.tab] = max(0, min(k.scroll[k.tab], len(lines)-room))
	if len(lines) > room {
		lines = lines[k.scroll[k.tab] : k.scroll[k.tab]+room]
	}
	out = append(out, lines...)
	switch {
	case k.tab == infoSettings:
		return append(out, "", keysFit(w, "↑↓", "choose", "enter", "open", "←→", "tabs", "esc", "close"))
	case len(lines) == room:
		return append(out, "", keysFit(w, "←→", "tabs", "↑↓", "scroll", "esc", "close"))
	}
	return append(out, "", keysFit(w, "←→", "tabs", "esc", "close"))
}

func sessionID(c *hostConn, a *fleet.Agent) string {
	if c != nil && c.sess.Info.SessionID != "" {
		return c.sess.Info.SessionID
	}
	return a.SessionID
}

// statusLines is the Status tab: the session, its model, the account and
// Claude Code.
func statusLines(m *Model, c *hostConn, a *fleet.Agent, w int) []string {
	s, now := c.sess, time.Now()
	out := []string{infoHead("Session")}
	out = append(out, infoRow("name", paint(cText+bold, firstNonEmpty(s.Info.Name, a.DisplayName, a.Name)), w))
	state := firstNonEmpty(s.Info.State, a.State)
	if c.client != nil && !s.Info.StartedAt.IsZero() {
		state += dim(" · up " + dur(now.Sub(s.Info.StartedAt)))
	}
	out = append(out, infoRow("state", paint(cText, state), w))
	out = append(out, infoRow("session id", dim(sessionID(c, a)), w))
	folder := tildify(firstNonEmpty(s.Info.Cwd, s.Cwd, a.Cwd))
	if a.Branch != "" {
		folder += dim(" on ") + paint(cSub, a.Branch)
	}
	out = append(out, infoRow("folder", folder, w))
	var conn string
	switch {
	case c.client != nil:
		conn = paint(cOrange, "agtop") + dim(fmt.Sprintf(" · host pid %d", s.Info.HostPID))
		if s.Info.ClaudePID != 0 {
			conn += dim(fmt.Sprintf(" · claude pid %d", s.Info.ClaudePID))
		} else {
			conn += dim(" · claude asleep, wakes on the next message")
		}
	case a.Interactive:
		conn = paint(cSub, "Claude Code") + dim(" · a terminal session, read from its transcript")
	default:
		conn = paint(cSub, "Claude Code") + dim(" · its daemon, read from the transcript")
	}
	out = append(out, infoRow("connection", conn, w))
	if a.TranscriptPath != "" {
		out = append(out, infoRow("transcript", dim(tildify(a.TranscriptPath)), w))
	}

	out = append(out, "", infoHead("Model"))
	out = append(out, infoRow("model", paint(cText, convo.PrettyModel(firstNonEmpty(s.Model, s.Info.Model))), w))
	out = append(out, infoRow("effort", paint(cText, s.Info.Effort), w))
	out = append(out, infoRow("permissions", paint(cText, s.Info.PermissionMode), w))
	if s.Context > 0 {
		win := int(claude.ContextWindow(firstNonEmpty(s.Model, s.Info.Model)))
		p := float64(s.Context) / float64(win) * 100
		out = append(out, infoRow("context", ctxBar(p)+" "+paint(cSub, fmt.Sprintf("%.0f%%", p))+dim(" · "+convo.Tokens(s.Context)+" of "+convo.Tokens(win)), w))
	}

	av, _ := m.accountView(a)
	out = append(out, "", infoHead("Account"))
	who := paint(cText+bold, firstNonEmpty(av.Name, a.Acct.Name))
	if av.Usage.Email != "" {
		who += dim(" · " + av.Usage.Email)
	}
	out = append(out, infoRow("login", who, w))
	var plan []string
	for _, p := range []string{av.Usage.Plan, av.Usage.Org, av.Usage.Role} {
		if p != "" {
			plan = append(plan, p)
		}
	}
	out = append(out, infoRow("plan", paint(cText, strings.Join(plan, " · ")), w))
	out = append(out, infoRow("config", dim(tildify(firstNonEmpty(a.Acct.ConfigDir, claude.DefaultAccount().ConfigDir))), w))

	out = append(out, "", infoHead("Claude Code"))
	ver := s.Version
	if ver == "" && c.client == nil {
		ver = faint("not known from a transcript")
	}
	out = append(out, infoRow("version", paint(cText, ver), w))
	if s.NTools > 0 {
		out = append(out, infoRow("tools", paint(cText, fmt.Sprint(s.NTools)), w))
	}
	if len(s.Commands) > 0 {
		out = append(out, infoRow("/ commands", paint(cText, fmt.Sprint(len(s.Commands))), w))
	}
	switch {
	case len(s.MCP) > 0:
		servers := append(s.MCP[:0:0], s.MCP...)
		sort.SliceStable(servers, func(i, j int) bool { return servers[i].Status != "connected" && servers[j].Status == "connected" })
		for i, sv := range servers {
			label := ""
			if i == 0 {
				label = "mcp"
			}
			col := cGreen
			switch sv.Status {
			case "connected":
			case "pending":
				col = cYellow
			default:
				col = cRed
			}
			out = append(out, infoRow(label, paint(col, "● ")+paint(cText, sv.Name)+" "+dim(sv.Status), w))
		}
	case c.client != nil:
		out = append(out, infoRow("mcp", faint("none"), w))
	}
	return out
}

// bigMeter is a window's use as a wide bar, with the share of the window
// gone marked, and when it resets.
func bigMeter(pct float64, resets, now time.Time, window time.Duration, w int) string {
	w = max(10, w)
	pace := -1.0
	if !resets.IsZero() && resets.After(now) {
		pace = 1 - float64(resets.Sub(now))/float64(window)
	}
	col := usageColor(pct)
	if pace >= 0 && pct < 60 && pct/100 > pace+0.1 {
		col = cYellow // burning faster than the window allows
	}
	fill := min(w, max(0, int(pct/100*float64(w)+0.5)))
	if pct > 0 && fill == 0 {
		fill = 1
	}
	tick := -1
	if pace >= 0 {
		tick = min(w-1, int(pace*float64(w)))
	}
	var b strings.Builder
	for i := range w {
		switch {
		case i == tick && i < fill:
			b.WriteString(paint(cText, "╋"))
		case i == tick:
			b.WriteString(paint(cSub, "┼"))
		case i < fill:
			b.WriteString(paint(col, "━"))
		default:
			b.WriteString(faint("─"))
		}
	}
	s := b.String() + " " + paint(col+bold, fmt.Sprintf("%3.0f%%", pct))
	if !resets.IsZero() {
		if resets.After(now) {
			at := resets.Local().Format("15:04")
			if resets.Sub(now) > 20*time.Hour {
				at = resets.Local().Format("Mon 15:04")
			}
			s += dim(" · resets in " + roughly(resets.Sub(now)) + ", " + at)
		} else {
			s += dim(" · has reset since")
		}
	}
	return s
}

// usageLines is the Usage tab: the plan's limits, what this agent has
// spent, and every account's.
func usageLines(m *Model, c *hostConn, a *fleet.Agent, w int) []string {
	var out []string
	now := m.snap.At
	if now.IsZero() {
		now = time.Now()
	}
	av, _ := m.accountView(a)
	u := av.Usage
	head := "Plan · " + firstNonEmpty(av.Name, a.Acct.Name)
	if u.Plan != "" {
		head += " · " + u.Plan
	}
	out = append(out, infoHead(head))
	meterW := min(40, max(10, w-60))
	if u.FiveHour.Present {
		out = append(out, infoRow("5-hour", bigMeter(u.FiveHour.Percent, u.FiveHour.ResetsAt, now, 5*time.Hour, meterW), w))
	}
	if u.SevenDay.Present {
		out = append(out, infoRow("weekly", bigMeter(u.SevenDay.Percent, u.SevenDay.ResetsAt, now, 7*24*time.Hour, meterW), w))
	}
	if !u.FiveHour.Present && !u.SevenDay.Present {
		out = append(out, infoRow("limits", faint(firstNonEmpty(u.Problem, "no reading yet: this account's limits come in with the next refresh")), w))
	} else {
		if u.Extra {
			out = append(out, infoRow("extra usage", paint(cText, "on")+dim(" · past the limits, it's billed"), w))
		}
		var note []string
		if !u.FetchedAt.IsZero() {
			note = append(note, "read "+u.FetchedAt.Local().Format("15:04"))
		}
		if u.Problem != "" {
			note = append(note, u.Problem)
		}
		if len(note) > 0 {
			out = append(out, infoRow("", faint(strings.Join(note, " · ")+" · ┼ marks how much of the window has gone"), w))
		}
	}

	s := c.sess
	t := s.Totals(time.Now())
	out = append(out, "", infoHead("This agent"))
	cost := paint(cText+bold, money(t.Cost))
	if t.Turns > 0 {
		cost += dim(fmt.Sprintf(" · %d turns · %s working", t.Turns, dur(t.Working)))
	}
	out = append(out, infoRow("cost", cost, w))
	if t.Requests > 0 {
		out = append(out, infoRow("requests", paint(cText, fmt.Sprint(t.Requests))+dim(fmt.Sprintf(" · %d tool calls", t.ToolCalls)), w))
		read := t.In + t.CacheRead + t.CacheOut
		hit := ""
		if read > 0 {
			hit = dim(fmt.Sprintf(" · %.0f%% from cache", float64(t.CacheRead)/float64(read)*100))
		}
		out = append(out, infoRow("tokens in", paint(cText, convo.Tokens(read))+hit, w))
		out = append(out, infoRow("", dim(convo.Tokens(t.In)+" new · "+convo.Tokens(t.CacheRead)+" cache read · "+convo.Tokens(t.CacheOut)+" cache written"), w))
		out = append(out, infoRow("tokens out", paint(cText, convo.Tokens(t.Out)), w))
		// By model, when there's more than one (subagents, /model).
		type byModel struct {
			name    string
			in, out int
		}
		per := map[string]*byModel{}
		for _, r := range s.Requests {
			name := convo.PrettyModel(r.Model)
			if per[name] == nil {
				per[name] = &byModel{name: name}
			}
			per[name].in += r.Usage.InputTokens + r.Usage.CacheReadInputTokens + r.Usage.CacheCreationInputTokens
			per[name].out += r.Usage.OutputTokens
		}
		if len(per) > 1 {
			var rows []*byModel
			for _, b := range per {
				rows = append(rows, b)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].in > rows[j].in })
			for i, b := range rows {
				label := ""
				if i == 0 {
					label = "by model"
				}
				out = append(out, infoRow(label, paint(cText, fit(b.name, 18))+dim(convo.Tokens(b.in)+" in · "+convo.Tokens(b.out)+" out"), w))
			}
		}
	}

	if len(m.snap.Accounts) > 0 {
		out = append(out, "", infoHead("Every account"))
		for _, x := range m.snap.Accounts {
			var parts []string
			if x.Usage.FiveHour.Present {
				parts = append(parts, dim("5h ")+paint(usageColor(x.Usage.FiveHour.Percent), fmt.Sprintf("%3.0f%%", x.Usage.FiveHour.Percent)))
			}
			if x.Usage.SevenDay.Present {
				parts = append(parts, dim("7d ")+paint(usageColor(x.Usage.SevenDay.Percent), fmt.Sprintf("%3.0f%%", x.Usage.SevenDay.Percent)))
			}
			parts = append(parts, paint(cText, money(x.Today))+dim(" today"))
			if x.Live > 0 {
				parts = append(parts, dim(fmt.Sprintf("%d running", x.Live)))
			}
			name := x.Name
			if x.ConfigDir == a.Acct.ConfigDir {
				name += " ◂"
			}
			out = append(out, infoRow(name, strings.Join(parts, dim("  ·  ")), w))
		}
	}
	return out
}

// unknownSheet asks what to do with a / command agtop doesn't know
// (askUnknown): open Claude Code on it, or send it to Claude after all.
type unknownSheet struct {
	conn, line string
	send       func() tea.Cmd
	cur        int // 0 open in Claude Code, 1 send as a message
}

func (k *unknownSheet) width(*Model) int { return 72 }

func (k *unknownSheet) key(m *Model, _ tea.KeyPressMsg, s string) tea.Cmd {
	c, a := m.sheetConn(k.conn), m.agentByKey(k.conn)
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "down", "tab", "shift+tab":
		k.cur = 1 - k.cur
	case "enter":
		m.sheet = nil
		if c == nil || a == nil {
			return nil
		}
		if k.cur == 1 {
			return k.send()
		}
		c.input, c.back = c.input[:0], 0
		return m.openScreen(c, a, k.line)
	}
	return nil
}

func (k *unknownSheet) body(m *Model, w, h int) []string {
	name, _, _ := strings.Cut(k.line, " ")
	out := []string{sheetTitle("/"+name, "isn't a command agtop knows", w), ""}
	for _, l := range wrap("agtop has no view of its own for it, and this session didn't list it among its commands, so it's most likely one of Claude Code's own screens.", w-4) {
		out = append(out, "  "+paint(cSub, l))
	}
	out = append(out, "")
	choices := []struct{ name, about string }{
		{"Open it in Claude Code", "in this agent's folder and account · esc, then ctrl+c twice, comes back"},
		{"Send it to Claude as a message", "as you typed it"},
	}
	for i, ch := range choices {
		out = append(out, sheetRow(paint(cText+bold, ch.name), i == k.cur, w))
		out = append(out, "    "+faint(ansi.Truncate(ch.about, w-6, "…")))
	}
	return append(out, "", keysFit(w, "↑↓", "choose", "enter", "go", "esc", "cancel"))
}

// claudeSheet says, before it does, that a claude: command (claudeCloud)
// opens a real Claude Code: a fresh one, in this agent's folder and on its
// account, not this conversation.
type claudeSheet struct{ conn, line string }

func (k *claudeSheet) width(*Model) int { return 72 }

func (k *claudeSheet) key(m *Model, _ tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "enter":
		m.sheet = nil
		c, a := m.sheetConn(k.conn), m.agentByKey(k.conn)
		if c == nil || a == nil {
			return nil
		}
		c.input, c.back = c.input[:0], 0
		return m.openScreen(c, a, k.line)
	}
	return nil
}

func (k *claudeSheet) body(m *Model, w, h int) []string {
	name, _, _ := strings.Cut(k.line, " ")
	about := ""
	for _, c := range claudeCloud {
		if c.Name == name {
			about = c.Description
		}
	}
	out := []string{sheetTitle("claude:"+name, about, w), ""}
	banner := "  " + paint(cOrange, "↗ ") + paint(cText+bold, "This opens a real Claude Code")
	out = append(out, onBg(bgChrome, banner, w), "")
	for _, l := range wrap("/"+name+" is Claude Code's own, for your account or Claude's cloud, and agtop leaves it to Claude Code. agtop hands the terminal to a fresh Claude Code in this agent's folder, on its account, and opens /"+k.line+" there. It isn't this conversation.", w-4) {
		out = append(out, "  "+paint(cSub, l))
	}
	out = append(out, "")
	for _, l := range wrap("When you're done: esc, then ctrl+c twice, and you're back here.", w-4) {
		out = append(out, "  "+paint(cSub, l))
	}
	return append(out, "", keysFit(w, "enter", "open Claude Code", "esc", "cancel"))
}
