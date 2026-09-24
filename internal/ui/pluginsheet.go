package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- /plugins ---

// pluginSheet is agtop's /plugins: what's installed (on/off, update,
// remove, what each brings and costs), what the marketplaces offer
// (search, install), and the marketplaces themselves. Changes go through
// `claude plugin`, so they're exactly what Claude Code's screen would do.
type pluginSheet struct {
	conn string // the Session it was opened from, reloaded after changes
	acct claude.Account
	cwd  string

	tab       int
	cur       [3]int
	query     []rune
	queryPos  int
	adding    bool // typing a marketplace to add
	source    []rune
	sourcePos int

	loaded    bool
	installed []claude.Plugin
	available []claude.Plugin
	markets   []claude.Marketplace
	costs     map[string]string // always-on tokens, by plugin id
	costAsked map[string]bool

	busy    string // what's running now
	err     string
	done    string
	armed   string // an id x was pressed on once
	changed bool
}

const (
	plInstalled = iota
	plDiscover
	plMarkets
)

func (m *Model) openPlugins(c *hostConn, a *fleet.Agent) tea.Cmd {
	p := &pluginSheet{conn: c.key, acct: a.Acct, cwd: firstNonEmpty(c.sess.Info.Cwd, a.Cwd), costs: map[string]string{}, costAsked: map[string]bool{}}
	m.sheet = p
	return p.load()
}

func (p *pluginSheet) load() tea.Cmd {
	acct, cwd := p.acct, p.cwd
	type lists struct {
		inst, avail []claude.Plugin
		markets     []claude.Marketplace
	}
	return sheetDo(func() (lists, error) {
		inst, avail, err := claude.Plugins(acct, cwd)
		if err != nil {
			return lists{}, err
		}
		ms, err := claude.Marketplaces(acct, cwd)
		return lists{inst, avail, ms}, err
	}, func(m *Model, l lists, err error) tea.Cmd {
		if m.sheet != p {
			return nil
		}
		p.loaded = true
		if err != nil {
			p.err = err.Error()
			return nil
		}
		p.installed, p.available, p.markets = l.inst, l.avail, l.markets
		return p.askCost()
	})
}

// askCost fetches what the selected installed plugin adds to every
// session, once.
func (p *pluginSheet) askCost() tea.Cmd {
	if p.tab != plInstalled || len(p.installed) == 0 {
		return nil
	}
	pl := p.installed[min(p.cur[plInstalled], len(p.installed)-1)]
	if p.costAsked[pl.ID] {
		return nil
	}
	p.costAsked[pl.ID] = true
	acct, dir := p.acct, firstNonEmpty(pl.ProjectPath, p.cwd)
	return sheetDo(func() (string, error) { return claude.PluginCost(acct, dir, pl.ID) },
		func(m *Model, cost string, err error) tea.Cmd {
			if err == nil && cost != "" {
				p.costs[pl.ID] = cost
			}
			return nil
		})
}

// run does a `claude plugin …` change, then reads everything again.
func (p *pluginSheet) run(what, dir string, args ...string) tea.Cmd {
	if p.busy != "" {
		return nil
	}
	p.busy, p.err, p.done, p.armed = what, "", "", ""
	acct := p.acct
	return sheetDo(func() ([]byte, error) { return claude.PluginCLI(acct, dir, args...) },
		func(m *Model, _ []byte, err error) tea.Cmd {
			p.busy = ""
			if err != nil {
				p.err = err.Error()
				return nil
			}
			p.done, p.changed = strings.TrimSuffix(what, "…"), true
			return p.load()
		})
}

// shown is the Discover list for the search.
func (p *pluginSheet) shown() []claude.Plugin {
	q := strings.ToLower(strings.TrimSpace(string(p.query)))
	if q == "" {
		return p.available
	}
	var out []claude.Plugin
	for _, pl := range p.available {
		if strings.Contains(strings.ToLower(pl.Name+" "+pl.Marketplace+" "+pl.Description), q) {
			out = append(out, pl)
		}
	}
	return out
}

func (p *pluginSheet) size() int {
	switch p.tab {
	case plInstalled:
		return len(p.installed)
	case plDiscover:
		return len(p.shown())
	}
	return len(p.markets)
}

func (p *pluginSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	if p.adding {
		switch s {
		case "esc":
			p.adding = false
		case "enter":
			src := strings.TrimSpace(string(p.source))
			p.adding, p.source, p.sourcePos = false, nil, 0
			if src != "" {
				return p.run("adding "+src+"…", p.cwd, "marketplace", "add", src)
			}
		default:
			p.source, p.sourcePos, _ = edit(p.source, p.sourcePos, k, s)
		}
		return nil
	}
	cur := &p.cur[p.tab]
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
		if p.changed {
			return m.reloadPlugins(p.conn)
		}
		return nil
	case "tab", "shift+tab":
		d := 1
		if s == "shift+tab" {
			d = 2
		}
		p.tab, p.armed = (p.tab+d)%3, ""
		return p.askCost()
	case "up":
		*cur = roundMove(*cur, -1, p.size())
		p.armed = ""
		return p.askCost()
	case "down":
		*cur = roundMove(*cur, 1, p.size())
		p.armed = ""
		return p.askCost()
	case "pgup":
		*cur = max(0, *cur-10)
		return p.askCost()
	case "pgdown":
		*cur = max(0, min(p.size()-1, *cur+10))
		return p.askCost()
	}
	switch p.tab {
	case plInstalled:
		if len(p.installed) == 0 {
			return nil
		}
		pl := p.installed[min(*cur, len(p.installed)-1)]
		dir, scope := firstNonEmpty(pl.ProjectPath, p.cwd), []string{"--scope", firstNonEmpty(pl.Scope, "user")}
		switch s {
		case "space", "enter":
			if pl.Enabled {
				return p.run("turning off "+pl.Name+"…", dir, append([]string{"disable", pl.ID}, scope...)...)
			}
			return p.run("turning on "+pl.Name+"…", dir, append([]string{"enable", pl.ID}, scope...)...)
		case "u":
			return p.run("updating "+pl.Name+"…", dir, append([]string{"update", pl.ID}, scope...)...)
		case "x", "delete", "backspace":
			if p.armed != pl.ID {
				p.armed = pl.ID
				return nil
			}
			return p.run("removing "+pl.Name+"…", dir, append([]string{"uninstall", pl.ID}, scope...)...)
		}
	case plDiscover:
		list := p.shown()
		switch s {
		case "enter":
			if len(list) == 0 {
				return nil
			}
			pl := list[min(*cur, len(list)-1)]
			return p.run("installing "+pl.Name+"…", p.cwd, "install", pl.ID, "--scope", "user")
		default:
			if q, pos, ok := edit(p.query, p.queryPos, k, s); ok {
				p.query, p.queryPos, *cur = q, pos, 0
			}
		}
	case plMarkets:
		switch s {
		case "a", "+":
			p.adding = true
			return nil
		case "U":
			return p.run("updating every marketplace…", p.cwd, "marketplace", "update")
		}
		if len(p.markets) == 0 {
			return nil
		}
		mk := p.markets[min(*cur, len(p.markets)-1)]
		switch s {
		case "u", "enter":
			return p.run("updating "+mk.Name+"…", p.cwd, "marketplace", "update", mk.Name)
		case "x", "delete", "backspace":
			if p.armed != mk.Name {
				p.armed = mk.Name
				return nil
			}
			return p.run("removing "+mk.Name+"…", p.cwd, "marketplace", "remove", mk.Name)
		}
	}
	return nil
}

func (p *pluginSheet) body(m *Model, w, h int) []string {
	enabled := 0
	for _, pl := range p.installed {
		if pl.Enabled {
			enabled++
		}
	}
	out := []string{
		sheetTitle("Plugins", "for "+p.acct.Name+" · "+tildify(p.cwd), w),
		"",
		sheetTabs([]string{
			fmt.Sprintf("Installed %d/%d on", enabled, len(p.installed)),
			fmt.Sprintf("Discover %d", len(p.available)),
			fmt.Sprintf("Marketplaces %d", len(p.markets)),
		}, p.tab),
		"",
	}
	detail := p.detail(w)
	listH := max(3, h-len(out)-len(detail)-5)
	switch {
	case !p.loaded:
		out = append(out, dim("  reading plugins…"))
	case p.tab == plInstalled:
		out = append(out, p.installedRows(w, listH)...)
	case p.tab == plDiscover:
		out = append(out, "  "+paint(cOrange, "⌕ ")+textField(p.query, p.queryPos, true, "search "+fmt.Sprint(len(p.available))+" plugins", w-6), "")
		out = append(out, p.discoverRows(w, listH-2)...)
	default:
		out = append(out, p.marketRows(w, listH)...)
	}
	out = append(out, "", faint(strings.Repeat("─", w)))
	out = append(out, detail...)
	out = append(out, "", p.footer(w))
	return out
}

func (p *pluginSheet) installedRows(w, h int) []string {
	if len(p.installed) == 0 {
		return []string{dim("  nothing installed yet · tab to Discover")}
	}
	var out []string
	cur := min(p.cur[plInstalled], len(p.installed)-1)
	from, to := window(len(p.installed), cur, h)
	for i := from; i < to; i++ {
		pl := p.installed[i]
		mark, col := paint(cGreen, "●"), cText
		if !pl.Enabled {
			mark, col = dim("○"), cDim
		}
		scope := pl.Scope
		if scope == pl.Marketplace {
			scope = "" // claude.ai's synced plugins
		}
		if pl.ProjectPath != "" {
			scope += " · " + tildify(pl.ProjectPath)
		}
		line := mark + " " + paint(col+bold, fit(pl.Name, 24)) + " " + dim(fit(pl.Version, 9)) + " " + dim(fit(pl.Marketplace, 24)) + " " + faint(ansi.Truncate(scope, max(0, w-66), "…"))
		if p.armed == pl.ID {
			line = paint(cRed, "x again removes "+pl.Name)
		}
		out = append(out, sheetRow(line, i == cur, w))
	}
	return out
}

func (p *pluginSheet) discoverRows(w, h int) []string {
	list := p.shown()
	if len(list) == 0 {
		return []string{dim("  no plugins match")}
	}
	var out []string
	cur := min(p.cur[plDiscover], len(list)-1)
	from, to := window(len(list), cur, h)
	for i := from; i < to; i++ {
		pl := list[i]
		line := paint(cText+bold, fit(pl.Name, 26)) + " " + dim(fit(installs(pl.Installs), 7)) + " " + faint(ansi.Truncate(oneLine(pl.Description), max(0, w-40), "…"))
		out = append(out, sheetRow(line, i == cur, w))
	}
	return out
}

func (p *pluginSheet) marketRows(w, h int) []string {
	if len(p.markets) == 0 {
		return []string{dim("  no marketplaces · a adds one")}
	}
	counts := map[string]int{}
	for _, pl := range p.available {
		counts[pl.Marketplace]++
	}
	for _, pl := range p.installed {
		counts[pl.Marketplace]++
	}
	var out []string
	cur := min(p.cur[plMarkets], len(p.markets)-1)
	from, to := window(len(p.markets), cur, h)
	for i := from; i < to; i++ {
		mk := p.markets[i]
		line := paint(cText+bold, fit(mk.Name, 28)) + " " + dim(fit(fmt.Sprintf("%d plugins", counts[mk.Name]), 12)) + " " + faint(ansi.Truncate(mk.Where(), max(0, w-46), "…"))
		if p.armed == mk.Name {
			line = paint(cRed, "x again removes the "+mk.Name+" marketplace")
		}
		out = append(out, sheetRow(line, i == cur, w))
	}
	return out
}

// detail is the selected row, in full: what it is, what it brings.
func (p *pluginSheet) detail(w int) []string {
	wrapDim := func(s string) (out []string) {
		for i, l := range wrap(oneLine(s), w-4) {
			if i == 3 {
				break
			}
			out = append(out, "  "+paint(cSub, l))
		}
		return out
	}
	switch p.tab {
	case plInstalled:
		if len(p.installed) == 0 {
			return nil
		}
		pl := p.installed[min(p.cur[plInstalled], len(p.installed)-1)]
		out := []string{"  " + paint(cText+bold, pl.ID)}
		out = append(out, wrapDim(firstNonEmpty(pl.Description, "no description"))...)
		var parts []string
		add := func(n []string, one, many string) {
			switch len(n) {
			case 0:
			case 1:
				parts = append(parts, "1 "+one+": "+n[0])
			default:
				parts = append(parts, fmt.Sprintf("%d %s: %s", len(n), many, strings.Join(n, ", ")))
			}
		}
		add(pl.Parts.Skills, "skill", "skills")
		add(pl.Parts.Agents, "agent", "agents")
		add(pl.Parts.Commands, "command", "commands")
		add(pl.Parts.Hooks, "hook", "hooks")
		add(pl.Parts.MCP, "MCP server", "MCP servers")
		if c := p.costs[pl.ID]; c != "" {
			parts = append(parts, c+" in every session")
		}
		if len(parts) > 0 {
			out = append(out, "  "+dim(ansi.Truncate(strings.Join(parts, " · "), w-4, "…")))
		}
		return out
	case plDiscover:
		list := p.shown()
		if len(list) == 0 {
			return nil
		}
		pl := list[min(p.cur[plDiscover], len(list)-1)]
		out := []string{"  " + paint(cText+bold, pl.ID) + dim("  "+installs(pl.Installs)+" installs")}
		return append(out, wrapDim(firstNonEmpty(pl.Description, "no description"))...)
	}
	if len(p.markets) == 0 {
		return nil
	}
	mk := p.markets[min(p.cur[plMarkets], len(p.markets)-1)]
	return []string{"  " + paint(cText+bold, mk.Name), "  " + dim(mk.Source+" · "+mk.Where())}
}

func (p *pluginSheet) footer(w int) string {
	switch {
	case p.adding:
		return paint(cOrange, "add a marketplace ❯ ") + textField(p.source, p.sourcePos, true, "owner/repo, a URL or a folder", w-24)
	case p.busy != "":
		return paint(cYellow, "⋯ "+p.busy)
	case p.err != "":
		return paint(cRed, ansi.Truncate(p.err, w, "…"))
	}
	var k string
	switch p.tab {
	case plInstalled:
		k = keysFit(w-30, "space", "on/off", "u", "update", "x", "remove", "tab", "discover", "esc", "done")
	case plDiscover:
		k = keysFit(w-30, "type", "search", "enter", "install", "tab", "marketplaces", "esc", "done")
	default:
		k = keysFit(w-30, "a", "add", "u", "update", "U", "update all", "x", "remove", "esc", "done")
	}
	if p.done != "" {
		k = paint(cGreen, "✓ "+p.done) + "   " + k
	}
	if p.changed {
		k += dim("  · sessions reload plugins")
	}
	return k
}

func installs(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// reloadPlugins has the Session's agtop session pick up plugin changes; a
// turn under way is left alone.
func (m *Model) reloadPlugins(key string) tea.Cmd {
	c := m.host
	if c == nil || c.key != key || c.client == nil {
		return nil
	}
	if c.sess.Info.State == "working" {
		m.flash("plugins change for "+c.sess.Info.Name+" after /reload-plugins, once this turn ends", false)
		return nil
	}
	cl := c.client
	return hostCmd(func() error { return cl.Send("/reload-plugins") })
}
