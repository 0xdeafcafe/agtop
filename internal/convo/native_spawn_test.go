package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/host"
)

// sideBySide renders an Agent-tool subagent and a shell-run agent that did
// the same work, opened or not, for comparing line by line.
func sideBySide(t *testing.T, open bool) (agentOut, shellOut string) {
	t.Helper()
	work := func(s *Session, parent string) {
		for i, f := range []string{"a.go", "b.go"} {
			id := parent + string(rune('a'+i))
			s.Apply(headless.Message{Role: "assistant", ParentToolUseID: parent, Blocks: []headless.Block{{Type: "tool_use", ID: id, Name: "Read", Input: raw(map[string]any{"file_path": "/work/" + f})}}}, at(2+i))
			s.Apply(headless.Message{Role: "user", ParentToolUseID: parent, Blocks: []headless.Block{{Type: "tool_result", ToolUseID: id, Text: "…"}}}, at(2+i))
		}
	}
	a := New()
	a.Info.Cwd = "/work"
	a.Apply(host.Sent{Text: "get a review"}, at(0))
	a.Apply(toolUse("x1", "Agent", map[string]any{"description": "review the diff", "prompt": "review the diff for races", "subagent_type": "Explore"}), at(1))
	work(a, "x1")
	a.Apply(toolResult("x1", "No races found.", false, nil), at(9))

	s := New()
	s.Info.Cwd = "/work"
	s.Apply(host.Sent{Text: "get a review"}, at(0))
	s.Apply(toolUse("x1", "Bash", map[string]any{"command": `run() { perl -e 'exec @ARGV' claude -p "$1"; }; run "$P"`, "description": "review the diff"}), at(1))
	s.Apply(toolResult("x1", "No races found.", false, nil), at(9))
	kid := New()
	kid.Apply(host.Sent{Text: "review the diff for races"}, at(1))
	work(kid, "")
	kid.Apply(say("No races found."), at(8))
	s.SetChildren(s.Step("x1"), []Child{{ID: "h1", Spawn: Spawn{Kind: "claude", Name: "Claude Code", Prompt: "review the diff for races"}, Sess: kid, Start: at(1)}})

	o := Options{Width: 100, Now: at(10), Open: map[string]bool{}}
	if open {
		o.Open["t1:s:x1"] = true
	}
	return plain(a.Render(o)), plain(s.Render(o))
}

// A shell-run agent draws as an Agent-tool subagent that did the same
// work, row for row, bar its own name: shut or opened.
func TestShellAgentReadsNative(t *testing.T) {
	for _, open := range []bool{false, true} {
		a, s := sideBySide(t, open)
		al, sl := strings.Split(a, "\n"), strings.Split(strings.ReplaceAll(s, "Claude Code", "Explore"), "\n")
		if len(al) != len(sl) {
			t.Fatalf("open %v: %d rows against %d\n%s\n%s", open, len(al), len(sl), a, s)
		}
		for i := range al {
			// The time is right-aligned, so the name's length moves it: compare words.
			if strings.Join(strings.Fields(al[i]), " ") != strings.Join(strings.Fields(sl[i]), " ") {
				t.Errorf("open %v, row %d:\n agent %q\n shell %q", open, i, al[i], sl[i])
			}
		}
	}
}

// backgrounded plays one review sent to the background both ways, and
// renders each while it runs and once its task has woken the session.
func backgrounded(t *testing.T) (running, woken [2]string) {
	t.Helper()
	dec := func(s *Session, l string, sec int) {
		ev, err := headless.Decode([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		s.Apply(ev, at(sec))
	}
	for k, s := range []*Session{New(), New()} {
		kid := New()
		kid.Apply(host.Sent{Text: "review the diff for races"}, at(1))
		for i, f := range []string{"a.go", "b.go"} {
			id := string(rune('a' + i))
			kid.Apply(toolUse(id, "Read", map[string]any{"file_path": "/work/" + f}), at(2+i))
			kid.Apply(toolResult(id, "…", false, nil), at(2+i))
		}
		s.Info.Cwd = "/work"
		s.Apply(host.Sent{Text: "get a review"}, at(0))
		typ := "local_agent"
		if k == 0 {
			s.Apply(toolUse("x1", "Agent", map[string]any{"description": "review the diff", "prompt": "review the diff for races", "subagent_type": "Explore", "run_in_background": true}), at(1))
			s.Apply(toolResult("x1", "Async agent launched successfully.\nagentId: a1", false, nil), at(1))
			for i, f := range []string{"a.go", "b.go"} {
				id := "x1" + string(rune('a'+i))
				s.Apply(headless.Message{Role: "assistant", ParentToolUseID: "x1", Blocks: []headless.Block{{Type: "tool_use", ID: id, Name: "Read", Input: raw(map[string]any{"file_path": "/work/" + f})}}}, at(2+i))
				s.Apply(headless.Message{Role: "user", ParentToolUseID: "x1", Blocks: []headless.Block{{Type: "tool_result", ToolUseID: id, Text: "…"}}}, at(2+i))
			}
		} else {
			typ = "local_bash"
			s.Apply(toolUse("x1", "Bash", map[string]any{"command": `run() { perl -e 'exec @ARGV' claude -p "$1"; }; run "$P" &`, "description": "review the diff", "run_in_background": true}), at(1))
			s.Apply(toolResult("x1", "Command running in background with ID: bg1", false, nil), at(1))
		}
		dec(s, `{"type":"system","subtype":"task_started","task_id":"bg1","tool_use_id":"x1","description":"review the diff","is_backgrounded":true,"task_type":"`+typ+`"}`, 1)
		s.Apply(say("It's reviewing."), at(3))
		s.Apply(headless.Result{Subtype: "success"}, at(3))
		sp := Spawn{Kind: "claude", Name: "Claude Code", Prompt: "review the diff for races"}
		if k == 1 {
			s.SetChildren(s.Step("x1"), []Child{{ID: "h1", Spawn: sp, Sess: kid, Live: true, Start: at(1)}})
		}
		running[k] = plain(s.Render(Options{Width: 100, Now: at(5)}))
		dec(s, `{"type":"system","subtype":"task_notification","task_id":"bg1","tool_use_id":"x1","status":"completed","output_file":"/tmp/tasks/bg1.output","summary":"review the diff"}`, 20)
		kid.Apply(say("No races found.\n\n- `a.go` is fine\n- `b.go` is fine"), at(19))
		if k == 1 {
			s.SetChildren(s.Step("x1"), []Child{{ID: "h1", Spawn: sp, Sess: kid, Start: at(1)}})
		}
		s.Apply(toolUse("r1", "Read", map[string]any{"file_path": "/tmp/tasks/bg1.output"}), at(21))
		s.Apply(toolResult("r1", "No races found.", false, nil), at(21))
		s.Apply(say("The review found no races."), at(22))
		s.Apply(headless.Result{Subtype: "success"}, at(22))
		woken[k] = plain(s.Render(Options{Width: 100, Now: at(30)}))
	}
	return running, woken
}

// The turn a backgrounded shell's agents woke is their reply: a card with
// what it said laid out, not a shell completing and a file read, which
// only verbose mode shows.
func TestAgentReplyCard(t *testing.T) {
	_, w := backgrounded(t)
	for _, want := range []string{"⇉ Claude Code  \"review the diff\" replied", "↩ review the diff for races", "│ • a.go is fine"} {
		if !strings.Contains(w[1], want) {
			t.Errorf("missing %q in\n%s", want, w[1])
		}
	}
	for _, not := range []string{"bg1.output", "Background command", "perl", "Command running"} {
		if strings.Contains(w[1], not) {
			t.Errorf("%q shows in\n%s", not, w[1])
		}
	}
	if !strings.Contains(w[0], "Background agent") || !strings.Contains(w[0], "bg1.output") {
		t.Errorf("an Agent-tool run's wake changed:\n%s", w[0])
	}
}
