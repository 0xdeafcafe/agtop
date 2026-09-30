package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// shift+x (or shift+k) on a picked task, command or subagent stops it as
// x (or k) would, with a message: the box takes what to tell the agent,
// and enter stops it and sends that straight after, so the agent hears
// why rather than only that it was stopped.

// stopNote is a stop waiting on its message: the key it stands for and
// what was picked when it was pressed.
type stopNote struct {
	key, sel string
}

// stopNoteKey starts, sends or drops a stop with a message.
func (m *Model) stopNoteKey(c *hostConn, s string, empty bool) (tea.Cmd, bool) {
	if n := c.stopNote; n != nil {
		switch s {
		case "esc":
			c.stopNote, c.sel = nil, n.sel
			return nil, true
		case "enter":
			c.stopNote, c.sel = nil, n.sel
			stop, _ := m.stopPicked(c, n.key)
			c.sel = ""
			if len(strings.TrimSpace(string(c.input))) == 0 {
				return stop, true
			}
			return tea.Sequence(stop, m.sendPane(c, true)), true
		}
		return nil, false
	}
	key := map[string]string{"X": "x", "shift+x": "x", "K": "k", "shift+k": "k"}[s]
	if key == "" || !empty || !m.stoppable(c, key) {
		return nil, false
	}
	c.stopNote = &stopNote{key: key, sel: c.sel}
	c.sel = "" // the box has the keys
	return nil, true
}

// stoppable is whether x (or k) would stop something picked now.
func (m *Model) stoppable(c *hostConn, key string) bool {
	if key == "k" {
		return m.pickedShell(c) != ""
	}
	if m.pickedShell(c) != "" || m.pickedJob(c) != nil {
		return true
	}
	_, _, ok := m.pickedSub(c)
	return ok
}

// stopPicked is x (or k) on what's picked, as with nothing typed.
func (m *Model) stopPicked(c *hostConn, key string) (tea.Cmd, bool) {
	if cmd, used := m.jobKey(c, key, true); used {
		return cmd, true
	}
	if key == "x" {
		if sa, live, ok := m.pickedSub(c); ok {
			return m.stopSub(c, sa, live), true
		}
	}
	return nil, false
}
