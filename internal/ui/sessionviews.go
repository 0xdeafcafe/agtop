package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// --- queue view ---

// The queue's keys never need alt, which a Mac's option key doesn't send
// unless the terminal is told to. ↑ from the empty box picks the last
// queued message, in the dock or here; a picked message takes plain keys
// (anything else goes back to typing), and ctrl+s always sends everything
// waiting, now.

// queueLines lists the messages waiting for the agent, each as you wrote
// it, line breaks kept; a picked one shows whole.
func (m *Model) queueLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	q := m.queueOf(c)
	var out []convo.Line
	line := func(text, ref string) { out = append(out, convo.Line{Text: fit(text, w), Ref: ref}) }
	line("  "+paint(cSub+bold, fmt.Sprintf("Queue  %d", len(q.items)))+"   "+queueHow(q), "")
	line("  "+faint(strings.Repeat("─", max(0, w-4))), "")
	if len(q.items) == 0 {
		line("", "")
		line("    "+dim("Nothing queued. Messages you send while the agent works wait here."), "")
		return out
	}
	i, picked := queueSel(c, len(q.items))
	for j, item := range q.items {
		ref := fmt.Sprintf("q:%d", j)
		sel := picked && j == i
		rows := queueRows(item, max(20, w-12))
		limit := 4
		if sel {
			limit = 30
		}
		if len(rows) > limit {
			more := len(rows) - limit + 1
			rows = append(rows[:limit-1], dim(fmt.Sprintf("… %d more lines", more)))
		}
		if j > 0 {
			line("", "")
		}
		for k, r := range rows {
			lead := "   " + paint(cSub+bold, fmt.Sprintf("%2d", j+1)) + "  "
			if k > 0 {
				lead = "       "
			}
			text := lead + paint(cText, r)
			if k == 0 {
				text = spread(text, dim(queueSize(item))+"  ", w-1)
			}
			if sel {
				text = picked1(text, w, o.Focused)
			}
			line(text, ref)
		}
	}
	return out
}

// queueHow says when the queue goes.
func queueHow(q queued) string {
	how := "goes as one message when this turn ends"
	switch {
	case q.local:
		how = "goes as one message once idle, or within 15s while it works"
	case q.separate:
		how = "goes one message per turn"
	}
	if q.held {
		return paint(cYellow, "held") + dim(" · h on a picked message lets it go")
	}
	return dim(how)
}

// queueHint is the keys for a picked queued message.
func queueHint(q queued, w int) string {
	hold := "hold"
	if q.held {
		hold = "let go"
	}
	pairs := []string{"enter", "edit", "shift+↑↓", "merge up/down", "[ ]", "move", "s", "send this now", "⌫", "drop", "h", hold}
	if !q.local {
		how := "one per turn"
		if q.separate {
			how = "all together"
		}
		pairs = append(pairs, "o", how)
	}
	return keysFit(w, append(pairs, "esc", "done")...)
}

// picked1 draws a row as picked.
func picked1(text string, w int, focused bool) string {
	bar := faint("▍")
	if focused {
		bar = paint(cOrange, "▍")
	}
	return selBG + strings.ReplaceAll(bar+fit(text, w-1)[1:], reset, reset+selBG) + reset
}

// queueRows is a queued message wrapped to w, its line breaks kept and
// blank lines between paragraphs squeezed to one.
func queueRows(item string, w int) []string {
	var rows []string
	blank := false
	for _, l := range strings.Split(strings.TrimSpace(shortImages(item)), "\n") {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			if !blank {
				rows = append(rows, "")
			}
			blank = true
			continue
		}
		blank = false
		rows = append(rows, wrap(l, w)...)
	}
	return rows
}

// queueSize says how long a queued message is, when that isn't obvious.
func queueSize(item string) string {
	if n := strings.Count(strings.TrimSpace(item), "\n") + 1; n > 1 {
		return fmt.Sprintf("%d lines", n)
	}
	return ""
}

// queueSel is the queued message picked, in the dock or the queue view.
func queueSel(c *hostConn, n int) (int, bool) {
	var i int
	if _, err := fmt.Sscanf(c.sel, "q:%d", &i); err != nil || i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

// queueKey acts on a picked queued message. It reports whether it used
// the key.
func (m *Model) queueKey(c *hostConn, s string) (tea.Cmd, bool) {
	q := m.queueOf(c)
	i, ok := queueSel(c, len(q.items))
	if !ok {
		return nil, false
	}
	switch s {
	case "enter", "e":
		// Edit it in the box; enter there saves it back in place. The
		// queue holds meanwhile, so it doesn't go half edited.
		c.pastes = pastes{}
		c.input, c.back, c.editQ, c.editWas = c.pastes.unfold(q.items[i]), 0, i+1, q.items[i]
		c.sel = ""
		var cmd tea.Cmd
		if c.editHeld = !q.held; c.editHeld {
			cmd = m.holdQueue(c, true)
		}
		return cmd, true
	case "shift+up":
		// Into the one above: it comes first, so the order holds.
		if i == 0 {
			m.flash("nothing before it to merge with", true)
			return nil, true
		}
		c.sel, c.selMoved = fmt.Sprintf("q:%d", i-1), true
		return m.queueEdit(c, "merge", i-1, 0), true
	case "shift+down":
		return m.queueEdit(c, "merge", i, 0), true
	case "[", "]", "alt+up", "alt+down":
		to := i - 1
		if s == "]" || s == "alt+down" {
			to = i + 1
		}
		if to < 0 || to >= len(q.items) {
			return nil, true
		}
		c.sel, c.selMoved = fmt.Sprintf("q:%d", to), true
		return m.queueEdit(c, "move", i, to), true
	case "m", "alt+m":
		return m.queueEdit(c, "merge", i, 0), true
	case "s":
		return m.queueEdit(c, "send", i, 0), true
	case "backspace", "delete", "ctrl+x":
		return m.queueEdit(c, "drop", i, 0), true
	case "h", "alt+h":
		c.editHeld = false // yours now, not the edit's
		return m.holdQueue(c, !q.held), true
	case "o", "alt+o":
		if q.local {
			return nil, true
		}
		on := !q.separate
		c.sess.Info.QueueSeparate = on
		return hostCmd(func() error { return c.client.QueueSeparately(on) }), true
	}
	return nil, false
}

// queueEdit changes the queue here at once, so the view and the next key
// see it, and tells the host of an agtop session, naming the message by
// its place and text as you saw it.
func (m *Model) queueEdit(c *hostConn, op string, i, to int) tea.Cmd {
	items := slices.Clone(m.queueOf(c).items)
	was := items[i]
	switch op {
	case "move":
		items = slices.Insert(slices.Delete(items, i, i+1), to, was)
	case "merge":
		if i+1 >= len(items) {
			m.flash("nothing after it to merge with", true)
			return nil
		}
		items[i] += "\n\n" + items[i+1]
		items = slices.Delete(items, i+1, i+2)
	case "send", "drop":
		items = slices.Delete(items, i, i+1)
		// The pick stays where it was, on the next message.
		if len(items) == 0 {
			c.sel = ""
		} else {
			c.sel = fmt.Sprintf("q:%d", min(i, len(items)-1))
		}
	}
	if c.client == nil {
		q := m.localQ[c.key]
		q.items = items
		if op == "send" {
			if a := m.agentByKey(c.key); a != nil {
				return reply(a, was)
			}
		}
		return nil
	}
	c.sess.Info.Queue = items
	cl := c.client
	return hostCmd(func() error {
		switch op {
		case "move":
			return cl.MoveQueued(i, was, to)
		case "merge":
			return cl.MergeQueued(i, was)
		case "send":
			return cl.SendQueued(i, was)
		}
		return cl.RemoveQueued(i, was)
	})
}

// holdQueue holds the queue or lets it go.
func (m *Model) holdQueue(c *hostConn, on bool) tea.Cmd {
	if c.client == nil {
		q := m.localQueueOf(c.key)
		q.held, q.fails, q.retry = on, 0, time.Time{}
		return nil
	}
	c.sess.Info.QueueHeld = on
	cl := c.client
	return hostCmd(func() error { return cl.HoldQueue(on) })
}

// sendQueueNow sends everything waiting straight away, and extra (what's
// in the box) after it, as one message: whether the agent is working,
// waiting on you or the queue is held, and in the order you wrote them.
func (m *Model) sendQueueNow(c *hostConn, extra string) tea.Cmd {
	queued := m.queueOf(c).items
	items := slices.Clone(queued)
	if extra != "" {
		items = append(items, extra)
	}
	if len(items) == 0 {
		m.flash("nothing queued", false)
		return nil
	}
	if c.client == nil {
		a := m.agentByKey(c.key)
		if !canQueue(a) {
			return nil
		}
		q := m.localQueueOf(c.key)
		q.items, q.fails, q.retry = items, 0, time.Time{}
		return m.sendLocal(c.key, a, q)
	}
	n := len(queued)
	c.sess.Info.Queue = nil
	cl := c.client
	return hostCmd(func() error {
		if n == 0 {
			return cl.SendNow(extra)
		}
		// Folded into the first, what's in the box last, then sent.
		was := items[0]
		for k, next := range items[1:] {
			var err error
			if k+1 < n {
				err = cl.MergeQueued(0, was)
			} else {
				err = cl.EditQueued(0, was, was+"\n\n"+next)
			}
			if err != nil {
				return err
			}
			was += "\n\n" + next
		}
		return cl.SendQueued(0, was)
	})
}

// endQueueEdit lets the queue go again if editing held it.
func (m *Model) endQueueEdit(c *hostConn) tea.Cmd {
	c.editQ = 0
	if !c.editHeld {
		return nil
	}
	c.editHeld = false
	return m.holdQueue(c, false)
}

// --- tasks view ---

func (m *Model) taskLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	var out []convo.Line
	line := func(text string) { out = append(out, convo.Line{Text: fit(text, w)}) }
	_, done, total := c.sess.Current()
	line("  " + paint(cSub+bold, "Tasks") + "  " + dim(fmt.Sprintf("%d of %d done", done, total)))
	line("  " + faint(strings.Repeat("─", max(0, w-4))))
	if total == 0 {
		line("")
		line("    " + dim("No task list yet. It appears when the agent plans its work."))
		return out
	}
	group := func(title, status, mark string) {
		var ts []convo.Task
		for _, t := range c.sess.Tasks {
			if t.Status == status {
				ts = append(ts, t)
			}
		}
		if len(ts) == 0 {
			return
		}
		line("")
		line("  " + paint(cSub+bold, title) + "  " + dim(fmt.Sprint(len(ts))))
		for _, t := range ts {
			label := t.Subject
			col := cSub
			switch status {
			case "in_progress":
				label, col = firstNonEmpty(t.Active, t.Subject), cText+bold
			case "completed":
				col = cDim
			}
			for j, r := range wrap(oneLine(label), max(20, w-10)) {
				lead := "    " + mark + " "
				if j > 0 {
					lead = "      "
				}
				line(lead + paint(col, r))
			}
		}
	}
	group("Now", "in_progress", paint(cOrange, "■"))
	group("Next", "pending", dim("☐"))
	group("Done", "completed", paint(cGreen, "✓"))
	return out
}

// --- slash commands ---

// agtopCommands are handled by agtop itself rather than sent to Claude.
// They keep Claude Code's / names; agtop's other commands take # (see
// fleetCommands).
var agtopCommands = []headless.Command{
	{Name: "clear", Description: "start a fresh session in the same folder (this one stays in the list)"},
	{Name: "fork", Description: "carry on in a copy of this conversation, as a new agent (this one stays as it is)", ArgumentHint: "[name]"},
	{Name: "rewind", Description: "go back to before one of your messages and try again; the path you leave is kept as a branch"},
	{Name: "model", Description: "switch model for the next turn: /model opus, sonnet, haiku, fable", ArgumentHint: "<model>"},
	{Name: "effort", Description: "change effort (applies from the next start): low, medium, high, xhigh, max", ArgumentHint: "<level>"},
}

// claudeScreens are Claude Code's own screens, which headless Claude Code
// can't show ("isn't available in this environment"). agtop hands the
// terminal to Claude Code on that screen, and comes back when you leave it.
// Given arguments, /mcp and /config go to Claude as usual.
var claudeScreens = []headless.Command{
	{Name: "skills", Description: "the skills and / commands Claude can use: search, use, edit"},
	{Name: "plugin", Description: "installed plugins on/off, discover and install, marketplaces"},
	{Name: "mcp", Description: "MCP servers: connect, sign in, tools"},
	{Name: "hooks", Description: "the hooks that run around tools, prompts and sessions"},
	{Name: "permissions", Description: "allow and deny rules for tools"},
	{Name: "memory", Description: "memory, CLAUDE.md and the other files Claude reads (the memory view)"},
	{Name: "config", Description: "Claude Code's settings (Settings › Claude)"},
	{Name: "status", Description: "version, account, model and connections"},
	{Name: "statusline", Description: "build status lines: this header, agtop's top bar, and Claude Code's"},
	{Name: "privacy-settings", Description: "privacy settings"},
	{Name: "install-github-app", Description: "set up Claude on GitHub Actions for a repo"},
	{Name: "release-notes", Description: "what's new in Claude Code"},
	{Name: "feedback", Description: "send feedback about Claude Code"},
}

// agtopScreens are the ones agtop draws itself (agtopScreen).
var agtopScreens = map[string]bool{"plugin": true, "skills": true, "memory": true, "config": true, "statusline": true, "permissions": true, "hooks": true}

// screenAliases are other names Claude Code takes for the same screens.
var screenAliases = map[string]string{"plugins": "plugin", "bug": "feedback", "settings": "config"}

// claudeScreen is the Claude Code screen a command opens, if it's one.
func claudeScreen(name string) (string, bool) {
	if n, ok := screenAliases[name]; ok {
		name = n
	}
	ok := slices.ContainsFunc(claudeScreens, func(c headless.Command) bool { return c.Name == name })
	return name, ok
}

// slashWord finds the /word being typed at the cursor, at the start of the
// message or after a space. A word with a second slash is a path.
func slashWord(c *hostConn) (start, end int, q string, ok bool) {
	in, pos := c.input, len(c.input)-c.back
	start = pos
	for start > 0 && !unicode.IsSpace(in[start-1]) {
		start--
	}
	if start >= len(in) || in[start] != '/' || start == pos {
		return 0, 0, "", false
	}
	end = pos
	for end < len(in) && !unicode.IsSpace(in[end]) {
		end++
	}
	q = string(in[start+1 : pos])
	if strings.Contains(q, "/") {
		return 0, 0, "", false
	}
	return start, end, strings.ToLower(q), true
}

// slashMatches is what the picker offers for the /word at the cursor:
// agtop's own commands (at the start of a message only), the session's,
// then the custom commands and skills found on disk.
func slashMatches(c *hostConn) []headless.Command {
	start, _, q, ok := slashWord(c)
	if !ok {
		return nil
	}
	lists := [][]headless.Command{c.sess.Commands, c.local}
	if start == 0 {
		lists = append([][]headless.Command{agtopCommands, claudeScreens}, lists...)
	}
	var out []headless.Command
	seen := map[string]bool{}
	// Another name for one of Claude Code's screens (/plugins, /bug) finds
	// it first.
	if name, ok := screenAliases[q]; ok && start == 0 {
		if i := slices.IndexFunc(claudeScreens, func(c headless.Command) bool { return c.Name == name }); i >= 0 {
			seen[name] = true
			out = append(out, claudeScreens[i])
		}
	}
	for _, list := range lists {
		for _, cmd := range list {
			if seen[cmd.Name] {
				continue
			}
			if strings.Contains(strings.ToLower(cmd.Name), q) {
				seen[cmd.Name] = true
				out = append(out, cmd)
			}
		}
	}
	// What you typed exactly, then names starting with it, then the rest.
	rank := func(c headless.Command) int {
		n := strings.ToLower(c.Name)
		switch {
		case n == q || screenAliases[q] == c.Name:
			return 0
		case strings.HasPrefix(n, q):
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// loadLocal fills in the commands and skills on disk for the agent's
// account and folder (read at most every 30 seconds).
func (m *Model) loadLocal(c *hostConn) {
	a := m.agentByKey(c.key)
	if a == nil {
		return
	}
	cwd := firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	found := claude.Commands(firstNonEmpty(a.Acct.ConfigDir, claude.DefaultAccount().ConfigDir), cwd)
	c.local, c.skills = c.local[:0], map[string]bool{}
	for _, f := range found {
		c.local = append(c.local, headless.Command{Name: f.Name, Description: f.Description, ArgumentHint: f.ArgumentHint})
		if f.Skill {
			c.skills[f.Name] = true
		}
	}
}

// argChoices are what /model and /effort offer once you've typed a space.
var argChoices = map[string][]headless.Command{
	"model": {
		{Name: "opus", Description: "most capable"},
		{Name: "opus[1m]", Description: "Opus with a 1M-token context"},
		{Name: "sonnet", Description: "fast and capable"},
		{Name: "haiku", Description: "fastest and cheapest"},
		{Name: "fable", Description: "Fable"},
		{Name: "default", Description: "the account's default"},
	},
	"effort": {
		{Name: "low"}, {Name: "medium"}, {Name: "high"}, {Name: "xhigh"}, {Name: "max"},
	},
}

// argMatches is the picker for a command's argument: "/model so" offers
// the models starting with so, the current one marked.
func argMatches(c *hostConn) []headless.Command {
	text := string(c.input)
	if c.back != 0 || !strings.HasPrefix(text, "/") || strings.ContainsAny(text, "\n") {
		return nil
	}
	name, q, ok := strings.Cut(text[1:], " ")
	opts := argChoices[name]
	if !ok || opts == nil || strings.Contains(q, " ") {
		return nil
	}
	now := c.sess.Info.Model
	if name == "effort" {
		now = c.sess.Info.Effort
	}
	var out []headless.Command
	for _, o := range opts {
		if !strings.HasPrefix(o.Name, strings.ToLower(q)) {
			continue
		}
		d := o.Description
		if now != "" && (now == o.Name || name == "model" && strings.Contains(now, strings.TrimSuffix(o.Name, "[1m]"))) {
			d = strings.TrimPrefix(d+" · now", " · ")
		}
		out = append(out, headless.Command{Name: name + " " + o.Name, Description: d})
	}
	return out
}

// slashLines draws the picker above the message box.
func (m *Model) slashLines(c *hostConn, w int) []string {
	if cmds := m.hashMatches(c.input, c.back); len(cmds) > 0 {
		c.slashSel = max(0, min(c.slashSel, len(cmds)-1))
		return pickerRows(cmds, c.slashSel, w, "#", func(string) string { return "" }, "agtop's, for this agent · ↑↓ · tab completes · enter runs")
	}
	cmds := argMatches(c)
	if cmds == nil {
		if _, _, _, ok := slashWord(c); !ok {
			return nil
		}
		m.loadLocal(c)
		cmds = slashMatches(c)
	}
	if len(cmds) == 0 {
		return nil
	}
	c.slashSel = max(0, min(c.slashSel, len(cmds)-1))
	st, _, _, _ := slashWord(c)
	tag := func(name string) string {
		switch {
		case slices.ContainsFunc(agtopCommands, func(a headless.Command) bool { return a.Name == name }):
			return paint(cOrange, " agtop")
		case st == 0 && agtopScreens[name]:
			return paint(cOrange, " agtop")
		case st == 0 && slices.ContainsFunc(claudeScreens, func(a headless.Command) bool { return a.Name == name }):
			return paint(cSub, " claude code ↗")
		case c.skills[name]:
			return paint(cBlue, " skill")
		}
		return ""
	}
	how := "↑↓ choose · tab completes · enter runs"
	if st > 0 {
		how = "↑↓ choose · tab or enter completes"
	}
	return pickerRows(cmds, c.slashSel, w, "/", tag, how)
}

// pickerRows draws a command picker: up to six commands around the
// selected one, then a row saying how to use it.
func pickerRows(cmds []headless.Command, sel, w int, lead string, tag func(string) string, how string) []string {
	start := max(0, sel-5)
	end := min(len(cmds), start+6)
	nameW := 0
	for _, cmd := range cmds[start:end] {
		nameW = max(nameW, len([]rune(cmd.Name))+1)
	}
	nameW = min(nameW, 28)
	var out []string
	for i := start; i < end; i++ {
		cmd := cmds[i]
		name := paint(cBright+bold, fit(lead+cmd.Name, nameW+1))
		t := tag(cmd.Name)
		desc := dim(ansi.Truncate(oneLine(cmd.Description), max(10, w-nameW-7-ansi.StringWidth(t)), "…"))
		row := "   " + name + "  " + desc + t
		if i == sel {
			out = append(out, onBg(selBG, paint(cOrange, " ▸ ")+strings.TrimPrefix(row, "   "), w))
		} else {
			out = append(out, onBg(bgChrome, row, w))
		}
	}
	more := ""
	if len(cmds) > end-start {
		more = fmt.Sprintf("%d of %d · ", sel+1, len(cmds))
	}
	return append(out, onBg(bgChrome, "   "+dim(more+how), w))
}

// slashKey drives the picker while a command is being typed.
func (m *Model) slashKey(c *hostConn, s string) (tea.Cmd, bool) {
	if cmd, used := m.paneHashKey(c, s); used {
		return cmd, true
	}
	if args := argMatches(c); len(args) > 0 {
		c.slashSel = max(0, min(c.slashSel, len(args)-1))
		switch s {
		case "up":
			c.slashSel = roundMove(c.slashSel, -1, len(args))
		case "down":
			c.slashSel = roundMove(c.slashSel, 1, len(args))
		case "tab":
			c.input, c.back = []rune("/"+args[c.slashSel].Name), 0
		case "enter":
			c.input, c.back = []rune("/"+args[c.slashSel].Name), 0
			return m.sendPane(c, false), true
		default:
			return nil, false
		}
		return nil, true
	}
	cmds := slashMatches(c)
	if len(cmds) == 0 {
		return nil, false
	}
	start, end, _, _ := slashWord(c)
	complete := func(tail string) {
		rest := c.input[end:]
		if len(rest) > 0 && unicode.IsSpace(rest[0]) {
			tail = ""
		}
		name := []rune("/" + cmds[c.slashSel].Name + tail)
		c.input = append(append(append([]rune{}, c.input[:start]...), name...), rest...)
		c.back = len(rest)
	}
	switch s {
	case "up":
		c.slashSel = roundMove(c.slashSel, -1, len(cmds))
		return nil, true
	case "down":
		c.slashSel = roundMove(c.slashSel, 1, len(cmds))
		return nil, true
	case "tab":
		complete(" ")
		return nil, true
	case "enter":
		// A command that is the whole message runs; one mid-message is
		// completed, and enter again sends the message.
		if start == 0 && strings.TrimSpace(string(c.input[end:])) == "" {
			c.input, c.back = []rune("/"+cmds[c.slashSel].Name), 0
			return m.sendPane(c, false), true
		}
		complete(" ")
		return nil, true
	}
	return nil, false
}

// runAgtopCommand handles the commands agtop answers itself. It reports
// whether text was one of them.
func (m *Model) runAgtopCommand(c *hostConn, text string) (tea.Cmd, bool) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	arg = strings.TrimSpace(arg)
	a := m.agentByKey(c.key)
	switch name {
	case "done":
		// Putting it away closes its conversation rather than showing the
		// next agent's; on a narrow screen the list comes back. Zen goes
		// on to the next agent that needs you.
		if a == nil || a.Done || m.zen {
			return m.markDone(a), true
		}
		cmd := m.markDone(a)
		if m.confirm != nil && m.confirm.onYes != nil {
			yes := m.confirm.onYes
			m.confirm.onYes = func() tea.Cmd { m.leavePane(); return yes() }
			return cmd, true
		}
		m.leavePane()
		return cmd, true
	case "clean":
		m.askClean(a)
		return nil, true
	case "agtop":
		if a == nil {
			return nil, true
		}
		return m.moveToAgtop(a), true
	case "clear":
		if a == nil {
			return nil, true
		}
		return m.startHosted("", a.Cwd), true
	case "fork", "branch":
		if a == nil {
			return nil, true
		}
		m.openFork(c, a, arg)
		return nil, true
	case "rewind", "checkpoint", "undo":
		if a == nil {
			return nil, true
		}
		return m.openRewind(c, a), true
	case "model":
		if c.client == nil {
			m.flash("/model works in agtop-mode sessions · /agtop moves this one over", true)
			return nil, true
		}
		m.flash("model: "+firstNonEmpty(arg, "default")+" from the next turn", false)
		return hostCmd(func() error { return c.client.SetModel(arg) }), true
	case "effort":
		if c.client == nil {
			m.flash("/effort works in agtop-mode sessions · /agtop moves this one over", true)
			return nil, true
		}
		m.flash("effort: "+firstNonEmpty(arg, "default")+" from the next start", false)
		return hostCmd(func() error { return c.client.SetEffort(arg) }), true
	}
	if screen, ok := claudeScreen(name); ok && (arg == "" || screen != "mcp" && screen != "config") && a != nil {
		if cmd, ok := m.agtopScreen(c, a, screen); ok {
			return cmd, true
		}
		return m.openScreen(c, a, screen), true
	}
	return nil, false
}

// agtopScreen is agtop's own take on one of Claude Code's screens, where it
// has one; the rest open Claude Code's.
func (m *Model) agtopScreen(c *hostConn, a *fleet.Agent, screen string) (tea.Cmd, bool) {
	switch screen {
	case "plugin":
		return m.openPlugins(c, a), true
	case "memory":
		if m.showView(c, "memory") {
			return nil, true
		}
	case "config":
		m.openDialog(tabClaude)
		return nil, true
	case "statusline":
		m.openStatusLine(c, a)
		return nil, true
	case "skills":
		m.openSkills(c, a)
		return nil, true
	case "permissions":
		m.openPermissions(c, a)
		return nil, true
	case "hooks":
		return m.openHooks(c, a), true
	}
	return nil, false
}

// showView switches the Session to one of its views by name.
func (m *Model) showView(c *hostConn, name string) bool {
	for i, v := range m.views(c) {
		if v == name {
			c.view = i
			return true
		}
	}
	return false
}

// openScreen hands the terminal to Claude Code on one of its own screens,
// for the agent's account and folder. Leaving it (esc, or ctrl+c twice)
// brings agtop back; a hosted session reloads its plugins after /plugin.
func (m *Model) openScreen(c *hostConn, a *fleet.Agent, screen string) tea.Cmd {
	key := c.key
	hint := "\033[2m  agtop · Claude Code's /" + screen + " · when you're done: esc, then ctrl+c twice to come back\033[0m"
	cmd := actions.Screen(a.Acct, firstNonEmpty(c.sess.Info.Cwd, a.Cwd), screen, hint)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return screenDoneMsg{key: key, screen: screen, err: err} })
}

type screenDoneMsg struct {
	key, screen string
	err         error
}

// onScreenDone is agtop coming back from a Claude Code screen.
func (m *Model) onScreenDone(msg screenDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.flash("Claude Code's /"+msg.screen+": "+msg.err.Error(), true)
		return nil
	}
	c := m.host
	if msg.screen != "plugin" || c == nil || c.key != msg.key || c.client == nil {
		return nil
	}
	// A running session only sees newly enabled plugins once it reloads
	// them; mid-turn, that's left to you.
	if c.sess.Info.State == "working" {
		m.flash("plugins change for "+c.sess.Info.Name+" after /reload-plugins, once this turn ends", false)
		return nil
	}
	cl := c.client
	return hostCmd(func() error { return cl.Send("/reload-plugins") })
}
