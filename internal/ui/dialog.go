package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

const (
	tabAccounts = iota
	tabAgents
	tabGeneral
)

var tabNames = []string{"Accounts", "Coding agents", "Settings"}

type dialog struct {
	tab      int
	cursor   int
	input    []rune
	asking   string // what the input line is for; empty when not typing
	draft    string // first answer of a two-step form
	confirm  string
	onYes    func() tea.Cmd
	accounts []acctRow
	agents   []agentDef
}

type acctRow struct {
	claude.Account
	found bool // on disk but not added yet
}

type agentDef struct {
	name, scope, model, desc, path string
}

func (m *Model) openDialog(tab int) {
	m.dialog = &dialog{tab: tab}
	m.loadDialog()
}

func (m *Model) loadDialog() {
	d := m.dialog
	d.accounts = d.accounts[:0]
	known := map[string]bool{}
	for _, a := range m.store.Config.AllAccounts() {
		d.accounts = append(d.accounts, acctRow{Account: a})
		known[a.ConfigDir] = true
	}
	for _, dir := range discoverConfigDirs() {
		if !known[dir] {
			d.accounts = append(d.accounts, acctRow{Account: claude.Account{Name: strings.TrimPrefix(filepath.Base(dir), ".claude-"), ConfigDir: dir}, found: true})
		}
	}
	d.agents = m.agentDefs()
}

// discoverConfigDirs finds Claude config folders in the home directory:
// ~/.claude plus any ~/.claude-* that holds sessions or settings.
func discoverConfigDirs() []string {
	home, _ := os.UserHomeDir()
	ents, _ := os.ReadDir(home)
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() || !strings.HasPrefix(n, ".claude") {
			continue
		}
		dir := filepath.Join(home, n)
		for _, marker := range []string{".claude.json", "projects", "settings.json"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil || n == ".claude" {
				out = append(out, dir)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// agentDirs are the user's agents plus the project agents of every
// repository an agent has worked in.
func (m *Model) agentDirs() [][2]string {
	acct := m.store.Config.ActiveAccount()
	dirs := [][2]string{{"user", filepath.Join(acct.ConfigDir, "agents")}}
	seen := map[string]bool{}
	roots := []string{m.launchDir}
	for _, a := range m.snap.Agents {
		roots = append(roots, a.Repo)
	}
	for _, r := range roots {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		d := filepath.Join(r, ".claude", "agents")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, [2]string{filepath.Base(r), d})
		}
	}
	return dirs
}

func (m *Model) agentDefs() []agentDef {
	out := []agentDef{{name: "claude", scope: "built-in", desc: "Claude Code's default agent"}}
	for _, d := range m.agentDirs() {
		files, _ := filepath.Glob(filepath.Join(d[1], "*.md"))
		for _, f := range files {
			def := readAgentDef(f)
			def.scope = d[0]
			out = append(out, def)
		}
	}
	return out
}

func readAgentDef(path string) agentDef {
	def := agentDef{name: strings.TrimSuffix(filepath.Base(path), ".md"), path: path}
	f, err := os.Open(path)
	if err != nil {
		return def
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inFront, inDesc := false, false
	for i := 0; sc.Scan() && i < 60; i++ {
		l := sc.Text()
		if strings.TrimSpace(l) == "---" {
			if inFront {
				break
			}
			inFront = true
			continue
		}
		if !inFront {
			continue
		}
		switch {
		case strings.HasPrefix(l, "name:"):
			def.name, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "name:")), false
		case strings.HasPrefix(l, "model:"):
			def.model, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "model:")), false
		case strings.HasPrefix(l, "description:"):
			v := strings.TrimSpace(strings.TrimPrefix(l, "description:"))
			inDesc = v == "|" || v == ">" || v == ""
			if !inDesc {
				def.desc = strings.Trim(v, `"'`)
			}
		case inDesc && strings.HasPrefix(l, " "):
			if def.desc == "" {
				def.desc = strings.TrimSpace(l)
			}
		default:
			inDesc = false
		}
	}
	return def
}

// settings rows for the Coding agents and General tabs.
type setting struct {
	label   string
	value   string
	choices []string
	set     func(string)
}

// settingHelp says what a setting does, and what its current choice means.
func settingHelp(label, value string) (what, now string) {
	switch label {
	case "Group by":
		what = "How finished agents are sorted into sections. Needs you, Working, Waiting on you and Idle always come first."
		now = map[string]string{
			"status":  "status: finished agents from the last day under Today, older ones under Earlier.",
			"repo":    "repo: one section per repository and branch, so work on the same code sits together.",
			"account": "account: one section per Claude account, handy when you run several subscriptions.",
			"group":   "group: your own sections; put an agent in one with /group <name>. Ungrouped agents fall back to status.",
		}[value]
	case "Sort rows by":
		what = "The order of rows inside each section. You can also click a column header on the Agents view."
		now = map[string]string{
			"name":   "name: alphabetical, so a row stays put while its agent works.",
			"recent": "recent: most recently active first; rows move as agents update.",
			"cost":   "cost: most expensive first.",
			"cpu":    "cpu: busiest first, by CPU across everything the agent started.",
			"ram":    "ram: heaviest first, by memory across everything the agent started.",
			"time":   "time: longest-running first.",
		}[value]
	case "Hibernate finished agents":
		what = "A finished agent's process stays in memory (often 300–800 MB) until it is stopped. Hibernating stops it; the conversation is kept and enter resumes it."
		if value == "off" {
			now = "off: finished agents stay in memory until you stop them (ctrl+x)."
		} else {
			now = "after " + value + ": an agent that finished and has been idle that long is stopped automatically."
		}
	case "Notify when an agent needs you":
		what = "A macOS notification when an agent starts waiting on you (a question or a permission), not when you have already seen it."
		now = map[string]string{"on": "on: you are notified once per new question.", "off": "off: the Needs you section is the only signal."}[value]
	case "Earlier section":
		what = "Agents that finished more than a day ago. Folded, it is one line with a count and a peek at the names."
		now = map[string]string{"folded": "folded: open it with enter or → when you need it.", "open": "open: every older agent is listed."}[value]
	case "Model":
		what = "The model new sessions start with (the --model flag). Running agents keep their own."
		now = map[string]string{
			"":         "Claude Code default: whatever your Claude Code settings choose.",
			"opus":     "opus: the most capable Opus for hard, long-running work.",
			"opus[1m]": "opus[1m]: Opus with the 1M-token context window, for very large tasks.",
			"sonnet":   "sonnet: faster and cheaper, good for routine work.",
			"haiku":    "haiku: fastest and cheapest, for small tasks.",
			"fable":    "fable: Anthropic's most capable model, at a higher price.",
		}[value]
	case "Effort":
		what = "How hard new sessions think before acting (the --effort flag): more effort is slower and costs more tokens."
		now = map[string]string{
			"":       "Claude Code default: the model's own default level.",
			"low":    "low: quick and cheap; fine for simple, well-specified tasks.",
			"medium": "medium: a balance of speed and care.",
			"high":   "high: careful; the usual choice for real engineering work.",
			"xhigh":  "xhigh: very careful; for tricky, long-horizon tasks.",
			"max":    "max: as thorough as possible, whatever it costs.",
		}[value]
	case "Permissions":
		what = "What new sessions may do without asking you (the --permission-mode flag)."
		now = map[string]string{
			"":                  "Claude Code default: your settings decide.",
			"default":           "default: asks before edits and commands it isn't sure about.",
			"acceptEdits":       "acceptEdits: edits files without asking; still asks before other commands.",
			"plan":              "plan: plans first and changes nothing until you approve.",
			"auto":              "auto: a classifier approves safe actions and asks about risky ones.",
			"bypassPermissions": "bypassPermissions: never asks. Only for sandboxed or throwaway work.",
		}[value]
	}
	return what, now
}

func (m *Model) agentSettings() []setting {
	d := &m.store.Config.Dispatch
	return []setting{
		{"Model", d.Model, []string{"", "opus", "opus[1m]", "sonnet", "haiku", "fable"}, func(v string) { d.Model = v }},
		{"Effort", d.Effort, []string{"", "low", "medium", "high", "xhigh", "max"}, func(v string) { d.Effort = v }},
		{"Permissions", d.Permission, []string{"", "default", "acceptEdits", "plan", "auto", "bypassPermissions"}, func(v string) { d.Permission = v }},
	}
}

func (m *Model) generalSettings() []setting {
	c := &m.store.Config
	hib := "off"
	if c.Hibernate.AfterMinutes > 0 {
		hib = fmt.Sprintf("%dm", c.Hibernate.AfterMinutes)
	}
	notify := "on"
	if c.Quiet {
		notify = "off"
	}
	earlier := "folded"
	if !m.folded("Earlier") {
		earlier = "open"
	}
	sortBy := c.SortBy
	if sortBy == "" {
		sortBy = "name"
	}
	return []setting{
		{"Group by", c.GroupBy, groupModes, func(v string) { c.GroupBy = v }},
		{"Sort rows by", sortBy, sortModes, func(v string) { c.SortBy = v }},
		{"Hibernate finished agents", hib, []string{"off", "15m", "30m", "60m"}, func(v string) {
			c.Hibernate.AfterMinutes = 0
			fmt.Sscanf(v, "%dm", &c.Hibernate.AfterMinutes)
		}},
		{"Notify when an agent needs you", notify, []string{"on", "off"}, func(v string) { c.Quiet = v == "off" }},
		{"Earlier section", earlier, []string{"folded", "open"}, func(v string) {
			if c.Folds == nil {
				c.Folds = map[string]bool{}
			}
			c.Folds["Earlier"] = v == "folded"
		}},
	}
}

func cycle(s setting, dir int) {
	i := 0
	for j, c := range s.choices {
		if c == s.value {
			i = j
		}
	}
	s.set(s.choices[(i+dir+len(s.choices))%len(s.choices)])
}

func (m *Model) dialogLen() int {
	d := m.dialog
	switch d.tab {
	case tabAccounts:
		return len(d.accounts)
	case tabAgents:
		return len(m.agentSettings()) + len(d.agents)
	default:
		return len(m.generalSettings())
	}
}

func (m *Model) dialogKey(k tea.KeyPressMsg, s string) tea.Cmd {
	d := m.dialog
	if d.confirm != "" {
		switch s {
		case "y", "enter":
			f := d.onYes
			d.confirm, d.onYes = "", nil
			if f != nil {
				return f()
			}
		case "n", "esc":
			d.confirm, d.onYes = "", nil
		}
		return nil
	}
	if d.asking != "" {
		switch s {
		case "esc":
			d.asking, d.input = "", nil
		case "enter":
			v := strings.TrimSpace(string(d.input))
			what := d.asking
			d.asking, d.input = "", nil
			return m.answer(what, v)
		default:
			m.dialogEdit(k, s)
		}
		return nil
	}
	switch s {
	case "esc", "q", "ctrl+g", "ctrl+a":
		m.setView(0)
		m.refresh()
		return nil
	case "up", "k":
		d.cursor = max(0, d.cursor-1)
		return nil
	case "down", "j":
		d.cursor = min(m.dialogLen()-1, d.cursor+1)
		return nil
	}
	switch d.tab {
	case tabAccounts:
		return m.accountsKey(s)
	case tabAgents:
		return m.agentsKey(s)
	default:
		rows := m.generalSettings()
		if d.cursor < len(rows) {
			switch s {
			case "enter", "right", "l", "space":
				cycle(rows[d.cursor], 1)
			case "left", "h":
				cycle(rows[d.cursor], -1)
			default:
				return nil
			}
			_ = m.store.SaveConfig()
			m.rebuild()
		}
	}
	return nil
}

func (m *Model) dialogEdit(k tea.KeyPressMsg, s string) {
	d := m.dialog
	switch s {
	case "backspace":
		if len(d.input) > 0 {
			d.input = d.input[:len(d.input)-1]
		}
	case "ctrl+u":
		d.input = nil
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			d.input = append(d.input, []rune(k.Text)...)
		}
	}
}

func (m *Model) ask(what, prefill string) {
	m.dialog.asking, m.dialog.input = what, []rune(prefill)
}

func (m *Model) accountsKey(s string) tea.Cmd {
	d := m.dialog
	if d.cursor >= len(d.accounts) {
		if s == "a" {
			m.ask("account name", "")
		}
		return nil
	}
	row := d.accounts[d.cursor]
	switch s {
	case "enter":
		if row.found {
			m.addAccount(row.Name, row.ConfigDir, false)
		}
		m.store.Config.Active = row.Name
		_ = m.store.SaveConfig()
		m.flash("new sessions start on "+row.Name, false)
		m.loadDialog()
	case "a":
		m.ask("account name", "")
	case "r":
		if !row.found {
			m.ask("rename "+row.Name, row.Name)
		}
	case "l":
		return tea.ExecProcess(actions.Login(row.Account), func(err error) tea.Msg { return doneMsg{err: err, text: "signed in to " + row.Name} })
	case "d", "x":
		if row.IsDefault() || row.found {
			return nil
		}
		m.dialog.confirm = fmt.Sprintf("Remove %s from agtop? Its folder %s is kept.", row.Name, tildify(row.ConfigDir))
		m.dialog.onYes = func() tea.Cmd {
			var keep []claude.Account
			for _, a := range m.store.Config.Accounts {
				if a.ConfigDir != row.ConfigDir {
					keep = append(keep, a)
				}
			}
			m.store.Config.Accounts = keep
			_ = m.store.SaveConfig()
			m.loadDialog()
			return nil
		}
	}
	return nil
}

func (m *Model) agentsKey(s string) tea.Cmd {
	d := m.dialog
	settings := m.agentSettings()
	if d.cursor < len(settings) {
		switch s {
		case "enter", "right", "l", "space":
			cycle(settings[d.cursor], 1)
		case "left", "h":
			cycle(settings[d.cursor], -1)
		case "n":
			m.ask("new agent name", "")
		}
		_ = m.store.SaveConfig()
		return nil
	}
	def := d.agents[d.cursor-len(settings)]
	switch s {
	case "enter":
		name := def.name
		if name == "claude" && def.scope == "built-in" {
			name = ""
		}
		m.store.Config.Dispatch.Agent = name
		_ = m.store.SaveConfig()
		m.flash("new sessions use "+def.name, false)
	case "e":
		if def.path != "" {
			return editFile(def.path)
		}
	case "n":
		m.ask("new agent name", "")
	case "x", "d":
		if def.path == "" {
			return nil
		}
		d.confirm = "Delete the agent " + def.name + " (" + tildify(def.path) + ")?"
		d.onYes = func() tea.Cmd {
			_ = os.Remove(def.path)
			m.loadDialog()
			d.cursor = max(0, d.cursor-1)
			return nil
		}
	}
	return nil
}

func editFile(path string) tea.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	c := exec.Command("sh", "-c", ed+` "$1"`, "sh", path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return dialogReload{err: err} })
}

type dialogReload struct{ err error }

func (m *Model) answer(what, v string) tea.Cmd {
	if v == "" {
		return nil
	}
	d := m.dialog
	switch {
	case what == "account name":
		d.draft = v
		home, _ := os.UserHomeDir()
		m.ask("folder for "+v, "~/"+filepath.Base(filepath.Join(home, ".claude-"+v)))
	case strings.HasPrefix(what, "folder for "):
		return m.addAccount(d.draft, expand(v), true)
	case strings.HasPrefix(what, "rename "):
		old := strings.TrimPrefix(what, "rename ")
		for i, a := range m.store.Config.Accounts {
			if a.Name == old {
				m.store.Config.Accounts[i].Name = v
			}
		}
		if old == m.store.Config.ActiveAccount().Name && claude.DefaultAccount().Name == old {
			m.store.Config.Accounts = append(m.store.Config.Accounts, claude.Account{Name: v, ConfigDir: claude.DefaultAccount().ConfigDir})
		}
		if m.store.Config.Active == old {
			m.store.Config.Active = v
		}
		_ = m.store.SaveConfig()
		m.loadDialog()
	case what == "new agent name":
		name := strings.ToLower(strings.Join(strings.Fields(v), "-"))
		dir := m.agentDirs()[0][1]
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err != nil {
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(path, []byte(fmt.Sprintf(agentTemplate, name)), 0o644)
		}
		return editFile(path)
	}
	return nil
}

const agentTemplate = `---
name: %s
description: When to use this agent, in one or two sentences.
model: inherit
---

What this agent does, and how.
`

func (m *Model) addAccount(name, dir string, login bool) tea.Cmd {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	m.store.Config.Accounts = append(m.store.Config.Accounts, claude.Account{Name: name, ConfigDir: dir})
	_ = m.store.SaveConfig()
	m.loadDialog()
	if !login {
		return nil
	}
	acct := claude.Account{Name: name, ConfigDir: dir}
	return tea.ExecProcess(actions.Login(acct), func(err error) tea.Msg { return doneMsg{err: err, text: "added " + name} })
}

// dialogBody renders the dialog's inner lines at width w.
func (m *Model) dialogBody(w int) []string {
	d := m.dialog
	out := []string{paint(cText+bold, tabNames[d.tab]), ""}
	row := func(i int, s string) string {
		if i == d.cursor {
			return highlight(paint(cOrange, "▍")+" "+s, w)
		}
		return "  " + s
	}
	switch d.tab {
	case tabAccounts:
		views := map[string]fleet.AccountView{}
		for _, av := range m.snap.Accounts {
			views[av.ConfigDir] = av
		}
		cols := []int{16, 20, 26, 18, 18, 9, 10, 10}
		cell := func(i int, v string, alignRight bool) string {
			if alignRight {
				return right(v, cols[i])
			}
			return fit(v, cols[i])
		}
		meter := func(win claude.Window, problem string) string {
			if !win.Present {
				if problem != "" {
					return paint(cYellow, problem)
				}
				return faint("fetching…")
			}
			return bar(win.Percent) + " " + paint(cText, fmt.Sprintf("%3.0f%%", win.Percent))
		}
		head := "   " + faint(cell(0, "NAME", false)+cell(1, "FOLDER", false)+cell(2, "EMAIL", false)+cell(3, "5-HOUR", false)+cell(4, "7-DAY", false)+cell(5, "AGENTS", true)+cell(6, "TODAY", true)+cell(7, "ALL", true))
		out = append(out, head)
		active := m.store.Config.ActiveAccount()
		for i, a := range d.accounts {
			mark := faint("○")
			if a.ConfigDir == active.ConfigDir {
				mark = paint(cOrange, "●")
			}
			av, ok := views[a.ConfigDir]
			name := paint(cText, cell(0, a.Name, false))
			folder := faint(cell(1, tildify(a.ConfigDir), false))
			var line string
			switch {
			case a.found:
				line = paint(cSub, cell(0, a.Name, false)) + folder + paint(cYellow, "found on disk") + dim(" · enter to add")
			case !ok:
				line = name + folder + dim("not loaded yet")
			default:
				u := av.Usage
				email := u.Email
				if email == "" {
					email = "not signed in"
				}
				line = name + folder + dim(cell(2, email, false)) +
					fit(meter(u.FiveHour, u.Problem), cols[3]) + fit(meter(u.SevenDay, ""), cols[4]) +
					paint(cSub, cell(5, fmt.Sprintf("%d/%d", av.Live, av.Agents), true)) +
					paint(cText, cell(6, money(av.Today), true)) + dim(cell(7, money(av.Spend), true))
			}
			out = append(out, row(i, mark+" "+line))
		}
		if len(d.accounts) > 0 {
			if d.cursor < len(d.accounts) {
				out = append(out, "", rule(d.accounts[d.cursor].Name, "", w))
				out = append(out, m.accountDetail(d.accounts[d.cursor], views[d.accounts[d.cursor].ConfigDir], w)...)
			}
		}
		out = append(out, "", keysFit(w, "enter", "use for new sessions", "a", "add", "r", "rename", "l", "sign in", "d", "remove"))
	case tabAgents:
		out = append(out, dim("New sessions start with"))
		settings := m.agentSettings()
		for i, st := range settings {
			v := st.value
			if v == "" {
				v = "Claude Code default"
			}
			out = append(out, m.settingRow(i, st, v, 14, w)...)
		}
		out = append(out, "", dim("Coding agents"))
		cur := m.store.Config.Dispatch.Agent
		for j, a := range d.agents {
			mark := faint("○")
			if a.name == cur || (cur == "" && a.scope == "built-in") {
				mark = paint(cOrange, "●")
			}
			model := ""
			if a.model != "" && a.model != "inherit" {
				model = " · " + strings.TrimPrefix(a.model, "claude-")
			}
			line := mark + " " + paint(cText, fit(a.name, 18)) + faint(fit(a.scope+model, 24)) + dim(fit(a.desc, max(10, w-50)))
			out = append(out, row(len(settings)+j, line))
		}
		out = append(out, m.about(w)...)
		out = append(out, "", keysFit(w, "enter", "use for new sessions", "←→", "change", "e", "edit", "n", "new", "d", "delete"))
	default:
		for i, st := range m.generalSettings() {
			out = append(out, m.settingRow(i, st, st.value, 32, w)...)
		}
		out = append(out, m.about(w)...)
		out = append(out, "", keysFit(w, "←→", "change", "tab", "next view", "esc", "back to agents"))
	}
	return out
}

// overlayBox draws body in a panel of width bw centred over base, dimming
// everything behind it.
func (m *Model) overlayBox(base string, body []string, bw int) string {
	lines := strings.Split(base, "\n")
	inner := bw - 4
	bh := len(body) + 4
	top := max(1, (len(lines)-bh)/2)
	left := (m.w - bw) / 2
	edge := func(s string) string { return paint(cDim, s) }
	box := []string{edge("╭" + strings.Repeat("─", bw-2) + "╮")}
	for _, l := range append([]string{""}, append(body, "")...) {
		box = append(box, edge("│")+panel(" "+fit(l, inner)+" ")+edge("│"))
	}
	box = append(box, edge("╰"+strings.Repeat("─", bw-2)+"╯"))
	for y := range lines {
		lines[y] = faint(ansi.Strip(fit(lines[y], m.w)))
	}
	for i, b := range box {
		y := top + i
		if y >= len(lines) {
			break
		}
		l := lines[y]
		lines[y] = ansi.Truncate(l, left, "") + reset + b + ansi.TruncateLeft(l, left+bw, "")
	}
	return strings.Join(lines, "\n")
}

const panelBG = "\x1b[48;2;30;28;26m"

func panel(s string) string {
	return panelBG + strings.ReplaceAll(s, reset, reset+panelBG) + reset
}

// accountDetail is everything known about one account, under the table.
func (m *Model) accountDetail(a acctRow, av fleet.AccountView, w int) []string {
	now := m.snap.At
	u := av.Usage
	var out []string
	label := func(k string) string { return dim(fit(k, 10)) }
	who := []string{}
	seen := map[string]bool{}
	for _, v := range []string{u.Email, u.Org, u.Role, u.Plan, u.Billing} {
		if v = strings.ReplaceAll(v, "_", " "); v != "" && !seen[v] {
			seen[v] = true
			who = append(who, v)
		}
	}
	if u.Extra {
		who = append(who, "extra usage on")
	}
	if len(who) == 0 {
		who = append(who, "not signed in: press l to sign in")
	}
	out = append(out, label("who")+paint(cText, strings.Join(who, " · ")))
	window := func(name string, win claude.Window) string {
		if !win.Present {
			msg := "no reading"
			if u.Problem != "" {
				msg = u.Problem
			}
			return label(name) + paint(cYellow, msg)
		}
		s := label(name) + bar(win.Percent) + " " + paint(cText, fmt.Sprintf("%.0f%%", win.Percent))
		if !win.ResetsAt.IsZero() {
			when := win.ResetsAt.Local().Format("15:04")
			if win.ResetsAt.Sub(now) > 20*time.Hour {
				when = win.ResetsAt.Local().Format("Mon 15:04")
			}
			if win.ResetsAt.After(now) {
				s += dim("  resets " + when + " · in " + dur(win.ResetsAt.Sub(now)))
			} else {
				s += faint("  reset at " + when + ", since this reading")
			}
		}
		return s
	}
	out = append(out, window("5-hour", u.FiveHour), window("7-day", u.SevenDay))
	out = append(out, label("agents")+paint(cText, fmt.Sprintf("%d running · %d in total", av.Live, av.Agents))+dim("  ·  today ")+paint(cText, money(av.Today))+dim("  ·  all time ")+paint(cText, money(av.Spend)))
	var top []*fleet.Agent
	for _, ag := range m.snap.Agents {
		if ag.Acct.ConfigDir == a.ConfigDir && ag.Spend.Today > 0 {
			top = append(top, ag)
		}
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Spend.Today > top[j].Spend.Today })
	if len(top) > 0 {
		var parts []string
		for i, ag := range top {
			if i == 3 {
				break
			}
			parts = append(parts, oneLine(ag.DisplayName)+" "+paint(cText, money(ag.Spend.Today)))
		}
		out = append(out, label("top today")+dim(strings.Join(parts, "  ·  ")))
	}
	source := "Claude Code's saved reading"
	if u.Fetched {
		source = "fetched from Anthropic"
	}
	status := []string{tildify(a.ConfigDir)}
	if u.Email != "" {
		status = append(status, "signed in")
	}
	if av.Daemon {
		status = append(status, "daemon running")
	} else {
		status = append(status, "daemon not running")
	}
	if !u.FetchedAt.IsZero() {
		status = append(status, "usage "+source+" at "+u.FetchedAt.Local().Format("15:04"))
	}
	out = append(out, label("status")+faint(strings.Join(status, " · ")))
	return out
}

// settingRow is always one line, the highlighted one too, so moving the
// highlight never shifts the page; the About section explains it.
func (m *Model) settingRow(i int, st setting, shown string, labelW, w int) []string {
	_, now := settingHelp(st.label, st.value)
	short := now
	if _, rest, ok := strings.Cut(now, ": "); ok {
		short = rest
	}
	line := fit(st.label, labelW) + faint("‹ ") + paint(cText, fit(shown, 18)) + faint(" › ")
	if room := w - ansi.StringWidth(line) - 6; room > 12 {
		line += faint(ansi.Truncate(short, room, "…"))
	}
	if i == m.dialog.cursor {
		return []string{highlight(paint(cOrange, "▍")+" "+line, w)}
	}
	return []string{"  " + line}
}

const aboutLines = 6

// about explains the highlighted row in a fixed-height section, so the page
// keeps its shape whatever is selected.
func (m *Model) about(w int) []string {
	d := m.dialog
	var title string
	var body []string
	add := func(col, text string) {
		for _, l := range wrap(text, w-4) {
			body = append(body, "  "+paint(col, l))
		}
	}
	switch d.tab {
	case tabGeneral:
		if rows := m.generalSettings(); d.cursor < len(rows) {
			st := rows[d.cursor]
			what, now := settingHelp(st.label, st.value)
			title = st.label
			add(cSub, what)
			add(cText, now)
		}
	case tabAgents:
		settings := m.agentSettings()
		switch {
		case d.cursor < len(settings):
			st := settings[d.cursor]
			what, now := settingHelp(st.label, st.value)
			title = st.label + " for new sessions"
			add(cSub, what)
			add(cText, now)
		case d.cursor-len(settings) < len(d.agents):
			a := d.agents[d.cursor-len(settings)]
			title = a.name
			meta := a.scope
			if a.model != "" {
				meta += " · model " + a.model
			}
			if a.path != "" {
				meta += " · " + tildify(a.path)
			}
			add(cSub, meta)
			add(cText, a.desc)
		}
	}
	out := []string{"", rule("About "+title, "", w)}
	for i := 0; i < aboutLines; i++ {
		if i < len(body) {
			out = append(out, body[i])
		} else {
			out = append(out, "")
		}
	}
	return out
}
