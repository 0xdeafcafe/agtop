package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/menubar"
	"github.com/0xdeafcafe/agtop/internal/state"
)

const (
	tabAccounts = iota
	tabAgents
	tabGeneral
	tabClaude
)

var tabNames = []string{"Accounts", "Coding agents", "General", "Claude"}

type dialog struct {
	tab      int
	cursor   int
	input    []rune
	asking   string // what the input line is for; empty when not typing
	draft    string // first answer of a two-step form
	confirm  string
	onYes    func() tea.Cmd
	found    []claude.Account // Claude folders on disk, not added yet
	profiles []agent.Profile  // other agents' config homes
	agents   []agentDef

	claudeAcct int              // which account the Claude tab edits
	claude     *claude.Settings // that account's settings.json
}

// acctRow is one account in Accounts: a Claude sign-in with the folders
// signed in as it, a Claude folder that isn't signed in to one, or another
// agent's.
type acctRow struct {
	login   *fleet.LoginView
	folders []claude.Account
	found   bool          // a folder on disk, not added yet
	profile agent.Profile // another agent's; its Kind is empty for Claude's
}

// name is what the row is called.
func (r acctRow) name() string {
	switch {
	case r.login != nil:
		return r.login.Name
	case r.profile.Kind != "":
		return strings.TrimPrefix(filepath.Base(r.profile.Dir), ".")
	case len(r.folders) > 0:
		return r.folders[0].Name
	}
	return ""
}

// accountRows are the accounts Accounts lists: Claude's sign-ins first,
// each with the folders signed in as it, then Claude folders that aren't,
// then every other agent's.
func (m *Model) accountRows() []acctRow {
	d := m.dialog
	byLogin := map[string][]claude.Account{}
	var loose []claude.Account
	signedIn := map[string]bool{}
	for _, lv := range m.snap.Logins {
		signedIn[lv.ID] = true
	}
	views := map[string]fleet.AccountView{}
	for _, av := range m.snap.Accounts {
		views[av.ConfigDir] = av
	}
	for _, a := range m.store.Config.AllAccounts() {
		if id := views[a.ConfigDir].Usage.AccountID; id != "" && signedIn[id] {
			byLogin[id] = append(byLogin[id], a)
			continue
		}
		loose = append(loose, a)
	}
	var out []acctRow
	for i := range m.snap.Logins {
		lv := &m.snap.Logins[i]
		out = append(out, acctRow{login: lv, folders: byLogin[lv.ID]})
	}
	for _, a := range loose {
		out = append(out, acctRow{folders: []claude.Account{a}})
	}
	for _, a := range d.found {
		out = append(out, acctRow{folders: []claude.Account{a}, found: true})
	}
	for _, p := range d.profiles {
		out = append(out, acctRow{profile: p})
	}
	return out
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
	d.found = d.found[:0]
	known := map[string]bool{}
	for _, a := range m.store.Config.AllAccounts() {
		known[a.ConfigDir] = true
	}
	for _, dir := range discoverConfigDirs() {
		if !known[dir] {
			d.found = append(d.found, claude.Account{Name: strings.TrimPrefix(filepath.Base(dir), ".claude-"), ConfigDir: dir})
		}
	}
	d.profiles = otherProfiles()
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
	case "Layout":
		what = "How agtop lays out Agents and the Session, now and next time. #view and shift+← → change it too."
		now = map[string]string{
			"split": "split: Agents on the left, the picked agent's Session beside them, when the screen is wide enough.",
			"agent": "agent: one Session with the whole screen; esc shows Agents, and the next one you open has the whole screen too.",
			"list":  "list: Agents alone; a Session you open takes the screen until esc.",
			"ask":   "ask: agtop asks which the next time it opens.",
		}[value]
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
	case "Theme":
		what = "What agtop's colours are made for. Its text and panels are shades between your terminal's background and text colour, so they follow its theme; its orange, green and red stay, made as easy to read on your background as on agtop's own."
		now = map[string]string{
			"match terminal": "match terminal: agtop asks the terminal for its background and text, again whenever you come back to it; a terminal that doesn't say gets agtop's dark.",
			"dark":           "dark: agtop's own dark colours, whatever the terminal says.",
			"light":          "light: agtop's own light colours, for a light terminal that doesn't say it's light.",
		}[value]
	case "Colours":
		what = "How agtop tells good from bad: added and removed lines in a diff, done and failed steps and agents."
		now = map[string]string{
			"standard":     "standard: green and red.",
			"colour-blind": "colour-blind: sky blue and amber, which stay apart for red-green and blue-yellow colour blindness alike; + and − still mark every diff line.",
		}[value]
	case "Notify when an agent needs you":
		what = "A macOS notification when an agent starts waiting on you (a question or a permission), not when you have already seen it."
		now = map[string]string{"on": "on: you are notified once per new question.", "off": "off: the Needs you section is the only signal."}[value]
	case "Menu bar icon":
		what = "agtop in the menu bar: every account's usage, what's working, and who needs you, with a badge. A question with a few answers can be answered from its notification's buttons, a permission allowed or denied. The first time, it's built with Xcode's Swift compiler (a few seconds)."
		now = map[string]string{"on": "on: it opens with agtop, and its menu can open it at login; agtop's own notifications give way to its.", "off": "off: no menu bar icon."}[value]
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
	case "Account":
		what = "Whose Claude settings this tab edits. Each account has its own settings.json."
		now = value + ": changes here save to that account's settings.json."
	case "New sessions run in":
		what = "Where new Claude sessions run. agtop mode runs Claude Code headless in agtop's own host and draws the conversation here; the daemon is Claude Code's own background service and its terminal screen."
		now = map[string]string{
			"":       "agtop mode: agtop's conversation view, queue, approvals and overview. /agtop moves a daemon session over.",
			"daemon": "daemon: Claude Code's background service, shown through its own terminal screen.",
		}[value]
	case "When a usage limit hits":
		what = "What an agtop-mode session does when a 5-hour or weekly usage limit stops it."
		now = map[string]string{
			"":     "ask each session: it asks once whether to continue by itself when the limit resets.",
			"auto": "auto: every session continues by itself at the reset, a few seconds apart.",
			"off":  "off: sessions wait for you after a limit.",
		}[value]
	case "Compress idle transcripts":
		what = "Transcripts untouched for two days are stored compressed the way macOS stores its own system files: same names, same contents, and Claude Code, grep, your editor and agtop read them exactly as before; the system decompresses as they're read (about 30 ms for a 33 MB one). A transcript written to again is stored plainly again. Each is checked byte for byte before it replaces the original."
		now = map[string]string{
			"on":  "on: about a quarter of the disk they took (33 MB → 8 MB for the biggest).",
			"off": "off: transcripts stay as Claude Code writes them.",
			"":    "off: transcripts stay as Claude Code writes them.",
		}[value]
	case "Clean up done work after":
		what = "What happens to an agent's worktree and temp work once you've marked it done (alt+d) and left it alone. Stopping an agent never removes anything. A worktree goes only if git says every change in it is committed and pushed; its branch stays. One that isn't is kept and listed in the Cleanup view (the last tab)."
		now = map[string]string{
			"":    "3h: done work that's committed and pushed goes 3 hours after it was last touched.",
			"off": "off: nothing goes by itself; the Cleanup view removes what you choose.",
		}[value]
		if now == "" {
			now = value + ": done work that's committed and pushed goes " + value + " after it was last touched."
		}
	case "Rest Claude after":
		what = "How long an idle agtop-mode session keeps Claude Code running. An idle Claude Code holds 150-580 MB; after this it stops, and your next message starts it again in about a second. The prompt cache lasts an hour either way."
		now = "Claude stops after " + firstNonEmpty(value, "5 min") + " idle; the host, the conversation and its queue stay."
	case "Quick start":
		what = "Starts agtop-mode sessions without Claude Code's non-essential network traffic (CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC), for them alone. Claude Code is ready in about 0.3s instead of 0.65s, which you feel on every new session and every message after one has rested."
		now = map[string]string{
			"":   "off: sessions start with everything Claude Code has.",
			"on": "on: quicker starts, but no DesignSync, Projects, plugin downloads or live preview in them. Your telemetry settings are unaffected.",
		}[value]
	case "Default model", "Default effort", "Permission mode", "Always think", "Co-authored-by in commits", "Keep transcripts for", "Hooks",
		"Subagent model", "Max output tokens", "Bash timeout", "Telemetry", "Non-essential traffic":
		what, now = claudeHelp(label, value)
	}
	return what, now
}

// claudeHelp explains the Claude tab's settings.json and env rows.
func claudeHelp(label, value string) (what, now string) {
	unset := "not set: Claude Code's own default."
	switch label {
	case "Default model":
		what = "The model every session on this account starts with (settings.json model), unless a session or agtop picks one."
	case "Default effort":
		what = "How hard sessions think by default (settings.json effortLevel)."
	case "Permission mode":
		what = "What sessions may do without asking (settings.json permissions.defaultMode). Your allow and deny rules are kept."
	case "Always think":
		what = "Extended thinking on every request (settings.json alwaysThinkingEnabled)."
	case "Co-authored-by in commits":
		what = "Whether Claude adds a Co-authored-by line to commits it makes (settings.json includeCoAuthoredBy)."
	case "Keep transcripts for":
		what = "How many days Claude Code keeps old transcripts before cleaning them up (settings.json cleanupPeriodDays)."
		if value != "" {
			return what, value + " days: older transcripts are removed."
		}
	case "Hooks":
		what = "Whether the hooks in your settings run (settings.json disableAllHooks). Turning them off stops every hook without deleting them."
	case "Subagent model":
		what = "The model subagents use, whatever the main session runs (env CLAUDE_CODE_SUBAGENT_MODEL)."
	case "Max output tokens":
		what = "The longest single reply Claude may write (env CLAUDE_CODE_MAX_OUTPUT_TOKENS)."
	case "Bash timeout":
		what = "How long a shell command may run before it's stopped (env BASH_DEFAULT_TIMEOUT_MS)."
		if ms, err := strconv.Atoi(value); err == nil {
			return what, fmt.Sprintf("%d minutes.", ms/60000)
		}
	case "Telemetry":
		what = "Whether Claude Code sends usage telemetry (env DISABLE_TELEMETRY)."
		if value == "1" {
			return what, "off: no telemetry is sent."
		}
	case "Non-essential traffic":
		what = "Whether Claude Code makes non-essential network calls such as update checks (env CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC)."
		if value == "1" {
			return what, "off: only the calls a session needs."
		}
	}
	if value == "" {
		return what, unset
	}
	return what, value + "."
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
	menuBar := "off"
	if c.MenuBar {
		menuBar = "on"
	}
	colours := "standard"
	if c.ColorBlind {
		colours = "colour-blind"
	}
	themeName := c.Theme
	if themeName == "" {
		themeName = "match terminal"
	}
	barSearch := "as you type"
	if c.SearchTranscriptsOnKey {
		barSearch = "on ctrl+enter"
	}
	spaces := "hidden"
	if c.ShowWhitespace {
		spaces = "shown"
	}
	sortBy := c.SortBy
	if sortBy == "" {
		sortBy = "name"
	}
	enter := c.EnterOn
	if enter == "" {
		enter = "ask"
	}
	view := c.View
	if view == "" {
		view = "ask"
	}
	return []setting{
		{"Layout", view, []string{"split", "agent", "list", "ask"}, func(v string) {
			if v == "ask" {
				c.View = ""
				return
			}
			c.SetView(v)
		}},
		{"Group by", c.GroupBy, m.groupModes(), func(v string) { c.GroupBy = v }},
		{"Enter on an agent", enter, []string{"rename", "open", "ask"}, func(v string) {
			c.EnterOn = v
			if v == "ask" {
				c.EnterOn = ""
			}
		}},
		{"Sort rows by", sortBy, sortModes, func(v string) { c.SortBy = v }},
		{"Hibernate finished agents", hib, []string{"off", "15m", "30m", "60m"}, func(v string) {
			c.Hibernate.AfterMinutes = 0
			fmt.Sscanf(v, "%dm", &c.Hibernate.AfterMinutes)
		}},
		{"Notify when an agent needs you", notify, []string{"on", "off"}, func(v string) { c.Quiet = v == "off" }},
		{"Menu bar icon", menuBar, []string{"on", "off"}, func(v string) {
			c.MenuBar, c.MenuBarAsked = v == "on", true
			if !c.MenuBar {
				menubar.Stop()
			}
		}},
		{"Theme", themeName, []string{"match terminal", "dark", "light"}, func(v string) {
			c.Theme = v
			if v == "match terminal" {
				c.Theme = ""
			}
			m.applyColors()
		}},
		{"Colours", colours, []string{"standard", "colour-blind"}, func(v string) {
			c.ColorBlind = v == "colour-blind"
			m.colored = false
			m.applyColors()
		}},
		{"Spaces and tabs in diffs", spaces, []string{"hidden", "shown"}, func(v string) {
			c.ShowWhitespace = v == "shown"
			convo.SetShowWhitespace(c.ShowWhitespace)
		}},
		{"ctrl+k searches transcripts", barSearch, []string{"as you type", "on ctrl+enter"}, func(v string) {
			c.SearchTranscriptsOnKey = v == "on ctrl+enter"
		}},
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
		return len(m.accountRows())
	case tabAgents:
		return len(m.agentSettings()) + len(d.agents)
	case tabClaude:
		return len(m.claudeRows())
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
		m.setView(placeAgents)
		m.refresh()
		return nil
	case "up", "k":
		d.cursor = roundMove(d.cursor, -1, m.dialogLen())
		return nil
	case "down", "j":
		d.cursor = roundMove(d.cursor, 1, m.dialogLen())
		return nil
	}
	switch d.tab {
	case tabAccounts:
		return m.accountsKey(s)
	case tabAgents:
		return m.agentsKey(s)
	case tabClaude:
		return m.claudeKey(s)
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
			return m.startMenuBar()
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
	if s == "a" {
		m.ask("login name", "")
		return nil
	}
	if s == "s" {
		m.store.Config.StayOnAccount = !m.store.Config.StayOnAccount
		_ = m.store.SaveConfig()
		if m.store.Config.StayOnAccount {
			m.flash("agtop stays on this account, even when it's nearly out", false)
		} else {
			m.flash(fmt.Sprintf("agtop switches account at %.0f%% of 5h or 7d", state.SwitchAt), false)
		}
		return m.autoSwitch()
	}
	rows := m.accountRows()
	if d.cursor >= len(rows) {
		return nil
	}
	r := rows[d.cursor]
	switch {
	case r.login != nil:
		return m.loginKey(*r.login, s)
	case r.profile.Kind != "":
		return nil
	}
	row := r.folders[0]
	switch s {
	case "enter":
		if r.found {
			m.addAccount(row.Name, row.ConfigDir, false)
		}
	case "r":
		if !r.found {
			m.ask("rename "+row.Name, row.Name)
		}
	case "l":
		return tea.ExecProcess(actions.Login(row), func(err error) tea.Msg { return doneMsg{err: err, text: "signed in to " + row.Name} })
	case "d", "x":
		if row.IsDefault() || r.found {
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

// loginKey handles a key on one of the logins in Accounts.
func (m *Model) loginKey(lv fleet.LoginView, s string) tea.Cmd {
	switch s {
	case "enter":
		if lv.Current {
			m.flash("already on "+lv.Name, false)
			return nil
		}
		return m.switchLogin(lv.Login, "")
	case "r":
		m.ask("rename login "+lv.Name, lv.Name)
	case "l":
		return m.addLogin(lv.Name)
	case "d", "x":
		if lv.Current {
			m.flash("switch to another account before forgetting "+lv.Name, true)
			return nil
		}
		m.dialog.confirm = fmt.Sprintf("Forget %s (%s)? agtop drops its saved sign-in; sessions already on it keep going.", lv.Name, lv.Email)
		m.dialog.onYes = func() tea.Cmd {
			var keep []claude.Login
			for _, l := range m.store.Config.Logins {
				if l.ID != lv.ID {
					keep = append(keep, l)
				}
			}
			m.store.Config.Logins = keep
			_ = state.Vault().Forget(lv.ID)
			_ = m.store.SaveConfig()
			m.refresh()
			m.dialog.cursor = max(0, m.dialog.cursor-1)
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
	if d.tab == tabClaude {
		m.claudeAnswer(what, v)
		return nil
	}
	switch {
	case what == "login name":
		return m.addLogin(v)
	case strings.HasPrefix(what, "rename login "):
		old := strings.TrimPrefix(what, "rename login ")
		for i, l := range m.store.Config.Logins {
			if l.Name == old {
				m.store.Config.Logins[i].Name = v
			}
		}
		_ = m.store.SaveConfig()
		m.refresh()
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
		stay := fmt.Sprintf("switches Claude at %.0f%% to the account with the most room", state.SwitchAt)
		if m.store.Config.StayOnAccount {
			stay = "stays on this Claude account, even when it's nearly out"
		}
		out = append(out, faint("● in use: new sessions run on it · agtop "+stay), "")
		cols := []int{9, 16, 30, 19, 19, 9}
		head := faint(fit("AGENT", cols[0]) + fit("NAME", cols[1]) + fit("EMAIL", cols[2]) + fit("LIMITS", cols[3]+cols[4]) + right("RUNNING", cols[5]))
		out = append(out, "    "+head)
		rows := m.accountRows()
		for i, r := range rows {
			out = append(out, row(i, m.accountLine(r, cols)))
		}
		if d.cursor < len(rows) {
			r := rows[d.cursor]
			out = append(out, "", rule(r.name(), "", w))
			out = append(out, m.accountDetail(r, w)...)
			switch {
			case r.login != nil:
				out = append(out, "", keysFit(w, "enter", "switch to", "a", "add account", "r", "rename", "l", "sign in again", "d", "forget", "s", "stay/switch"))
			case r.found:
				out = append(out, "", keysFit(w, "enter", "add", "a", "add account", "l", "sign in", "s", "stay/switch"))
			case r.profile.Kind != "":
				out = append(out, "", keysFit(w, "a", "add Claude account", "s", "stay/switch"))
			default:
				out = append(out, "", keysFit(w, "a", "add account", "r", "rename", "l", "sign in", "d", "remove", "s", "stay/switch"))
			}
		}
	case tabAgents:
		out = append(out, dim("New sessions start with"))
		settings := m.agentSettings()
		for i, st := range settings {
			v := st.value
			if v == "" {
				v = "Claude Code default"
			}
			out = append(out, m.settingRow(i, st, v, 14, 18, w)...)
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
			out = append(out, m.settingRow(i, st, st.value, 32, 18, w)...)
		}
		out = append(out, m.about(w)...)
		out = append(out, "", keysFit(w, "←→", "change", "tab", "next page", "esc", "back to agents"))
	case tabClaude:
		out = append(out, m.claudeBody(w)...)
	}
	return out
}

// overlayBox draws body in a panel of width bw centred over base, dimming
// everything behind it.
func (m *Model) overlayBox(base string, body []string, bw int) string {
	lines := strings.Split(base, "\n")
	box := boxLines(body, bw)
	for y := range lines {
		lines[y] = faint(ansi.Strip(fit(lines[y], m.w)))
	}
	return strings.Join(pasteAt(lines, box, max(1, (len(lines)-len(box))/2), (m.w-bw)/2), "\n")
}

// boxLines is body in a rounded box bw wide, a blank line inside top and
// bottom.
func boxLines(body []string, bw int) []string { return edgedBox(body, bw, cDim) }

// edgedBox is boxLines with its edge in col.
func edgedBox(body []string, bw int, col string) []string {
	inner := bw - 4
	edge := func(s string) string { return paint(col, s) }
	box := []string{edge("╭" + strings.Repeat("─", bw-2) + "╮")}
	for _, l := range append([]string{""}, append(body, "")...) {
		box = append(box, edge("│")+panel(" "+fit(l, inner)+" ")+edge("│"))
	}
	return append(box, edge("╰"+strings.Repeat("─", bw-2)+"╯"))
}

// pasteAt lays box over lines with its top left corner at (left, top).
func pasteAt(lines, box []string, top, left int) []string {
	for i, b := range box {
		y := top + i
		if y < 0 || y >= len(lines) {
			continue
		}
		l := lines[y]
		lines[y] = ansi.Truncate(l, left, "") + reset + b + ansi.TruncateLeft(l, left+cellw.String(ansi.Strip(b)), "")
	}
	return lines
}

var panelBG string // applyColors sets this and every other ground

func panel(s string) string {
	return panelBG + strings.ReplaceAll(s, reset, reset+panelBG) + reset
}

// accountLine is one account's row in the table.
func (m *Model) accountLine(r acctRow, cols []int) string {
	views := map[string]fleet.AccountView{}
	for _, av := range m.snap.Accounts {
		views[av.ConfigDir] = av
	}
	mark := faint("○")
	agentName := "Claude"
	var email string
	var q usage.Quota
	live := 0
	switch {
	case r.login != nil:
		if r.login.Current {
			mark = paint(cOrange, "●")
		}
		email, q = r.login.Email, r.login.Quota
	case r.profile.Kind != "":
		mark = paint(cOrange, "●")
		agentName = kindName(r.profile.Kind)
		q = m.quotas[r.profile.Dir]
		email = q.Email
	case r.found:
		line := faint(fit("Claude", cols[0])) + paint(cSub, fit(r.name(), cols[1])) + faint(fit(tildify(r.folders[0].ConfigDir), cols[2]))
		return mark + " " + line + paint(cYellow, "found on disk") + dim(" · enter to add")
	default:
		av := views[r.folders[0].ConfigDir]
		email, q = av.Usage.Email, av.Quota
		if email == "" {
			email = "not signed in"
			if q.Problem == "" {
				q.Problem = "l signs in"
			}
		}
	}
	for _, a := range m.snap.Agents {
		if a.Live() && m.runsOn(a, r) {
			live++
		}
	}
	running := faint(right("·", cols[5]))
	if live > 0 {
		running = paint(cSub, right(fmt.Sprint(live), cols[5]))
	}
	line := faint(fit(agentName, cols[0])) + paint(cText, fit(r.name(), cols[1])) + dim(fit(email, cols[2])) +
		m.limits(r, q, cols[3], cols[4]) + running
	return mark + " " + line
}

// kindName is an agent's name, short enough for the table.
func kindName(k agent.Kind) string {
	if ad, ok := agent.Get(k); ok && len(ad.Name()) <= 9 {
		return ad.Name()
	}
	s := string(k)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runsOn is whether agent a runs on the account r: in one of its Claude
// folders, or with its agent.
func (m *Model) runsOn(a *fleet.Agent, r acctRow) bool {
	if r.profile.Kind != "" {
		return a.Kind == string(r.profile.Kind)
	}
	if a.Kind != "" && a.Kind != "claude" {
		return false
	}
	for _, f := range r.folders {
		if a.Acct.ConfigDir == f.ConfigDir {
			return true
		}
	}
	return false
}

// limits is an account's first two windows, each a labelled meter, or why
// there are none.
func (m *Model) limits(r acctRow, q usage.Quota, w1, w2 int) string {
	if len(q.Windows) == 0 {
		msg := faint("no reading")
		switch {
		case q.Problem != "":
			msg = paint(cYellow, q.Problem)
		case r.profile.Kind != "":
			ad, _ := agent.Get(r.profile.Kind)
			if _, ok := ad.(agent.QuotaSource); !ok {
				msg = faint("doesn't report its limits")
			} else if q.FetchedAt.IsZero() {
				msg = faint("fetching…")
			}
		case q.FetchedAt.IsZero():
			msg = faint("fetching…")
		}
		return fit(msg, w1+w2)
	}
	stale := time.Since(q.FetchedAt) > 3*claude.UsageEvery
	var out string
	for i, cw := range []int{w1, w2} {
		if i >= len(q.Windows) {
			out += fit("", cw)
			continue
		}
		win := q.Windows[i]
		pct := fmt.Sprintf("%3.0f%%", win.Percent)
		cell := faint(fit(win.Label, 3)) + bar(win.Percent) + " " + paint(cText, pct)
		if stale {
			cell = faint(fit(win.Label, 3) + strings.Repeat("▱", 10) + " " + pct)
		}
		out += fit(cell, cw)
	}
	return out
}

// accountDetail is everything known about one account, under the table.
func (m *Model) accountDetail(r acctRow, w int) []string {
	now := m.snap.At
	views := map[string]fleet.AccountView{}
	for _, av := range m.snap.Accounts {
		views[av.ConfigDir] = av
	}
	var out []string
	label := func(k string) string { return dim(fit(k, 10)) }
	var who []string
	seen := map[string]bool{}
	addWho := func(vs ...string) {
		for _, v := range vs {
			if v = strings.ReplaceAll(v, "_", " "); v != "" && !seen[v] {
				seen[v] = true
				who = append(who, v)
			}
		}
	}
	var q usage.Quota
	var u claude.Usage
	switch {
	case r.login != nil:
		u, q = r.login.Usage, r.login.Quota
		addWho(r.login.Email, u.Org, u.Role, u.Plan, u.Billing)
	case r.profile.Kind != "":
		q = m.quotas[r.profile.Dir]
		addWho(q.Email, q.Plan)
	case r.found:
		return []string{label("folder") + faint(tildify(r.folders[0].ConfigDir)+" · found on disk, not added yet: enter adds it")}
	default:
		av := views[r.folders[0].ConfigDir]
		u, q = av.Usage, av.Quota
		addWho(u.Email, u.Org, u.Role, u.Plan, u.Billing)
	}
	if u.Extra {
		who = append(who, "extra usage on")
	}
	if len(who) == 0 {
		switch {
		case r.profile.Kind != "":
			who = append(who, "who it's signed in as isn't known yet")
		default:
			who = append(who, "not signed in: press l to sign in")
		}
	}
	out = append(out, label("who")+paint(cText, strings.Join(who, " · ")))
	for _, win := range q.Windows {
		s := label(win.Label) + bar(win.Percent) + " " + paint(cText, fmt.Sprintf("%.0f%%", win.Percent))
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
		out = append(out, s)
	}
	if len(q.Windows) == 0 && q.Problem != "" {
		out = append(out, label("limits")+paint(cYellow, q.Problem))
	}
	// Sessions, and what they've spent, where they live.
	var live, total int
	var today, spend float64
	var top []*fleet.Agent
	for _, ag := range m.snap.Agents {
		if !m.runsOn(ag, r) {
			continue
		}
		total++
		if ag.Live() {
			live++
		}
		today += ag.Spend.Today
		spend += ag.Spend.Cost
		if ag.Spend.Today > 0 {
			top = append(top, ag)
		}
	}
	out = append(out, label("agents")+paint(cText, fmt.Sprintf("%d running · %d in total", live, total))+dim("  ·  today ")+paint(cText, money(today))+dim("  ·  all time ")+paint(cText, money(spend)))
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
	var where []string
	switch {
	case r.profile.Kind != "":
		where = append(where, tildify(r.profile.Dir))
	case len(r.folders) == 0:
		where = append(where, "kept by agtop · no folder is signed in as it now")
	default:
		for _, f := range r.folders {
			s := tildify(f.ConfigDir)
			if views[f.ConfigDir].Daemon {
				s += " (daemon running)"
			}
			where = append(where, s)
		}
	}
	out = append(out, label("folders")+faint(strings.Join(where, " · ")))
	if !q.FetchedAt.IsZero() {
		source := "read"
		switch q.Source {
		case usage.Fetched:
			source = "fetched"
		case usage.Live:
			source = "reported by a session"
		}
		out = append(out, label("usage")+faint(source+" at "+q.FetchedAt.Local().Format("15:04")))
	}
	return out
}

// settingRow is always one line, the highlighted one too, so moving the
// highlight never shifts the page; the About section explains it.
func (m *Model) settingRow(i int, st setting, shown string, labelW, valueW, w int) []string {
	_, now := settingHelp(st.label, st.value)
	short := now
	if _, rest, ok := strings.Cut(now, ": "); ok {
		short = rest
	}
	line := fit(st.label, labelW) + faint("‹ ") + paint(cText, fit(shown, valueW)) + faint(" › ")
	if room := w - cellw.String(line) - 6; room > 12 {
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
	case tabClaude:
		var what, now string
		title, what, now = m.claudeAbout()
		add(cSub, what)
		add(cText, now)
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
