package convo

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// applyNeutral folds in an event from any agent. The model was made for
// Claude Code and still speaks its shapes, so most events go in as
// headless.FromNeutral says Claude would have sent them.
func (s *Session) applyNeutral(ev event.Event, now time.Time) {
	switch e := ev.(type) {
	case event.CallUpdated:
		if st := s.byID[e.Call.ID]; st != nil {
			st.Tool, st.Input = headless.ClaudeTool(e.Call)
			st.Kind = e.Call.Kind
			s.touchStep(st)
		}
		return
	case event.Message:
		if text, ok := said(e); ok {
			// What you said, as the agent's history tells it.
			s.Apply(host.Sent{Text: text}, now)
			return
		}
		for _, h := range headless.FromNeutral(ev) {
			s.Apply(h, now)
		}
		// Each call keeps the kind its agent gave it: some have no Claude
		// tool (a delete, a move).
		for _, p := range e.Parts {
			if p.Call != nil {
				if st := s.byID[p.Call.ID]; st != nil {
					st.Kind = p.Call.Kind
				}
			}
		}
		return
	case event.Approval:
		s.ensureStep(e.Call, now)
	case event.Question:
		s.ensureStep(tool.Call{ID: headless.QuestionCall(e), Name: "AskUserQuestion", Kind: tool.Question}, now)
	case event.TurnEnd:
		if e.Reason == "interrupted" {
			s.interrupted(now)
			return
		}
	case event.Limited:
		s.Limit = "rejected"
		return
	case event.Context:
		s.Context = e.Tokens
		return
	case event.Plan:
		b, _ := json.Marshal(map[string]any{"todos": headless.ClaudeTodos(e.Todos)})
		s.tasksFromInput(&Step{Tool: "TodoWrite", Input: b})
		return
	}
	for _, h := range headless.FromNeutral(ev) {
		s.Apply(h, now)
	}
}

// ensureStep makes a step of a call an approval or question is about,
// when the agent asks before it says it's making the call.
func (s *Session) ensureStep(c tool.Call, now time.Time) {
	if c.ID == "" || s.byID[c.ID] != nil {
		return
	}
	name, input := headless.ClaudeTool(c)
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: c.ID, Name: name, Input: input}}}, now)
	if st := s.byID[c.ID]; st != nil {
		st.Kind = c.Kind
	}
}

// said is a message of yours: a user message of text alone.
func said(m event.Message) (string, bool) {
	if m.Role != "user" || len(m.Parts) == 0 {
		return "", false
	}
	var texts []string
	for _, p := range m.Parts {
		if p.Kind != event.Text {
			return "", false
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n\n"), true
}
