package codex

import (
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// Codex's commands run in terminals that can outlive their call: one
// still running when the model reads on, or when its turn ends, has gone
// to the background, where it stays until its item completes, as Claude
// Code's backgrounded Bash commands do. Rush can stop one.

// shell is a command whose item hasn't completed: its terminal, and
// whether it runs beside the turn yet.
type shell struct {
	process      string
	bg, stopping bool
}

// started keeps a command item until it completes.
func (c *Conn) started(it threadItem) {
	if it.Type != "commandExecution" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shells == nil {
		c.shells = map[string]*shell{}
	}
	c.shells[it.ID] = &shell{process: it.ProcessID}
}

// detach is the commands still running when the model reads on, or the
// turn ends: their calls returned, and they run beside the turn now.
func (c *Conn) detach() []event.Event {
	var out []event.Event
	c.mu.Lock()
	for id, s := range c.shells {
		call, ok := c.calls[id]
		if s.bg || !ok {
			continue
		}
		s.bg, call.Input.Background = true, true
		c.calls[id] = call
		out = append(out, event.CallUpdated{Call: call},
			event.TaskStarted{ID: id, CallID: id, Kind: event.ShellTask, Label: call.Input.Command, Background: true})
	}
	c.mu.Unlock()
	if out == nil {
		return nil
	}
	return append(out, c.background())
}

// finished is what a command item completing means: a background shell's
// task is done.
func (c *Conn) finished(it threadItem) []event.Event {
	c.mu.Lock()
	s := c.shells[it.ID]
	delete(c.shells, it.ID)
	c.mu.Unlock()
	if s == nil || !s.bg {
		return nil
	}
	status := "completed"
	switch {
	case s.stopping:
		status = "stopped"
	case it.Status == "failed" || it.Status == "declined" || it.ExitCode != nil && *it.ExitCode != 0:
		status = "failed"
	}
	return []event.Event{event.TaskDone{ID: it.ID, CallID: it.ID, Status: status}, c.background()}
}

// shellOf is the background shell id names (its item, or its terminal),
// marked as being stopped. Called with mu held.
func (c *Conn) shellOf(id string) (thread, process string, ok bool) {
	for item, s := range c.shells {
		if s.bg && s.process != "" && (item == id || s.process == id) {
			s.stopping = true
			return c.thread, s.process, true
		}
	}
	return "", "", false
}
