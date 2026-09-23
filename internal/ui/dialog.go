package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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

var tabNames = []string{"Accounts", "Coding agents", "General"}

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
		meter := func(win claude.Window) string {
			if !win.Present {
				return faint("no reading yet")
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
					fit(meter(u.FiveHour), cols[3]) + fit(meter(u.SevenDay), cols[4]) +
					paint(cSub, cell(5, fmt.Sprintf("%d/%d", av.Live, av.Agents), true)) +
					paint(cText, cell(6, money(av.Today), true)) + dim(cell(7, money(av.Spend), true))
			}
			out = append(out, row(i, mark+" "+line))
		}
		if len(d.accounts) > 0 {
			if av, ok := views[active.ConfigDir]; ok && !av.Usage.FetchedAt.IsZero() {
				out = append(out, "", faint("usage as Claude Code last saw it · "+tildify(active.ConfigDir)+" · "+av.Usage.FetchedAt.Local().Format("Mon 15:04")))
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
			out = append(out, row(i, fit(st.label, 14)+faint("‹ ")+paint(cText, v)+faint(" ›")))
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
		out = append(out, "", keysFit(w, "enter", "use for new sessions", "←→", "change", "e", "edit", "n", "new", "d", "delete"))
	default:
		for i, st := range m.generalSettings() {
			out = append(out, row(i, fit(st.label, 32)+faint("‹ ")+paint(cText, st.value)+faint(" ›")))
		}
		out = append(out, "", keysFit(w, "←→", "change", "tab", "next view", "esc", "back to agents"))
	}
	switch {
	case d.confirm != "":
		out = append(out, "", paint(cText+bold, d.confirm)+"   "+paint(cOrange, "y")+dim(" yes   ")+paint(cOrange, "n")+dim(" no"))
	case d.asking != "":
		out = append(out, "", paint(cOrange, d.asking+" ❯ ")+paint(cText, string(d.input))+paint(cOrange, "▏"))
	}
	return out
}

// overlay draws the dialog box centred over the rendered screen.
func (m *Model) overlay(base string) string {
	bw := min(m.w-6, 110)
	return m.overlayBox(base, m.dialogBody(bw-4), bw)
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
