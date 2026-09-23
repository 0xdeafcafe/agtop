package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// --- queue view ---

// queueLines lists the messages waiting for the agent. A selected one can
// be edited, moved, merged, sent now or dropped; the whole queue can be
// held, and sent as one message or one per turn.
func (m *Model) queueLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	q := m.queueOf(c)
	var out []convo.Line
	line := func(text, ref string) { out = append(out, convo.Line{Text: fit(text, w), Ref: ref}) }
	how := "sends as one message when this turn ends"
	switch {
	case q.local:
		how = "sends as one message once idle, or within 15s while it works"
	case q.separate:
		how = "sends one message per turn"
	}
	if q.held {
		how = paint(cYellow, "held") + dim(" · alt+h releases it")
	} else {
		how = dim(how)
	}
	line("  "+paint(cSub+bold, fmt.Sprintf("Queue  %d", len(q.items)))+"   "+how, "")
	line("  "+faint(strings.Repeat("─", max(0, w-4))), "")
	if len(q.items) == 0 {
		line("", "")
		line("    "+dim("Nothing queued. Messages you send while the agent works wait here."), "")
	}
	for i, item := range q.items {
		ref := fmt.Sprintf("q:%d", i)
		rows := wrap(shortImages(oneLine(item)), max(20, w-10))
		for j, r := range rows {
			if j == 3 {
				line("         "+dim("…"), ref)
				break
			}
			lead := "  " + dim(fmt.Sprintf("%2d", i+1)) + "  "
			if j > 0 {
				lead = "      "
			}
			text := lead + paint(cText, r)
			if ref == o.Selected {
				bar := faint("▍")
				if o.Focused {
					bar = paint(cOrange, "▍")
				}
				text = selBG + strings.ReplaceAll(bar+fit(text, w-1)[1:], reset, reset+selBG) + reset
			}
			line(text, ref)
		}
	}
	line("", "")
	hint := keysFit(w-4, "↑↓", "pick", "enter", "edit", "shift+↑↓", "move", "alt+m", "merge with next", "ctrl+s", "send now", "ctrl+x", "drop")
	line("  "+hint, "")
	if q.local {
		line("  "+keysFit(w-4, "alt+h", "hold or release"), "")
	} else {
		line("  "+keysFit(w-4, "alt+h", "hold or release", "alt+o", "one message or separately"), "")
	}
	return out
}

// queueKey acts on the queue view. It reports whether it used the key.
func (m *Model) queueKey(c *hostConn, s string) (tea.Cmd, bool) {
	info := c.sess.Info
	switch s {
	case "alt+h":
		on := !info.QueueHeld
		return hostCmd(func() error { return c.client.HoldQueue(on) }), true
	case "alt+o":
		on := !info.QueueSeparate
		return hostCmd(func() error { return c.client.QueueSeparately(on) }), true
	}
	var i int
	if _, err := fmt.Sscanf(c.sel, "q:%d", &i); err != nil || i >= len(info.Queue) {
		return nil, false
	}
	was := info.Queue[i]
	switch s {
	case "enter":
		// Edit it in the box; enter there saves it back in place.
		c.input, c.back, c.editQ, c.editWas = []rune(info.Queue[i]), 0, i+1, info.Queue[i]
		c.sel = ""
		return nil, true
	case "shift+up", "shift+down":
		to := i - 1
		if s == "shift+down" {
			to = i + 1
		}
		if to < 0 || to >= len(info.Queue) {
			return nil, true
		}
		c.sel = fmt.Sprintf("q:%d", to)
		return hostCmd(func() error { return c.client.MoveQueued(i, was, to) }), true
	case "alt+m":
		return hostCmd(func() error { return c.client.MergeQueued(i, was) }), true
	case "ctrl+s":
		c.sel = ""
		return hostCmd(func() error { return c.client.SendQueued(i, was) }), true
	case "ctrl+x", "delete":
		c.sel = ""
		return hostCmd(func() error { return c.client.RemoveQueued(i, was) }), true
	}
	return nil, false
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
var agtopCommands = []headless.Command{
	{Name: "clear", Description: "start a fresh session in the same folder (this one stays in the list)"},
	{Name: "model", Description: "switch model for the next turn: /model opus, sonnet, haiku, fable", ArgumentHint: "<model>"},
	{Name: "effort", Description: "change effort (applies from the next start): low, medium, high, xhigh, max", ArgumentHint: "<level>"},
	{Name: "agtop", Description: "move this Claude Code session into agtop mode (a terminal one is copied, not stopped)"},
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
		lists = append([][]headless.Command{agtopCommands}, lists...)
	}
	var out []headless.Command
	seen := map[string]bool{}
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
	sort.SliceStable(out, func(i, j int) bool {
		pi := strings.HasPrefix(strings.ToLower(out[i].Name), q)
		pj := strings.HasPrefix(strings.ToLower(out[j].Name), q)
		return pi && !pj
	})
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
	start := max(0, c.slashSel-5)
	end := min(len(cmds), start+6)
	nameW := 0
	for _, cmd := range cmds[start:end] {
		nameW = max(nameW, len([]rune(cmd.Name))+1)
	}
	nameW = min(nameW, 28)
	var out []string
	for i := start; i < end; i++ {
		cmd := cmds[i]
		tag := ""
		switch {
		case slices.ContainsFunc(agtopCommands, func(a headless.Command) bool { return a.Name == cmd.Name }):
			tag = paint(cOrange, " agtop")
		case c.skills[cmd.Name]:
			tag = paint(cBlue, " skill")
		}
		name := paint(cBright+bold, fit("/"+cmd.Name, nameW+1))
		desc := dim(ansi.Truncate(oneLine(cmd.Description), max(10, w-nameW-16), "…"))
		row := "   " + name + "  " + desc + tag
		if i == c.slashSel {
			out = append(out, onBg(selBG, paint(cOrange, " ▸ ")+strings.TrimPrefix(row, "   "), w))
		} else {
			out = append(out, onBg(bgChrome, row, w))
		}
	}
	more := ""
	if len(cmds) > end-start {
		more = fmt.Sprintf("%d of %d · ", c.slashSel+1, len(cmds))
	}
	how := "↑↓ choose · tab completes · enter runs"
	if st, _, _, _ := slashWord(c); st > 0 {
		how = "↑↓ choose · tab or enter completes"
	}
	out = append(out, onBg(bgChrome, "   "+dim(more+how), w))
	return out
}

// slashKey drives the picker while a command is being typed.
func (m *Model) slashKey(c *hostConn, s string) (tea.Cmd, bool) {
	if args := argMatches(c); len(args) > 0 {
		c.slashSel = max(0, min(c.slashSel, len(args)-1))
		switch s {
		case "up":
			c.slashSel = max(0, c.slashSel-1)
		case "down":
			c.slashSel = min(len(args)-1, c.slashSel+1)
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
		c.slashSel = max(0, c.slashSel-1)
		return nil, true
	case "down":
		c.slashSel = min(len(cmds)-1, c.slashSel+1)
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
	return nil, false
}
