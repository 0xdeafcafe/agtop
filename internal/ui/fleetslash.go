package ui

import (
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// agtop's own commands start with #, so / is always Claude's: in the
// Prompt it starts a session with one of Claude's commands or skills, in
// a Session it goes to the agent. A # command in the Prompt acts on the
// selected agent, in a Session on that Session's agent.

// fleetCommands are agtop's # commands, the ones command() runs. A hint in
// <> needs an argument; one in [] can go without.
var fleetCommands = []headless.Command{
	{Name: "done", Description: "move the agent to Done (alt+d); its idle process stops"},
	{Name: "stop", Description: "stop the agent"},
	{Name: "rm", Description: "delete the session, and its worktree when that's safe"},
	{Name: "kill", Description: "kill the agent and everything it started"},
	{Name: "restart", Description: "restart the agent's Claude Code on the account in use, resuming the conversation; text is sent first (#rs)", ArgumentHint: "[message]"},
	{Name: "clean", Description: "delete the agent's temp work; all does every finished agent", ArgumentHint: "[all]"},
	{Name: "cd", Description: "restart the agent in another folder", ArgumentHint: "<path>"},
	{Name: "add-dir", Description: "restart the agent with another folder added", ArgumentHint: "<path>"},
	{Name: "rename", Description: "rename the agent; empty resets it", ArgumentHint: "[name]"},
	{Name: "group", Description: "put the agent in a group; empty clears it", ArgumentHint: "[name]"},
	{Name: "pin", Description: "pin the agent in Claude Code's list, or unpin it"},
	{Name: "pr", Description: "open the agent's pull request"},
	{Name: "full", Description: "open a Claude Code agent full screen, in Claude Code"},
	{Name: "agtop", Description: "move the agent into agtop mode (a terminal one is copied, not stopped)"},
	{Name: "sort", Description: "sort agents by " + strings.Join(sortModes, ", "), ArgumentHint: "<by>"},
	{Name: "by", Description: "group agents by " + strings.Join(groupModes, ", "), ArgumentHint: "<group>"},
	{Name: "folder", Description: "choose the folder new sessions start in"},
	{Name: "account", Description: "switch to another account; alone opens Accounts", ArgumentHint: "[name]"},
	{Name: "hibernate", Description: "stop finished agents after this many idle minutes; 0 turns it off", ArgumentHint: "<minutes>"},
	{Name: "width", Description: "the list's share of the screen; alone goes back to agtop's", ArgumentHint: "[n%]"},
	{Name: "dock", Description: "how many lines the agent's card under the list shows", ArgumentHint: "<lines>"},
	{Name: "native", Description: "open Claude Code's own agents view"},
	{Name: "help", Description: "a short guide to agtop"},
	{Name: "tour", Description: "the tour of agtop again; off puts Getting started away", ArgumentHint: "[off]"},
	{Name: "quit", Description: "leave agtop"},
}

// fleetAliases are other names command() answers to.
var fleetAliases = map[string]string{"undone": "done", "delete": "rm", "move": "cd", "exit": "quit", "rs": "restart"}

// isHashCmd is whether text is a # command: # and a letter, so a Markdown
// heading (# Plan) or an issue (#123) is still a message.
func isHashCmd(text string) bool {
	r := []rune(text)
	return len(r) >= 2 && r[0] == '#' && unicode.IsLetter(r[1])
}

// typingHash is whether a # command is being typed, # alone included.
func typingHash(text string) bool { return text == "#" || isHashCmd(text) }

// isFleetCommand is whether name (without its prefix) is one of agtop's.
func isFleetCommand(name string) bool {
	if fleetAliases[name] != "" {
		return true
	}
	for _, c := range fleetCommands {
		if c.Name == name {
			return true
		}
	}
	return false
}

// fleetArgs are what a command offers once you've typed a space, and which
// of them is in use now.
func (m *Model) fleetArgs(name string) (opts []string, now string) {
	switch name {
	case "sort":
		return sortModes, m.store.Config.SortBy
	case "by":
		return groupModes, m.store.Config.GroupBy
	case "account":
		for _, a := range m.store.Config.AllAccounts() {
			opts = append(opts, a.Name)
		}
		return opts, m.store.Config.ActiveAccount().Name
	case "clean":
		return []string{"all"}, ""
	}
	return nil, ""
}

// filterCommands keeps the commands whose name contains q, those starting
// with it first. An argument hint leads the description.
func filterCommands(q string, lists ...[]headless.Command) []headless.Command {
	var out []headless.Command
	seen := map[string]bool{}
	for _, list := range lists {
		for _, cmd := range list {
			if seen[cmd.Name] || !strings.Contains(strings.ToLower(cmd.Name), q) {
				continue
			}
			seen[cmd.Name] = true
			if cmd.ArgumentHint != "" {
				cmd.Description = strings.TrimSpace(cmd.ArgumentHint + "  " + cmd.Description)
			}
			out = append(out, cmd)
		}
	}
	// A plugin's plugin:name starts with q when its name does.
	starts := func(name string) bool {
		name = strings.ToLower(name)
		_, short, _ := strings.Cut(name, ":")
		return strings.HasPrefix(name, q) || strings.HasPrefix(short, q)
	}
	sort.SliceStable(out, func(i, j int) bool { return starts(out[i].Name) && !starts(out[j].Name) })
	return out
}

// hashMatches is what the picker offers for a # command being typed: the
// commands containing what's typed, or after a space, the command's
// choices.
func (m *Model) hashMatches(in []rune, back int) []headless.Command {
	text := string(in)
	if back != 0 || !strings.HasPrefix(text, "#") || strings.ContainsAny(text, "\n") ||
		len(in) > 1 && !unicode.IsLetter(in[1]) {
		return nil
	}
	name, q, hasArg := strings.Cut(text[1:], " ")
	if !hasArg {
		return filterCommands(strings.ToLower(name), fleetCommands)
	}
	opts, now := m.fleetArgs(name)
	if strings.Contains(q, " ") {
		return nil
	}
	var out []headless.Command
	for _, o := range opts {
		if !strings.HasPrefix(strings.ToLower(o), strings.ToLower(q)) {
			continue
		}
		d := ""
		if o == now {
			d = "now"
		}
		out = append(out, headless.Command{Name: name + " " + o, Description: d})
	}
	return out
}

// newSessionCommands are Claude's commands and skills on disk for the
// account and folder a new session would start in.
func (m *Model) newSessionCommands() []claude.Command {
	acct := m.store.Config.ActiveAccount()
	return claude.Commands(firstNonEmpty(acct.ConfigDir, claude.DefaultAccount().ConfigDir), m.startDir())
}

// promptPicker is what the Prompt's picker offers, and the prefix its
// commands take: # for agtop's, / for a new session's.
func (m *Model) promptPicker() ([]headless.Command, string) {
	if m.inKind != inPrompt || m.sessionFocused() || !m.acceptsText() {
		return nil, ""
	}
	if cmds := m.hashMatches(m.input, m.back); len(cmds) > 0 {
		return cmds, "#"
	}
	text := string(m.input)
	if m.back != 0 || !strings.HasPrefix(text, "/") || strings.ContainsAny(text[1:], " \n/") {
		return nil, ""
	}
	var list []headless.Command
	for _, f := range m.newSessionCommands() {
		list = append(list, headless.Command{Name: f.Name, Description: f.Description, ArgumentHint: f.ArgumentHint})
	}
	return filterCommands(strings.ToLower(text[1:]), list), "/"
}

// fleetSlashLines draws the Prompt's picker above its box.
func (m *Model) fleetSlashLines(w int) []string {
	cmds, lead := m.promptPicker()
	if len(cmds) == 0 {
		return nil
	}
	m.slashSel = max(0, min(m.slashSel, len(cmds)-1))
	if lead == "#" {
		return pickerRows(cmds, m.slashSel, w, "#", func(string) string { return "" }, "↑↓ · tab completes · enter runs")
	}
	skills := map[string]bool{}
	for _, f := range m.newSessionCommands() {
		skills[f.Name] = f.Skill
	}
	tag := func(name string) string {
		if skills[name] {
			return paint(cBlue, " skill")
		}
		return ""
	}
	return pickerRows(cmds, m.slashSel, w, "/", tag, "↑↓ · tab completes · enter starts it")
}

// fleetSlashKey drives the Prompt's picker while a command is being typed.
func (m *Model) fleetSlashKey(s string) (tea.Cmd, bool) {
	cmds, lead := m.promptPicker()
	if len(cmds) == 0 {
		return nil, false
	}
	m.slashSel = max(0, min(m.slashSel, len(cmds)-1))
	pick := cmds[m.slashSel]
	switch s {
	case "up", "down":
		m.slashSel = pickerMove(m.slashSel, len(cmds), s)
	case "tab":
		m.input, m.back, m.slashSel = completed(lead, pick, true), 0, 0
	case "enter":
		m.input, m.back, m.slashSel = completed(lead, pick, false), 0, 0
		if !needsArg(pick) {
			return m.submit(), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// paneHashKey drives the # picker in a Session's box: its commands act on
// that Session's agent.
func (m *Model) paneHashKey(c *hostConn, s string) (tea.Cmd, bool) {
	cmds := m.hashMatches(c.input, c.back)
	if len(cmds) == 0 {
		return nil, false
	}
	c.slashSel = max(0, min(c.slashSel, len(cmds)-1))
	pick := cmds[c.slashSel]
	switch s {
	case "up", "down":
		c.slashSel = pickerMove(c.slashSel, len(cmds), s)
	case "tab":
		c.input, c.back, c.slashSel = completed("#", pick, true), 0, 0
	case "enter":
		c.input, c.back, c.slashSel = completed("#", pick, false), 0, 0
		if !needsArg(pick) {
			text := string(c.input)
			c.input = c.input[:0]
			return m.command(m.agentByKey(c.key), text), true
		}
	default:
		return nil, false
	}
	return nil, true
}

func pickerMove(sel, n int, s string) int {
	if s == "up" {
		return roundMove(sel, -1, n)
	}
	return roundMove(sel, 1, n)
}

// roundMove is a menu's cursor moved d rows: ↑ on the first row goes to
// the last, ↓ on the last to the first.
func roundMove(cur, d, n int) int {
	if n <= 0 {
		return 0
	}
	return ((cur+d)%n + n) % n
}

// needsArg is whether a picked command can't run without an argument.
func needsArg(c headless.Command) bool { return strings.HasPrefix(c.ArgumentHint, "<") }

// completed is the box's text once a command is picked: with a space to
// type its argument after when it takes one, or always with tab.
func completed(lead string, c headless.Command, tab bool) []rune {
	out := []rune(lead + c.Name)
	if needsArg(c) || tab && c.ArgumentHint != "" {
		out = append(out, ' ')
	}
	return out
}

// legacyCommand runs a Prompt command typed the old way, /stop for #stop,
// when Claude has no command of that name, and says what it's called now.
func (m *Model) legacyCommand(text string) (tea.Cmd, bool) {
	f := strings.Fields(text)
	name := strings.ToLower(strings.TrimPrefix(f[0], "/"))
	if !isFleetCommand(name) {
		return nil, false
	}
	for _, c := range m.newSessionCommands() {
		if c.Name == name {
			return nil, false
		}
	}
	at := m.statusAt
	cmd := m.command(m.selected(), "#"+strings.TrimPrefix(text, "/"))
	if m.statusAt == at {
		m.flash("agtop's commands start with # now: #"+name+" · / starts a session with one of Claude's", false)
	}
	return cmd, true
}
