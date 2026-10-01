package convo

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A session told for a hand-off carries how it began, its steps in words,
// the files it changed, its last words and its open todos.
func TestConversation(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "fix the flaky test"}, at(0))
	s.Apply(toolUse("b1", "Bash", map[string]any{"command": "go test ./...", "description": "run the tests"}), at(1))
	s.Apply(toolResult("b1", "FAIL", true, nil), at(1))
	s.Apply(toolUse("e1", "Edit", map[string]any{"file_path": "/r/x_test.go", "old_string": "a", "new_string": "b"}), at(2))
	s.Apply(toolResult("e1", "ok", false, nil), at(2))
	s.Apply(toolUse("t1", "TodoWrite", map[string]any{"todos": []map[string]string{
		{"content": "fix it", "status": "completed", "activeForm": "fixing"},
		{"content": "run it again", "status": "in_progress", "activeForm": "running"},
	}}), at(3))
	s.Apply(toolResult("t1", "ok", false, nil), at(3))
	s.Apply(say("Fixed the race; running again."), at(4))
	s.Apply(headless.Result{Subtype: "success"}, at(4))

	c := s.Conversation("claude")
	if c.First != "fix the flaky test" || len(c.Steps) < 2 || len(c.Todos) != 2 {
		t.Fatalf("conversation = %+v", c)
	}
	if len(c.Recent) != 2 || c.Recent[1].Role != "assistant" || !strings.Contains(c.Recent[1].Text, "Fixed the race") {
		t.Errorf("recent = %+v", c.Recent)
	}
	if h := c.History; len(h) != 2 || h[0].Text != "fix the flaky test" ||
		!strings.Contains(h[1].Text, "→ run the tests (failed)") || !strings.HasSuffix(h[1].Text, "Fixed the race; running again.") {
		t.Errorf("history = %+v", h)
	}
	text := agent.Handoff(c).Text
	for _, want := range []string{"> fix the flaky test", "- run the tests (failed)", "Fixed the race", "- [in progress] run it again"} {
		if !strings.Contains(text, want) {
			t.Errorf("hand-off lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "fix it\n") {
		t.Errorf("a done todo is handed on:\n%s", text)
	}
}

// A whole hand-off keeps the latest of a long conversation, and opens on
// something you said.
func TestHistoryKeepsTheLatest(t *testing.T) {
	long := strings.Repeat("x", historyMax/2)
	turns := []*Turn{
		{Prompt: "first", Items: []*Item{{Kind: KText, Text: long}}},
		{Prompt: "second", Items: []*Item{{Kind: KText, Text: long}}},
		{Prompt: "third", Items: []*Item{{Kind: KText, Text: "done"}}},
	}
	h := history(turns)
	if len(h) != 4 || h[0].Text != "second" || h[3].Text != "done" {
		t.Errorf("history = %d lines, from %q", len(h), h[0].Text)
	}
}

func TestHistoryKeepsOversizedLatestMessages(t *testing.T) {
	for _, role := range []string{"user", "assistant"} {
		t.Run(role, func(t *testing.T) {
			large := "begin " + strings.Repeat("界", historyMax) + " last instruction"
			turn := &Turn{Prompt: "question", Items: []*Item{{Kind: KText, Text: "answer"}}}
			if role == "user" {
				turn.Prompt = large
			} else {
				turn.Items[0].Text = large
			}
			got := history([]*Turn{turn})
			if len(got) != 2 || got[0].Role != "user" {
				t.Fatalf("latest exchange lost: %d lines", len(got))
			}
			var n int
			for _, line := range got {
				n += len(line.Text)
				if !utf8.ValidString(line.Text) {
					t.Fatal("split UTF-8")
				}
				if line.Role == role && (!strings.HasPrefix(line.Text, "begin ") || !strings.HasSuffix(line.Text, " last instruction")) {
					t.Fatal("lost message boundaries")
				}
			}
			if n > historyMax {
				t.Fatalf("history grew to %d", n)
			}
		})
	}
}

func TestHistoryPreservesSteeringOrder(t *testing.T) {
	turn := &Turn{Prompt: "first", Items: []*Item{
		{Kind: KText, Text: "started"}, {Kind: KInterject, Text: "also this"},
		{Kind: KText, Text: "adjusted"}, {Kind: KInterject, Text: "and that"}, {Kind: KText, Text: "done"},
	}}
	got := history([]*Turn{turn})
	want := []string{"first", "started", "also this", "adjusted", "and that", "done"}
	if len(got) != len(want) {
		t.Fatalf("got %d messages", len(got))
	}
	for i, l := range got {
		if l.Text != want[i] {
			t.Fatalf("message %d = %q", i, l.Text)
		}
	}
	s := New()
	s.Turns = []*Turn{turn}
	recent := s.Conversation("codex").Recent
	if len(recent) != 6 || recent[4].Role != "user" || recent[4].Text != "and that" {
		t.Fatalf("summary lost steering: %+v", recent)
	}
}

func BenchmarkHistoryLongSession(b *testing.B) {
	turns := make([]*Turn, 3000)
	for i := range turns {
		turns[i] = &Turn{Prompt: "do the task", Items: []*Item{{Kind: KText, Text: strings.Repeat("an answer ", 1000)}}}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = history(turns)
	}
}

func TestConversationBoundsStepSummaryWithoutLosingChanges(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "finish the task"}, at(0))
	s.Apply(toolUse("old-edit", "Edit", map[string]any{"file_path": "/r/old.go", "old_string": "a", "new_string": "b"}), at(1))
	s.Apply(toolResult("old-edit", "ok", false, nil), at(2))
	for i := range 100 {
		id := fmt.Sprintf("run-%d", i)
		s.Apply(toolUse(id, "Bash", map[string]any{"command": id, "description": id}), at(i+3))
		s.Apply(toolResult(id, "result", i == 99, nil), at(i+3))
	}
	c := s.Conversation("claude")
	if len(c.Steps) != 64 || c.EarlierSteps != 37 {
		t.Fatalf("step summary: %d kept, %d omitted", len(c.Steps), c.EarlierSteps)
	}
	if c.Steps[0].Call.ID != "run-36" || c.Steps[63].Call.ID != "run-99" || !c.Steps[63].Failed {
		t.Fatal("latest calls, their order or failure status lost")
	}
	if len(c.Changed) != 1 || c.Changed[0] != "/r/old.go" {
		t.Fatalf("old changed file lost: %v", c.Changed)
	}
	text := agent.Handoff(c).Text
	if !strings.Contains(text, "37 earlier tool calls omitted") || !strings.Contains(text, "run-99 (failed)") {
		t.Fatalf("inaccurate summary: %s", text)
	}
	if len(c.History) < 2 || !strings.Contains(c.History[1].Text, "run-0") {
		t.Fatal("bounding step summary unexpectedly bounded full recent history")
	}
}

// The complete export used when switching an open session, including recent
// history, the step summary and every changed path.
func BenchmarkConversationExport(b *testing.B) {
	for _, n := range []int{300, 3000} {
		s := benchSession(n)
		b.Run(fmt.Sprintf("%dturns", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = s.Conversation("claude")
			}
		})
	}
}
