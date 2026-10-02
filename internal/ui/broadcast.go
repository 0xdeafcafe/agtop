package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

// #broadcast sends one message to several agents. Named in the command
// (#broadcast all … or #broadcast @a @b …) it goes at once; without a
// message, rush waits while you click agents in the list to pick them,
// then the Prompt's next message goes to every one picked. Esc gives up.

// broadcastAll are the agents "all" means: the ones a message can reach
// that are still at work, not finished or past.
func (m *Model) broadcastAll() []*fleet.Agent {
	var out []*fleet.Agent
	for _, a := range m.order {
		if mentionable(a) && !a.Past && !a.Done && !strings.HasPrefix(a.Key, roomKeyPrefix) {
			out = append(out, a)
		}
	}
	return out
}

// broadcastCommand runs #broadcast arg.
func (m *Model) broadcastCommand(arg string) tea.Cmd {
	var to []*fleet.Agent
	words := strings.Fields(arg)
	n := 0
	for ; n < len(words); n++ {
		w := words[n]
		if strings.EqualFold(w, "all") {
			to = append(to, m.broadcastAll()...)
			continue
		}
		tag, ok := strings.CutPrefix(w, "@")
		if !ok {
			break
		}
		found := m.mentionsIn("# @" + tag)
		if len(found) == 0 {
			m.flash("no agent called @"+tag, true)
			return nil
		}
		to = append(to, found...)
	}
	msg := strings.Join(words[n:], " ")
	if msg != "" && len(to) > 0 {
		return m.broadcastTo(to, msg)
	}
	m.broadcast = map[string]bool{}
	for _, a := range to {
		m.broadcast[a.Key] = true
	}
	if msg != "" {
		m.input = []rune(msg)
	}
	m.flash("broadcast: click agents to pick them, type the message, enter sends · esc stops", false)
	return nil
}

// broadcastPick picks or unpicks the agent at key, when it can be sent to.
func (m *Model) broadcastPick(key string) {
	i := slices.IndexFunc(m.order, func(a *fleet.Agent) bool { return a.Key == key })
	if i < 0 || !mentionable(m.order[i]) || strings.HasPrefix(key, roomKeyPrefix) {
		return
	}
	if m.broadcast[key] {
		delete(m.broadcast, key)
	} else {
		m.broadcast[key] = true
	}
	m.flash(fmt.Sprintf("broadcast to %d · enter sends · esc stops", len(m.broadcast)), false)
}

// sendBroadcast sends text to the picked agents and leaves picking.
func (m *Model) sendBroadcast(text string) tea.Cmd {
	var to []*fleet.Agent
	for _, a := range m.order {
		if m.broadcast[a.Key] {
			to = append(to, a)
		}
	}
	if len(to) == 0 {
		m.flash("pick an agent first: click its name in the list", true)
		return nil
	}
	return m.broadcastTo(to, text)
}

func (m *Model) broadcastTo(to []*fleet.Agent, text string) tea.Cmd {
	m.broadcast = nil
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, a := range to {
		if seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		cmds = append(cmds, m.replyTo(a, text, text))
	}
	if len(seen) == 0 {
		m.flash("no agents at work to send to", true)
		return nil
	}
	m.flash(fmt.Sprintf("sent to %d agents", len(seen)), false)
	return tea.Batch(cmds...)
}
