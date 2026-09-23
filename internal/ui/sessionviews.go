package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// --- queue view ---

// queueLines lists the messages waiting for the agent. A selected one can
// be edited, moved, merged, sent now or dropped; the whole queue can be
// held, and sent as one message or one per turn.
func (m *Model) queueLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	info := c.sess.Info
	var out []convo.Line
	line := func(text, ref string) { out = append(out, convo.Line{Text: fit(text, w), Ref: ref}) }
	how := "sends as one message when this turn ends"
	if info.QueueSeparate {
		how = "sends one message per turn"
	}
	if info.QueueHeld {
		how = paint(cYellow, "held") + dim(" · alt+h releases it")
	} else {
		how = dim(how)
	}
	line("  "+paint(cSub+bold, fmt.Sprintf("Queue  %d", len(info.Queue)))+"   "+how, "")
	line("  "+faint(strings.Repeat("─", max(0, w-4))), "")
	if len(info.Queue) == 0 {
		line("", "")
		line("    "+dim("Nothing queued. Messages you send while the agent works wait here."), "")
	}
	for i, q := range info.Queue {
		ref := fmt.Sprintf("q:%d", i)
		rows := wrap(oneLine(q), max(20, w-10))
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
	line("  "+keysFit(w-4, "alt+h", "hold or release", "alt+o", "one message or separately"), "")
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
}

// slashMatches is what the picker offers for the text typed so far.
func slashMatches(c *hostConn) []headless.Command {
	text := string(c.input)
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n") {
		return nil
	}
	q := strings.ToLower(strings.TrimPrefix(text, "/"))
	var out []headless.Command
	seen := map[string]bool{}
	for _, list := range [][]headless.Command{agtopCommands, c.sess.Commands} {
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

// slashLines draws the picker above the message box.
func (m *Model) slashLines(c *hostConn, w int) []string {
	cmds := slashMatches(c)
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
		mine := false
		for _, a := range agtopCommands {
			if a.Name == cmd.Name {
				mine = true
			}
		}
		name := paint(cBright+bold, fit("/"+cmd.Name, nameW+1))
		desc := dim(ansi.Truncate(oneLine(cmd.Description), max(10, w-nameW-14), "…"))
		tag := ""
		if mine {
			tag = paint(cOrange, " agtop")
		}
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
	out = append(out, onBg(bgChrome, "   "+dim(more+"↑↓ choose · tab completes · enter runs"), w))
	return out
}

// slashKey drives the picker while a command is being typed.
func (m *Model) slashKey(c *hostConn, s string) (tea.Cmd, bool) {
	cmds := slashMatches(c)
	if len(cmds) == 0 {
		return nil, false
	}
	switch s {
	case "up":
		c.slashSel = max(0, c.slashSel-1)
		return nil, true
	case "down":
		c.slashSel = min(len(cmds)-1, c.slashSel+1)
		return nil, true
	case "tab":
		c.input, c.back = []rune("/"+cmds[c.slashSel].Name+" "), 0
		return nil, true
	case "enter":
		c.input, c.back = []rune("/"+cmds[c.slashSel].Name), 0
		return m.sendPane(c, false), true
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
