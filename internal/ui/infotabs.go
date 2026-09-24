package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- the History and Settings tabs of infoSheet ---

// contextLines is the Context tab: what fills the window, as a grid with
// its legend beside it, then what's inside each part.
func contextLines(c *hostConn, w int) []string {
	s := c.sess
	u := s.Usage
	if u == nil || u.Max <= 0 {
		var out []string
		if s.Context > 0 {
			win := int(claude.ContextWindow(firstNonEmpty(s.Model, s.Info.Model)))
			p := float64(s.Context) / float64(win) * 100
			out = append(out, infoRow("context", ctxBar(p)+" "+paint(cSub, fmt.Sprintf("%.0f%%", p))+dim(" · "+convo.Tokens(s.Context)+" of "+convo.Tokens(win)), w), "")
		}
		why := "the breakdown comes when this turn ends"
		switch {
		case c.client == nil:
			why = "a breakdown needs an agtop-mode session: Claude Code's own sessions only say how full it is"
		case s.Info.Proto < 2:
			why = "this agent's host is older than the breakdown: it comes once the host restarts (it does after resting idle)"
		case s.Info.ClaudePID == 0:
			why = "Claude is asleep: the breakdown comes when it next wakes"
		}
		return append(out, dim("  "+why))
	}
	meta := convo.Tokens(u.Total) + " of " + convo.Tokens(u.Max) + fmt.Sprintf(" · %.0f%%", float64(u.Total)/float64(u.Max)*100)
	if u.AutoCompact && u.AutoCompactAt > 0 {
		meta += " · auto-compacts at " + convo.Tokens(u.AutoCompactAt)
	}
	out := []string{infoHead(convo.PrettyModel(u.Model)) + "  " + dim(meta), ""}
	grid := convo.ContextGrid(u, 20, 10)
	legend := convo.ContextLegend(u, "")
	for i := range max(len(grid), len(legend)) {
		l, r := strings.Repeat(" ", 39), ""
		if i < len(grid) {
			l = grid[i]
		}
		if i < len(legend) {
			r = legend[i]
		}
		out = append(out, "  "+l+"    "+r)
	}
	if d := convo.ContextDetail(u, "  ", 6); len(d) > 0 {
		out = append(append(out, ""), d...)
	}
	when := "just now"
	if ago := time.Since(u.At); ago >= time.Minute {
		when = roughly(ago) + " ago"
	}
	out = append(out, "", faint("  counted "+when+", from the last reply and local estimates"))
	return out
}

// historyLines is the History tab: Claude Code's own record of the
// account's use (what its /stats shows), by day and by model, fitted to h.
func (k *infoSheet) historyLines(a *fleet.Agent, w, h int) []string {
	st := k.stats
	if k.statsErr != nil {
		why := "Claude Code hasn't kept a record for this account yet"
		if !errors.Is(k.statsErr, fs.ErrNotExist) {
			why = "couldn't read stats-cache.json: " + k.statsErr.Error()
		}
		return []string{dim("  " + why)}
	}
	out := []string{infoHead("All time · " + a.Acct.Name)}
	since := ""
	if !st.First.IsZero() {
		since = dim(" since " + st.First.Local().Format("2 Jan 2006"))
	}
	out = append(out, infoRow("sessions", paint(cText, thousands(int64(st.Sessions)))+dim(" · "+thousands(int64(st.Messages))+" messages")+since, w))
	if st.Longest > 0 {
		out = append(out, infoRow("longest", paint(cText, dur(st.Longest))+dim(fmt.Sprintf(" · %s messages · %s", thousands(int64(st.LongestMsgs)), st.LongestAt.Local().Format("2 Jan"))), w))
	}
	if peak := hourSpark(st.Hours); peak != "" {
		out = append(out, infoRow("by hour", peak, w))
		out = append(out, infoRow("", faint("0     6     12    18   23"), w))
	}

	// Days: as many as fit, newest last, bars by messages.
	days := st.Days
	room := max(5, h-len(out)-4-min(4, len(st.Models)))
	if len(days) > room {
		days = days[len(days)-room:]
	}
	if len(days) > 0 {
		out = append(out, "", infoHead(fmt.Sprintf("Last %d days", len(days))))
		top := 1
		for _, d := range days {
			top = max(top, d.Messages)
		}
		barW := max(8, min(30, w-70))
		for _, d := range days {
			fill := d.Messages * barW / top
			if d.Messages > 0 && fill == 0 {
				fill = 1
			}
			bar := paint(cOrange, strings.Repeat("━", fill)) + faint(strings.Repeat("─", barW-fill))
			nums := fmt.Sprintf("%7s msgs  %3d sessions  %6s tools  %6s tok", short(d.Messages), d.Sessions, short(d.ToolCalls), bigTokens(d.TotalTokens()))
			out = append(out, "  "+dim(fit(d.Date.Format("Mon 2 Jan"), 12))+" "+bar+" "+dim(nums))
		}
	}
	if len(st.Models) > 0 {
		out = append(out, "", infoHead("By model"))
		for _, t := range st.Models {
			out = append(out, infoRow(convo.PrettyModel(t.Model), paint(cText, fit(bigTokens(t.Total()), 9))+dim(bigTokens(t.Out)+" out · "+bigTokens(t.CacheRead)+" from cache"), w))
		}
	}
	if st.Computed != "" {
		out = append(out, "", faint("  as Claude Code last counted it, up to "+st.Computed))
	}
	return out
}

// bigTokens is a token count to three figures, up to billions.
func bigTokens(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	}
	return convo.Tokens(int(n))
}

// short is a count to three figures: 950, 12.6k, 1.3M.
func short(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// hourSpark draws the sessions started in each hour as one bar per hour.
func hourSpark(h [24]int) string {
	top := 0
	for _, n := range h {
		top = max(top, n)
	}
	if top == 0 {
		return ""
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, n := range h {
		if n == 0 {
			b.WriteString(faint("·"))
			continue
		}
		b.WriteString(paint(cOrange, string(bars[min(len(bars)-1, n*len(bars)/(top+1))])))
	}
	return b.String()
}

// settingsLink is a row of the Settings tab: an area of settings, what it
// holds now, and the sheet that edits it.
type settingsLink struct {
	name, value, about string
	claude             bool // opens Claude Code's own screen
	open               func(m *Model) tea.Cmd
}

// settingsLinks reads what the Settings tab shows, from every settings
// file the agent's session reads.
func (m *Model) settingsLinks(c *hostConn, a *fleet.Agent) []settingsLink {
	cwd := firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	var allow, ask, deny, hooks, plugins, env int
	model, mode, line := "", "", ""
	for i, f := range settingsFiles(a.Acct, cwd) {
		s, err := claude.LoadSettingsFile(f.path)
		if err != nil {
			continue
		}
		var rules []string
		for _, kind := range []struct {
			key string
			n   *int
		}{{"permissions.allow", &allow}, {"permissions.ask", &ask}, {"permissions.deny", &deny}} {
			rules = nil
			if s.Get(kind.key, &rules) {
				*kind.n += len(rules)
			}
		}
		var hs map[string][]json.RawMessage
		if s.Get("hooks", &hs) {
			for _, list := range hs {
				hooks += len(list)
			}
		}
		var on map[string]bool
		if s.Get("enabledPlugins", &on) {
			for _, v := range on {
				if v {
					plugins++
				}
			}
		}
		if i == 0 {
			env = len(s.Env())
		}
		model = firstNonEmpty(s.String("model"), model)
		mode = firstNonEmpty(s.String("permissions.defaultMode"), mode)
		line = firstNonEmpty(s.String("statusLine.command"), line)
	}
	skills, cmds := 0, 0
	for _, f := range claude.Commands(firstNonEmpty(a.Acct.ConfigDir, claude.DefaultAccount().ConfigDir), cwd) {
		if f.Skill {
			skills++
		} else {
			cmds++
		}
	}
	join := func(parts ...string) string {
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return strings.Join(out, " · ")
	}
	count := func(n int, one, many string) string {
		switch n {
		case 0:
			return ""
		case 1:
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	switch {
	case strings.Contains(line, "agtop"):
		line = "agtop's"
	case line != "":
		line = "your own command"
	}
	connected := 0
	for _, s := range c.sess.MCP {
		if s.Status == "connected" {
			connected++
		}
	}
	mcp := ""
	if len(c.sess.MCP) > 0 {
		mcp = fmt.Sprintf("%d of %d connected", connected, len(c.sess.MCP))
	}
	return []settingsLink{
		{name: "Claude Code", value: join(model, count(env, "env var", "env vars")), about: "settings.json and the environment every session starts with",
			open: func(m *Model) tea.Cmd { m.sheet = nil; m.openDialog(tabClaude); return nil }},
		{name: "Permissions", value: join(mode, count(allow, "allow", "allow"), count(ask, "ask", "ask"), count(deny, "deny", "deny")), about: "what tools may do without asking",
			open: func(m *Model) tea.Cmd { m.openPermissions(c, a); return nil }},
		{name: "Hooks", value: count(hooks, "hook", "hooks"), about: "commands run around tools, prompts and sessions",
			open: func(m *Model) tea.Cmd { return m.openHooks(c, a) }},
		{name: "Plugins", value: count(plugins, "on", "on"), about: "installed plugins, discover more, marketplaces",
			open: func(m *Model) tea.Cmd { return m.openPlugins(c, a) }},
		{name: "Skills", value: join(count(skills, "skill", "skills"), count(cmds, "command", "commands")), about: "what Claude picks up, and your own / commands",
			open: func(m *Model) tea.Cmd { m.openSkills(c, a); return nil }},
		{name: "Status line", value: line, about: "this header, agtop's top bar, and Claude Code's",
			open: func(m *Model) tea.Cmd { m.openStatusLine(c, a); return nil }},
		{name: "Memory", about: "CLAUDE.md and the other files Claude reads",
			open: func(m *Model) tea.Cmd { m.sheet = nil; m.showView(c, "memory"); return nil }},
		{name: "MCP servers", value: mcp, about: "connect, sign in, tools", claude: true,
			open: func(m *Model) tea.Cmd { m.sheet = nil; return m.openScreen(c, a, "mcp") }},
	}
}

// settingsLines is the Settings tab.
func (k *infoSheet) settingsLines(w int) []string {
	var out []string
	for i, l := range k.settings {
		value := l.value
		if value == "" {
			value = "–"
		}
		tag := ""
		if l.claude {
			tag = paint(cSub, "  claude code ↗")
		}
		line := paint(cText+bold, fit(l.name, 14)) + " " + paint(cText, fit(value, 26)) + " " + faint(ansi.Truncate(l.about, max(0, w-48-ansi.StringWidth(tag)), "…")) + tag
		out = append(out, sheetRow(line, i == k.cur, w))
	}
	return out
}
