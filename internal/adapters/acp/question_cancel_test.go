package acp

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"testing"
)

func TestQuestionCancellationReleasesHostWait(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "agent withdraws", true: "user interrupts"}[interrupt], func(t *testing.T) {
			s, f := start(t)
			nextOf[event.Init](t, s)
			if err := s.Send(agent.Input{Text: "go"}); err != nil {
				t.Fatal(err)
			}
			prompt := f.expect("session/prompt")
			f.request(41, "elicitation/create", map[string]any{"mode": "form", "message": "Form", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string", "title": "Choose"}}}})
			q := nextOf[event.Question](t, s)
			if interrupt {
				if err := s.Interrupt(); err != nil {
					t.Fatal(err)
				}
				f.expect("session/cancel")
			} else {
				s.withdraw([]byte("41"))
			}
			reply := f.recv()
			if string(reply.ID) != "41" {
				t.Fatalf("wrong cancelled request: %s", reply.ID)
			}
			if interrupt && string(reply.Result) != `{"action":"cancel"}` {
				t.Fatalf("question not cancelled: %s", reply.Result)
			}
			if ev := nextOf[event.ApprovalCancelled](t, s); ev.ID != q.ID {
				t.Fatalf("host cannot clear question: %+v", ev)
			}
			f.result(prompt.ID, map[string]any{"stopReason": "cancelled"})
			if ev := nextOf[event.TurnEnd](t, s); ev.Reason != "interrupted" {
				t.Fatal(ev)
			}
			if err := s.AnswerQuestion(q.ID, map[string][]string{"answer": {"late"}}); err == nil {
				t.Fatal("cancelled question still accepts answers")
			}
			if err := s.Send(agent.Input{Text: "continue"}); err != nil {
				t.Fatal(err)
			}
			next := f.expect("session/prompt")
			f.result(next.ID, map[string]any{"stopReason": "end_turn"})
			if ev := nextOf[event.TurnEnd](t, s); ev.Reason != "done" {
				t.Fatal(ev)
			}
		})
	}
}
