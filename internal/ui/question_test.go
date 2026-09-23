package ui

import (
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
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
}

// A message that starts with y, a, n or a digit must never answer a card:
// only ↑ onto the card, or an alt chord, does.
func TestCardsNeedFocus(t *testing.T) {
	m := &Model{}
	c := &hostConn{sess: convo.New()}
	c.sess.Apply(host.Sent{Text: "go"}, time.Now())
	c.sess.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "b1", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)}}}, time.Now())
	c.sess.Apply(headless.PermissionRequest{ID: "r1", Tool: "Bash", ToolUseID: "b1"}, time.Now())
	for _, k := range []string{"y", "a", "n", "enter", "1"} {
		if _, used := m.cardKey(c, k, true); used {
			t.Errorf("%q answered the card without focus", k)
		}
	}
	if _, used := m.cardKey(c, "up", true); !used || !c.cardFocus {
		t.Fatal("↑ from an empty box should focus the card")
	}
	if _, used := m.cardKey(c, "esc", true); !used || c.cardFocus {
		t.Fatal("esc should hand the keys back to the box")
	}
	_ = tea.KeyPressMsg{}
}

// A transcript-backed Session that finishes loading after you've moved on
// has no host client; discarding it must not crash.
func TestStaleTranscriptOpen(t *testing.T) {
	m := &Model{hostOpening: "acct/other"}
	m.onHostOpen(hostOpenMsg{key: "acct/old", c: &hostConn{key: "acct/old"}})
	m.hostOpening = "acct/old"
	m.dropHost()
}

func TestZenQueue(t *testing.T) {
	now := time.Now()
	ag := func(key string, age time.Duration, blocked bool) *fleet.Agent {
		a := &fleet.Agent{Key: key, PID: 1}
		a.UpdatedAt = now.Add(-age)
		if blocked {
			a.State = "blocked"
		} else {
			a.State = "working"
		}
		return a
	}
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{
		ag("new", time.Minute, true), ag("busy", time.Hour, false), ag("old", 10*time.Minute, true),
	}}, zen: true}
	m.zenPick()
	if m.sel != "old" || !m.paneFocus {
		t.Fatalf("zen should start on the oldest waiting agent, got %q", m.sel)
	}
	m.zenSkip()
	if m.sel != "new" {
		t.Fatalf("ctrl+n should skip to the next, got %q", m.sel)
	}
	// Once answered, it moves on by itself.
	m.snap.Agents[0].State = "working"
	m.zenPick()
	if m.sel != "old" {
		t.Fatalf("an answered agent should give way, got %q", m.sel)
	}
}
