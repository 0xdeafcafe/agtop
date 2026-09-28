package convo

import (
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Conversation is the session told for a hand-off to another agent
// (agent.Handoff): how it began, its steps in words, the files it changed,
// its last turns and its todo list.
func (s *Session) Conversation(from agent.Kind) agent.Conversation {
	c := agent.Conversation{From: from, Name: s.Info.Name, Cwd: s.Cwd}
	if c.Cwd == "" {
		c.Cwd = s.Info.Cwd
	}
	for _, t := range s.Turns {
		if c.First == "" && t.From == "" {
			c.First = t.Prompt
		}
		for _, it := range t.Items {
			if it.Kind != KStep || it.Step == nil {
				continue
			}
			d := claude.Doing(it.Step.Tool, it.Step.Input)
			if it.Step.Status == Failed {
				d += " (failed)"
			}
			if n := len(c.Done); d != "" && (n == 0 || c.Done[n-1] != d) {
				c.Done = append(c.Done, d)
			}
		}
	}
	for _, f := range s.Changes() {
		c.Changed = append(c.Changed, f.Path)
	}
	turns := s.Turns
	if len(turns) > 3 {
		turns = turns[len(turns)-3:]
	}
	for _, t := range turns {
		if t.Prompt != "" {
			c.Recent = append(c.Recent, agent.Line{Role: "user", Text: t.Prompt, At: t.Start})
		}
		if a := lastWords(t); a != "" {
			c.Recent = append(c.Recent, agent.Line{Role: "assistant", Text: a, At: t.End})
		}
	}
	for _, t := range s.Tasks {
		c.Todos = append(c.Todos, agent.Todo{Label: t.Subject, Done: t.Status == "completed", Started: t.Status == "in_progress"})
	}
	return c
}

// lastWords is the turn's answer, or what it last said when it has none
// yet: a turn a limit or a stop cut short.
func lastWords(t *Turn) string {
	if a := t.Answer(); a != "" {
		return a
	}
	for i := len(t.Items) - 1; i >= 0; i-- {
		if it := t.Items[i]; it.Kind == KText && it.Text != "" {
			return it.Text
		}
	}
	return ""
}
