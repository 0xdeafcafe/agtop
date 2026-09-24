package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/statusline"
)

// --- agtop's own status lines ---

// The top bar (top right of the window, about every agent) and the agent
// header (top of an agent's Session) are status lines you build in
// /statusline, like Claude Code's, from the segments below. What always
// shows stays put: the counts and clanker, an agent's name, state and
// connection, and anything that's wrong.

// barSeg is one thing an agtop line can show. draw returns it painted, or
// "" when it has nothing to say.
type barSeg struct {
	id, name, about string
	draw            func(x *barCtx) string
}

// barCtx is what a line is drawn from: the fleet, and for the agent
// header the agent and its conversation.
type barCtx struct {
	m *Model
	t tally
	a *fleet.Agent
	c *hostConn
}

const (
	barTop = iota
	barAgent
)

var topSegs = []barSeg{
	{"today", "Spend today", "what every account has spent today", func(x *barCtx) string {
		return paint(cText, money(x.t.today)) + dim(" today")
	}},
	{"usage", "Plan usage", "the account in use: its 5-hour and weekly limits, and when they reset", func(x *barCtx) string {
		return x.m.activeUsage()
	}},
	{"accounts", "Every account", "each account's 5-hour limit, when there are several", func(x *barCtx) string {
		if len(x.m.snap.Accounts) < 2 {
			return ""
		}
		var parts []string
		for _, av := range x.m.snap.Accounts {
			if av.Usage.FiveHour.Present {
				p := av.Usage.FiveHour.Percent
				parts = append(parts, dim(av.Name+" ")+paint(usageColor(p), fmt.Sprintf("%.0f%%", p)))
			}
		}
		return strings.Join(parts, "  ")
	}},
	{"agents", "Agents", "how many agents there are, and how many are running", func(x *barCtx) string {
		live, n := 0, 0
		for _, av := range x.m.snap.Accounts {
			live, n = live+av.Live, n+av.Agents
		}
		if n == 0 {
			return ""
		}
		return dim(fmt.Sprintf("%d agents · %d running", n, live))
	}},
	{"ram", "Memory", "what agents and their processes hold", func(x *barCtx) string {
		return dim(mem(x.m.snap.Machine.TotalMem) + " ram")
	}},
	{"cpu", "CPU", "what agents and their processes use", func(x *barCtx) string {
		return dim(fmt.Sprintf("%.0f%% cpu", x.m.snap.Machine.TotalCPU))
	}},
	{"tmp", "Temp files", "agents' scratch left on disk, when there's much", func(x *barCtx) string {
		if t := x.m.tempTotal(); t >= tempShown {
			return dim(disk(t) + " tmp")
		}
		return ""
	}},
	{"account", "Account", "the account new agents start on", func(x *barCtx) string {
		return dim(x.m.store.Config.ActiveAccount().Name)
	}},
	{"clock", "Clock", "the time of day", func(x *barCtx) string {
		return dim(x.m.snap.At.Local().Format("15:04"))
	}},
}

var agentSegs = []barSeg{
	{"context", "Context", "how full the context window is", func(x *barCtx) string {
		s := x.c.sess
		if s.Context <= 0 {
			return ""
		}
		win := int(claude.ContextWindow(firstNonEmpty(s.Model, s.Info.Model)))
		p := float64(s.Context) / float64(win) * 100
		return dim("ctx ") + ctxBar(p) + " " + paint(cSub, fmt.Sprintf("%.0f%%", p))
	}},
	{"cost", "Cost", "what the agent has cost so far", func(x *barCtx) string {
		if c := x.c.sess.Info.CostUSD; c > 0 {
			return paint(cText+bold, money(c))
		}
		return ""
	}},
	{"folder", "Folder", "the folder it works in", func(x *barCtx) string {
		return dim(tildify(x.a.Cwd))
	}},
	{"branch", "Git branch", "the branch checked out there", func(x *barCtx) string {
		if x.a.Branch == "" {
			return ""
		}
		return paint(cSub, x.a.Branch)
	}},
	{"model", "Model", "the model it runs", func(x *barCtx) string {
		if md := firstNonEmpty(x.c.sess.Model, x.c.sess.Info.Model); md != "" {
			return dim(convo.PrettyModel(md))
		}
		return ""
	}},
	{"effort", "Effort", "the effort level", func(x *barCtx) string {
		return dim(x.c.sess.Info.Effort)
	}},
	{"mode", "Permissions", "the permission mode: plan in blue, the ones that don't ask in orange", func(x *barCtx) string {
		mode := x.c.sess.Info.PermissionMode
		switch mode {
		case "":
			return ""
		case "plan":
			return paint(cBlue, mode)
		case "auto", "acceptEdits", "bypassPermissions":
			return paint(cOrange, mode)
		}
		return paint(cSub, mode)
	}},
	{"tmp", "Temp files", "its scratch left on disk, when there's much", func(x *barCtx) string {
		a := x.a
		if a.Temp < tempShown {
			return ""
		}
		t := paint(cSub, disk(a.Temp)+" tmp")
		if a.Temp >= 1<<30 {
			t = paint(cYellow, disk(a.Temp)+" tmp")
		}
		if a.PID == 0 {
			t += dim(" /clean")
		}
		return t
	}},
	{"lines", "Lines changed", "lines it added and removed", func(x *barCtx) string {
		add, del := 0, 0
		for _, f := range x.c.sess.Changes() {
			add, del = add+f.Add, del+f.Del
		}
		if add+del == 0 {
			return ""
		}
		return paint(cGreen, fmt.Sprintf("+%d", add)) + " " + paint(cRed, fmt.Sprintf("−%d", del))
	}},
	{"files", "Files changed", "how many files it changed", func(x *barCtx) string {
		if n := len(x.c.sess.Changes()); n > 0 {
			return dim(fmt.Sprintf("%d file%s", n, plural(n)))
		}
		return ""
	}},
	{"turns", "Turns", "how many of your messages it has had", func(x *barCtx) string {
		if n := len(x.c.sess.Turns); n > 0 {
			return dim(fmt.Sprintf("%d turn%s", n, plural(n)))
		}
		return ""
	}},
	{"time", "Time", "how long since its first turn", func(x *barCtx) string {
		ts := x.c.sess.Turns
		if len(ts) == 0 || ts[0].Start.IsZero() {
			return ""
		}
		return dim(dur(time.Since(ts[0].Start).Round(time.Minute)))
	}},
	{"cache", "Prompt cache", "while the cache is warm, until when: a message after that re-reads everything", func(x *barCtx) string {
		w := x.c.sess.Info.CacheWarm
		if w.IsZero() || time.Now().After(w) {
			return ""
		}
		return dim("warm till ") + paint(cGreen, w.Local().Format("15:04"))
	}},
	{"queue", "Queue", "messages waiting for the turn to end", func(x *barCtx) string {
		if n := len(x.c.sess.Info.Queue); n > 0 {
			return paint(cOrange, fmt.Sprintf("%d queued", n))
		}
		return ""
	}},
	{"account", "Account", "the Claude account it runs on", func(x *barCtx) string {
		return dim(x.a.Acct.Name)
	}},
	{"session", "Session id", "the conversation's short id", func(x *barCtx) string {
		if id := firstNonEmpty(x.c.sess.Info.SessionID, x.a.SessionID); len(id) >= 8 {
			return faint(id[:8])
		}
		return ""
	}},
	{"clock", "Clock", "the time of day", func(x *barCtx) string {
		return dim(time.Now().Format("15:04"))
	}},
}

func barSegs(which int) []barSeg {
	if which == barTop {
		return topSegs
	}
	return agentSegs
}

func findBarSeg(which int, id string) (barSeg, bool) {
	for _, s := range barSegs(which) {
		if s.id == id {
			return s, true
		}
	}
	return barSeg{}, false
}

func usageColor(p float64) string {
	switch {
	case p >= 85:
		return cRed
	case p >= 60:
		return cYellow
	}
	return cGreen
}

// barLayout is the line being drawn: the one saved, or the one being built
// in /statusline, so the real header shows your changes as you make them.
func (m *Model) barLayout(which int) statusline.Layout {
	if st, ok := m.sheet.(*statusSheet); ok {
		switch {
		case which == barTop && st.tab == stTop:
			return st.bars.Top
		case which == barAgent && st.tab == stAgent:
			return st.bars.Agent
		}
	}
	if which == barTop {
		if m.bars.Top.Lines == nil {
			return statusline.DefaultTop()
		}
		return m.bars.Top
	}
	if m.bars.Agent.Lines == nil {
		return statusline.DefaultAgent()
	}
	return m.bars.Agent
}

// barMore ends a line that had to leave segments out for room.
var barMore = faint(" ⋯")

// barLine draws line i of an agtop line, at most w wide. When it's too
// wide, the segments last on the line go first, whole, and the line ends
// with ⋯; which went is kept for /statusline to point out.
func (m *Model) barLine(which, i int, x *barCtx, w int) string {
	l := m.barLayout(which)
	if i >= len(l.Lines) || w <= 0 {
		return ""
	}
	sep := l.Sep
	if sep == "" {
		sep = " · "
	}
	var parts, ids []string
	for _, id := range l.Lines[i] {
		if s, ok := findBarSeg(which, id); ok {
			if p := s.draw(x); p != "" {
				parts, ids = append(parts, p), append(ids, id)
			}
		}
	}
	line := strings.Join(parts, dim(sep))
	n := len(parts)
	if ansi.StringWidth(line) > w {
		for n > 0 && ansi.StringWidth(strings.Join(parts[:n], dim(sep)))+ansi.StringWidth(barMore) > w {
			n--
		}
		line = strings.Join(parts[:n], dim(sep)) + barMore
		if n == 0 {
			line = strings.TrimLeft(barMore, " ")
		}
	}
	if m.barDrops == nil {
		m.barDrops = map[int][]string{}
	}
	m.barDrops[which*10+i] = ids[n:]
	return line
}

// barDropped is the segments the header last left out for room.
func (m *Model) barDropped(which int) map[string]bool {
	out := map[string]bool{}
	for i := range statusline.BarLines {
		for _, id := range m.barDrops[which*10+i] {
			out[id] = true
		}
	}
	return out
}
