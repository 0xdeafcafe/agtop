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

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
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
	pairs := []string{"enter", "edit", "shift+↑↓", "move", "s", "send this now", "⌫", "drop", "h", hold, "m", "merge with next"}
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
		c.input, c.back, c.editQ, c.editWas = []rune(q.items[i]), 0, i+1, q.items[i]
		c.sel = ""
		var cmd tea.Cmd
		if c.editHeld = !q.held; c.editHeld {
			cmd = m.holdQueue(c, true)
		}
		return cmd, true
	case "shift+up", "shift+down", "alt+up", "alt+down":
		to := i - 1
		if strings.HasSuffix(s, "down") {
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
