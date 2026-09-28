package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
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
