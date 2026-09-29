package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A session another agent ran, as the host sends it, draws as Claude's do.
func TestNeutralSession(t *testing.T) {
	s := New()
	exit := 2
	s.Apply(event.Init{SessionID: "th-1", Model: "gpt-5", Cwd: "/w"}, at(0))
	s.Apply(host.Sent{Text: "fix the build"}, at(1))
	s.Apply(event.MessageStart{ID: "m1"}, at(2))
	s.Apply(event.Delta{Kind: event.Text, Text: "Looking"}, at(2))
	s.Apply(event.Message{Role: "assistant", ID: "m1", Parts: []event.Part{
		{Kind: event.Text, Text: "Looking at it."},
		{Kind: event.ToolCall, Call: &tool.Call{ID: "c1", Name: "shell", Kind: tool.Shell, Input: tool.Input{Command: "go build ./..."}}},
	}, Tokens: &usage.TokenUsage{Input: 10, Output: 5}}, at(3))
	s.Apply(event.Approval{ID: "a1", Call: tool.Call{ID: "c1", Kind: tool.Shell, Input: tool.Input{Command: "go build ./..."}},
		Options: []event.Option{{ID: "yes", Kind: event.AllowOnce}, {ID: "always", Kind: event.AllowAlways}, {ID: "no", Kind: event.RejectOnce}}}, at(4))
	if p := s.Pending(); len(p) != 1 || p[0].Approval.ID != "a1" || p[0].Tool != "Bash" || !p[0].Approval.Always {
		t.Fatalf("pending = %+v", p)
	}
	s.Apply(host.Answered{ID: "a1"}, at(5))
	s.Apply(event.Message{Role: "user", Parts: []event.Part{{Kind: event.ToolResult, Output: &tool.Output{CallID: "c1", Text: "undefined: x", IsError: true, Exit: &exit}}}}, at(6))
	s.Apply(event.Approval{ID: "a2", Call: tool.Call{ID: "c2", Kind: tool.Edit, Input: tool.Input{Path: "/w/main.go"}}}, at(7))
	if st := s.Step("c2"); st == nil || st.Tool != "Edit" || st.Approval == nil {
		t.Fatalf("an approval for a call not yet seen made no step: %+v", st)
	}
	s.Apply(event.ApprovalCancelled{ID: "a2"}, at(8))
	s.Apply(event.Plan{Todos: []tool.TodoItem{{Label: "build", Status: "in_progress"}}}, at(8))
	s.Apply(event.TurnEnd{Reason: "done", Cost: 0.02}, at(9))

	st := s.Step("c1")
	if st.Status != Failed || exitCode(st) != 2 {
		t.Errorf("c1 = %v, exit %d; want failed with 2", st.Status, exitCode(st))
	}
	if len(s.Tasks) != 1 || s.Tasks[0].Subject != "build" {
		t.Errorf("tasks = %+v", s.Tasks)
	}
	out := plain(s.Render(Options{Width: 110, Now: at(10)}))
	for _, want := range []string{"fix the build", "Looking at it.", "go build"} {
		if !strings.Contains(out, want) {
			t.Errorf("render is missing %q:\n%s", want, out)
		}
	}
}

func TestNeutralQuestionAndInterrupt(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "pick"}, at(0))
	s.Apply(event.Question{ID: "q1", Asks: []event.Ask{{Text: "Which?", Options: []event.Choice{{Label: "A"}, {Label: "B"}}}}}, at(1))
	p := s.Pending()
	if len(p) != 1 || p[0].Tool != "AskUserQuestion" || p[0].Approval.Question == nil {
		t.Fatalf("pending = %+v", p)
	}
	if q := p[0].Approval.Question; len(q.Asks) != 1 || len(q.Asks[0].Options) != 2 {
		t.Errorf("question = %+v", q)
	}
	s.Apply(event.TurnEnd{Reason: "interrupted"}, at(2))
	if tr := s.Turns[0]; tr.Live || !tr.Stopped {
		t.Errorf("turn = live %v, stopped %v; want stopped", tr.Live, tr.Stopped)
	}
}

// A call Claude Code has no tool for keeps its kind, and draws as one.
func TestNeutralKindsWithoutAClaudeTool(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "tidy"}, at(0))
	s.Apply(event.Message{Role: "assistant", Parts: []event.Part{
		{Kind: event.ToolCall, Call: &tool.Call{ID: "d1", Name: "delete", Kind: tool.Delete, Input: tool.Input{Path: "/w/old.go"}}},
	}}, at(1))
	st := s.Step("d1")
	if st == nil || st.kind() != tool.Delete || glyphFor(st) != "✎" {
		t.Fatalf("delete step = %+v", st)
	}
	s.Apply(event.TurnEnd{Reason: "done"}, at(2))
	if out := plain(s.Render(Options{Width: 100, Now: at(3)})); !strings.Contains(out, "old.go") {
		t.Errorf("the delete doesn't say what it deleted:\n%s", out)
	}
}

// A step keeps the call its agent made, with what Claude's input has no
// key for: a move's destination.
func TestNeutralStepKeepsItsCall(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "rename"}, at(0))
	s.Apply(event.Message{Role: "assistant", Parts: []event.Part{
		{Kind: event.ToolCall, Call: &tool.Call{ID: "m1", Name: "move", Kind: tool.Move, Input: tool.Input{Path: "/w/a.go", To: "/w/b.go"}}},
	}}, at(1))
	st := s.Step("m1")
	if st == nil {
		t.Fatal("no step for the move")
	}
	if c := st.Call(); c.Name != "move" || c.Kind != tool.Move || c.Input.To != "/w/b.go" || st.in().Path != "/w/a.go" {
		t.Fatalf("call = %+v: want the agent's own", c)
	}
}

// A history's prompts are your messages: each starts a turn.
func TestNeutralPromptsStartTurns(t *testing.T) {
	s := New()
	s.Apply(event.Message{Role: "user", Parts: []event.Part{{Kind: event.Text, Text: "fix it"}}}, at(0))
	s.Apply(event.Message{Role: "assistant", Parts: []event.Part{{Kind: event.Text, Text: "Fixed."}}}, at(1))
	s.Apply(event.TurnEnd{Reason: "done"}, at(2))
	if len(s.Turns) != 1 || s.Turns[0].Prompt != "fix it" {
		t.Fatalf("turns = %+v", s.Turns)
	}
}
