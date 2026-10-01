package host

import (
	"fmt"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// These are deliberately conservative hard stops, not estimates. A stopped
// child keeps its conversation and can be resumed with a narrower next task.
const (
	subagentInputLimit = 8_000_000
	subagentToolLimit  = 40
)

type watchedTask struct {
	label   string
	kind    event.TaskKind
	stopped bool
}

type subagentWatchdog struct {
	tasks map[string]*watchedTask

	input        int64
	tools        map[string]struct{}
	messages     map[string]int64
	stoppedCause string
}

func (w *subagentWatchdog) startTask(e event.TaskStarted) {
	if w.tasks == nil {
		w.tasks = map[string]*watchedTask{}
	}
	w.tasks[e.ID] = &watchedTask{label: e.Label, kind: e.Kind}
}

func (w *subagentWatchdog) finishTask(id string) { delete(w.tasks, id) }

func (w *subagentWatchdog) taskReason(e event.TaskProgress) string {
	t := w.tasks[e.ID]
	if t == nil || t.kind != event.SubagentTask || t.stopped {
		return ""
	}
	if e.Tokens < subagentInputLimit && e.ToolUses < subagentToolLimit {
		return ""
	}
	t.stopped = true
	return watchdogReason(or(t.label, e.ID), int64(e.Tokens), e.ToolUses)
}

func (w *subagentWatchdog) resetTurn() {
	w.input = 0
	w.tools = map[string]struct{}{}
	w.messages = map[string]int64{}
	w.stoppedCause = ""
}

func (w *subagentWatchdog) observeMessage(m event.Message) string {
	if w.stoppedCause != "" || m.Role != "assistant" {
		return ""
	}
	if m.Tokens != nil {
		n := m.Tokens.Input + m.Tokens.CacheRead + m.Tokens.CacheWrite5m + m.Tokens.CacheWrite1h
		if m.ID == "" {
			w.input += n
		} else if before := w.messages[m.ID]; n > before {
			w.input += n - before
			w.messages[m.ID] = n
		}
	}
	for _, p := range m.Parts {
		if p.Kind != event.ToolCall || p.Call == nil {
			continue
		}
		id := p.Call.ID
		if id == "" {
			id = fmt.Sprintf("%s:%d", m.ID, len(w.tools))
		}
		w.tools[id] = struct{}{}
	}
	if w.input < subagentInputLimit && len(w.tools) < subagentToolLimit {
		return ""
	}
	w.stoppedCause = watchdogReason("this subagent", w.input, len(w.tools))
	return w.stoppedCause
}

func watchdogReason(label string, input int64, tools int) string {
	label = strings.TrimSpace(label)
	return fmt.Sprintf("Rush stopped %s after %.1fM input tokens and %d tool calls: it crossed the subagent safety ceiling (%.0fM input tokens or %d tool calls). If the work is legitimate, resume the same subagent with a narrower next task; its prior context is preserved.",
		label, float64(input)/1_000_000, tools, float64(subagentInputLimit)/1_000_000, subagentToolLimit)
}

// watchTaskProgress stops only the runaway task. The parent gets the reason as
// a guide, so it can decide whether to resume the retained child conversation.
// Called with s.mu held.
func (s *server) watchTaskProgress(conn agent.Conn, e event.TaskProgress) {
	reason := s.watchdog.taskReason(e)
	stopper, ok := conn.(agent.TaskStopper)
	if reason == "" || !ok {
		return
	}
	s.info.Detail = firstLine(reason)
	s.recordEvent(event.TaskUpdated{ID: e.ID, Err: reason})
	go func() {
		if err := stopper.StopTask(e.ID); err != nil {
			s.mu.Lock()
			if t := s.watchdog.tasks[e.ID]; t != nil {
				t.stopped = false
			}
			s.recordEvent(event.TaskUpdated{ID: e.ID, Err: "Rush's subagent watchdog could not stop it: " + err.Error()})
			s.mu.Unlock()
			return
		}
		_ = s.guide(reason, nil)
	}()
}
