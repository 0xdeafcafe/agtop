package ui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

func askReq() *headless.PermissionRequest {
	in := map[string]any{"questions": []map[string]any{
		{"question": "Which rules first?", "header": "Lint", "options": []map[string]any{{"label": "no-floating-promises"}, {"label": "explicit return types"}}},
		{"question": "Which packages?", "multiSelect": true, "options": []map[string]any{{"label": "mcp"}, {"label": "skills"}, {"label": "web"}}},
	}}
	b, _ := json.Marshal(in)
	return &headless.PermissionRequest{ID: "q1", Tool: "AskUserQuestion", Input: b}
}

func TestAnswerQuestions(t *testing.T) {
	m := &Model{}
	c := &hostConn{}
	req := askReq()
	// A digit answers a single-choice question and moves to the next.
	if _, used := m.questionKey(c, req, "1", true); !used || c.qIdx != 1 {
		t.Fatalf("first answer: used=%v idx=%d", used, c.qIdx)
	}
	// Multi-select: digits toggle, enter confirms and replies.
	m.questionKey(c, req, "1", true)
	m.questionKey(c, req, "3", true)
	m.questionKey(c, req, "3", true)
	m.questionKey(c, req, "2", true)
	cmd, used := m.questionKey(c, req, "enter", true)
	if !used || cmd == nil {
		t.Fatal("confirming the last question should reply")
	}
	want := map[string]string{"Which rules first?": "no-floating-promises", "Which packages?": "mcp, skills"}
	for k, v := range want {
		if c.qAnswer[k] != v {
			t.Errorf("%q: got %q want %q", k, c.qAnswer[k], v)
		}
	}
	// Typed text answers in your own words.
	c2 := &hostConn{input: []rune("both, but start with promises")}
	m.questionKey(c2, req, "enter", false)
	if c2.qAnswer["Which rules first?"] != "both, but start with promises" || len(c2.input) != 0 {
		t.Errorf("typed answer: %v", c2.qAnswer)
	}
	// Letters still type while a question is up.
	if _, used := m.questionKey(c2, req, "a", true); used {
		t.Error("a letter shouldn't be taken by the question card")
	}
	_ = tea.KeyPressMsg{}
}
