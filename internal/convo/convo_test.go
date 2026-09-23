package convo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

var t0 = time.Date(2026, 9, 23, 22, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func toolUse(id, name string, in any) headless.Message {
	return headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: id, Name: name, Input: raw(in)}}}
}

func toolResult(id, text string, isErr bool, structured any) headless.Message {
	m := headless.Message{Role: "user", Blocks: []headless.Block{{Type: "tool_result", ToolUseID: id, Text: text, IsError: isErr}}}
	if structured != nil {
		m.ToolResult = raw(structured)
	}
	return m
}

func say(text string) headless.Message {
	return headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "text", Text: text}}}
}

// session plays two turns: a finished one with three clean steps and an
// answer, and a live one with a failed command, an edit and a subagent.
func session() *Session {
	s := New()
	s.Info.Cwd = "/work/agtop"
	evs := []struct {
		sec int
		ev  any
	}{
		{0, host.Sent{Text: "the ux right now is totally broken when i attach"}},
		{1, say("Looking at how attach restores the terminal modes.")},
		{2, toolUse("r1", "Read", map[string]any{"file_path": "/work/agtop/internal/daemon/attach.go"})},
		{3, toolResult("r1", "…", false, map[string]any{"type": "text", "file": map[string]any{"numLines": 166, "startLine": 1, "totalLines": 166}})},
		{3, toolUse("g1", "Grep", map[string]any{"pattern": "1049h", "path": "/work/agtop/internal"})},
		{4, toolResult("g1", "internal/daemon/attach.go\ninternal/ui/live.go", false, nil)},
		{4, toolUse("b1", "Bash", map[string]any{"command": "go build ./..."})},
		{6, toolResult("b1", "", false, map[string]any{"stdout": "", "stderr": ""})},
		{7, toolUse("e1", "Edit", map[string]any{"file_path": "/work/agtop/internal/daemon/attach.go"})},
		{8, toolResult("e1", "ok", false, map[string]any{"structuredPatch": []map[string]any{{"oldStart": 60, "oldLines": 2, "newStart": 60, "newLines": 3,
			"lines": []string{" \tfor _, m := range info.DecModes {", "-\t\tfmt.Fprint(out, x)", "+\t\tfmt.Fprint(out, y)", "+\t\tfmt.Fprint(out, z)"}}}})},
		{9, say("Fixed the **alt screen**. Attach now clears to the alternate screen first.")},
		{10, headless.Result{Subtype: "success", CostUSD: 0.52}},

		{20, host.Sent{Text: "add modern key stuff to input too"}},
		{21, headless.Delta{Thinking: true, Text: "hmm"}},
		{22, toolUse("b2", "Bash", map[string]any{"command": "cd /work/agtop/internal/ui && go vet ./... 2>&1 | head -50"})},
		{23, toolResult("b2", "Exit code 1\ninternal/ui/editor.go:41:2: unreachable code", true,
			map[string]any{"stdout": "internal/ui/editor.go:41:2: unreachable code", "stderr": ""})},
		{24, toolUse("w1", "Write", map[string]any{"file_path": "/work/agtop/internal/ui/editor.go"})},
		{25, toolResult("w1", "ok", false, map[string]any{"type": "create", "content": "package ui\n\nfunc x() {}\n"})},
		{26, toolUse("a1", "Task", map[string]any{"subagent_type": "Explore", "description": "find the preview pane"})},
		{27, headless.Message{Role: "assistant", ParentToolUseID: "a1", Blocks: []headless.Block{{Type: "tool_use", ID: "a1g", Name: "Grep", Input: raw(map[string]any{"pattern": "previewLines"})}}}},
		{28, toolUse("b3", "Bash", map[string]any{"command": "go test -race ./..."})},
		{29, headless.PermissionRequest{ID: "req-1", Tool: "Bash", ToolUseID: "b3"}},
		{30, headless.Delta{Text: "Building the line editor"}},
	}
	for _, e := range evs {
		s.Apply(e.ev, at(e.sec))
	}
	return s
}

func plain(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(strings.TrimRight(stripANSI(l.Text), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestModel(t *testing.T) {
	s := session()
	if len(s.Turns) != 2 {
		t.Fatalf("turns: %d", len(s.Turns))
	}
	done, live := s.Turns[0], s.Turns[1]
	if done.Live || !live.Live {
		t.Fatalf("live flags: %v %v", done.Live, live.Live)
	}
	if got := done.Outcome(); got != "Fixed the alt screen. Attach now clears to the alternate screen first." {
		t.Errorf("outcome: %q", got)
	}
	if done.Cost != 0.52 || done.Steps() != 4 {
		t.Errorf("cost %v steps %d", done.Cost, done.Steps())
	}
	if st := s.byID["b2"]; st.Status != Failed || st.Exit != 1 {
		t.Errorf("b2: %v exit %d", st.Status, st.Exit)
	}
	if st := s.byID["a1"]; len(st.Children) != 1 || st.Children[0].ID != "a1g" {
		t.Errorf("subagent children: %+v", st.Children)
	}
	if p := s.Pending(); len(p) != 1 || p[0].ID != "b3" {
		t.Errorf("pending: %+v", p)
	}
	s.Apply(host.Answered{ID: "req-1"}, at(31))
	if st := s.byID["b3"]; st.Status != Running || st.Approval != nil {
		t.Errorf("after answer: %v", st.Status)
	}
}

func TestRender(t *testing.T) {
	s := session()
	out := plain(s.Render(Options{Width: 110, Now: at(40)}))
	if os.Getenv("CONVO_SHOW") != "" {
		t.Log("\n" + out)
	}
	want := []string{
		"▾ ✓ you  the ux right now is totally broken when i attach",
		"#1  4 steps   10s   $0.52",
		"Looking at how attach restores the terminal modes.", // narration
		"✓ 3 steps   ◧ 1   ⌕ 1   $ 1",                        // clean run folded
		"✓ ✎ internal/daemon/attach.go",                      // an edit never folds
		"+2 −1",
		"   Fixed the alt screen.", // answer on the conversation axis, markdown stripped
		"▾ ✻ you  add modern key stuff to input too",
		"✗ $  in internal/ui  go vet ./... 2>&1 | head -50", // cd becomes a chip
		"exit 1",
		"▎internal/ui/editor.go:41:2: unreachable code", // failure opened itself
		"✎ internal/ui/editor.go",
		"new · 3 lines",
		"⇉ Explore  find the preview pane",
		"1 step ",
		"⌕ previewLines", // a running subagent shows its steps
		"● $ go test -race ./...",
		"waiting on you",
		"Building the line editor",
		"Building the line editor",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if strings.Contains(out, "60 ") && strings.Contains(out, "fmt.Fprint(out, y)") {
		t.Errorf("a successful edit's diff should stay closed by default")
	}
}

func TestFoldAndOpen(t *testing.T) {
	s := session()
	// Three more finished turns push the first one out of the recent pair.
	for i := 0; i < 1; i++ {
		s.Apply(headless.Result{Subtype: "success"}, at(50))
		s.Apply(host.Sent{Text: "and another"}, at(51))
		s.Apply(say("Done."), at(52))
		s.Apply(headless.Result{Subtype: "success"}, at(53))
	}
	out := plain(s.Render(Options{Width: 110, Now: at(60)}))
	if !strings.Contains(out, "▸ ✓ #1  the ux right now is totally broken when i a…  → Fixed the alt screen.") {
		t.Errorf("first turn should fold to ask → outcome:\n%s", out)
	}
	// Opening a step shows its diff; opening the folded run lists its steps.
	o := Options{Width: 110, Now: at(60), Open: map[string]bool{"t1": true, "t1:s:e1": true, "t1:run:1": true}}
	out = plain(s.Render(o))
	for _, w := range []string{"61 + \t\tfmt.Fprint(out, y)", "61 − \t\tfmt.Fprint(out, x)", "◧ internal/daemon/attach.go"} {
		w = strings.ReplaceAll(w, "\t", "    ")
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
}

func TestStreamingAndCrash(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "go"}, at(0))
	s.Apply(headless.Delta{Text: "Work"}, at(1))
	s.Apply(headless.Delta{Text: "ing on it"}, at(1))
	if got := s.Turns[0].Items[0].Text; got != "Working on it" {
		t.Fatalf("stream: %q", got)
	}
	// The whole message replaces the streamed text rather than repeating it.
	s.Apply(say("Working on it."), at(2))
	if n := len(s.Turns[0].Items); n != 1 || s.Turns[0].Items[0].Text != "Working on it." {
		t.Fatalf("items after whole message: %d", n)
	}
	s.Apply(toolUse("x", "Bash", map[string]any{"command": "sleep 100"}), at(3))
	s.Apply(host.InfoEvent{Info: host.Info{State: "idle", Error: "exit status 1"}}, at(4))
	tn := s.Turns[0]
	if tn.Live || !strings.HasPrefix(tn.Err, "claude exited mid-turn") || s.byID["x"].Status != Lost {
		t.Fatalf("crash: live=%v err=%q status=%v", tn.Live, tn.Err, s.byID["x"].Status)
	}
	out := plain(s.Render(Options{Width: 100, Now: at(5)}))
	if !strings.Contains(out, "✗ claude exited mid-turn") || !strings.Contains(out, "◌ $ sleep 100") {
		t.Errorf("crash render:\n%s", out)
	}
}

func TestTasks(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "plan"}, at(0))
	s.Apply(toolUse("c1", "TaskCreate", map[string]any{"subject": "Build editor", "activeForm": "Building editor"}), at(1))
	s.Apply(toolResult("c1", "Task #1 created successfully: Build editor", false, nil), at(1))
	s.Apply(toolUse("c2", "TaskCreate", map[string]any{"subject": "Wire keys"}), at(2))
	s.Apply(toolResult("c2", "Task #2 created successfully: Wire keys", false, nil), at(2))
	s.Apply(toolUse("u1", "TaskUpdate", map[string]any{"taskId": "1", "status": "in_progress"}), at(3))
	now, done, total := s.Current()
	if now == nil || now.Subject != "Build editor" || done != 0 || total != 2 {
		t.Fatalf("current: %+v %d/%d", now, done, total)
	}
	out := plain(s.Render(Options{Width: 100, Now: at(4)}))
	if strings.Contains(out, "TaskCreate") || strings.Contains(out, "TaskUpdate") {
		t.Errorf("task bookkeeping should not show as steps:\n%s", out)
	}
}

func TestCacheReuse(t *testing.T) {
	s := session()
	s.Apply(headless.Result{Subtype: "success"}, at(50))
	o := Options{Width: 100, Now: at(60)}
	a := s.Render(o)
	b := s.Render(o)
	if &a[0] == &b[0] {
		t.Skip("slices alias; content equality is what matters")
	}
	if plain(a) != plain(b) {
		t.Error("cached render differs")
	}
	first := s.cache[s.Turns[0]]
	s.Render(Options{Width: 100, Now: at(70), Tick: 3})
	if s.cache[s.Turns[0]].key != first.key {
		t.Error("a finished turn should not redraw when only the clock moves")
	}
}

func TestTint(t *testing.T) {
	got := tint(`go test -run "Foo Bar" ./... && echo $HOME | head`)
	if stripANSI(got) != `go test -run "Foo Bar" ./... && echo $HOME | head` {
		t.Fatalf("tint changed the text: %q", stripANSI(got))
	}
	if !strings.Contains(got, cOrange+"&&") || !strings.Contains(got, cGreen+`"Foo Bar"`) || !strings.Contains(got, cBlue+"$HOME") {
		t.Errorf("tint colours: %q", got)
	}
}

func TestOverview(t *testing.T) {
	s := New()
	s.Info = host.Info{Cwd: "/work", Effort: "high", PermissionMode: "auto", Model: "claude-opus-5-5[1m]"}
	s.Apply(host.Sent{Text: "go"}, at(0))
	use := func(id, model, parent string, read, write int, sec int) {
		s.Apply(headless.Message{Role: "assistant", ID: id, Model: model, ParentToolUseID: parent,
			Usage:  &headless.Usage{InputTokens: 10, OutputTokens: 50, CacheReadInputTokens: read, CacheCreationInputTokens: write},
			Blocks: []headless.Block{{Type: "text", Text: "…"}}}, at(sec))
	}
	use("m1", "claude-opus-5-5[1m]", "", 0, 20000, 1) // first request: cold is expected
	s.Apply(toolUse("b1", "Bash", map[string]any{"command": "ls"}), at(2))
	s.Apply(toolResult("b1", "x", false, nil), at(3))
	s.Apply(toolUse("b2", "Bash", map[string]any{"command": "false"}), at(4))
	s.Apply(toolResult("b2", "Exit code 1", true, nil), at(5))
	s.Apply(toolUse("a1", "Task", map[string]any{"subagent_type": "Explore", "description": "look"}), at(6))
	use("s1", "claude-haiku-4-5-20251001", "a1", 0, 9000, 7)
	use("m2", "claude-opus-5-5[1m]", "", 20000, 500, 8)
	use("m3", "claude-opus-5-5[1m]", "", 100, 21000, 8+3700) // an hour idle: the cache went cold
	s.Apply(headless.Result{Subtype: "success", CostUSD: 1.25}, at(3710))

	cold := s.ColdStarts()
	if len(cold) != 1 || cold[0].Agent != "" || cold[0].Gap != 3700*time.Second {
		t.Fatalf("cold starts: %+v", cold)
	}
	out := plain(s.Overview(Options{Width: 110, Now: at(3720)}))
	if os.Getenv("CONVO_SHOW") != "" {
		t.Log("\n" + out)
	}
	for _, w := range []string{
		"main agent   opus 5.5 · 1M · effort high",
		"of 1M",
		"permissions  auto",
		"$1.25   1 turn · 1h 01m working",
		"tool calls   3   1 failed",
		"#1           opus 5.5 · 1M   effort high",
		"most used: Bash",
		"Bash",
		"cold after 1h 01m idle · main · rewrote 21k",
		"Explore      ×1   haiku 4.5",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if PrettyModel("claude-sonnet-5") != "sonnet 5" {
		t.Errorf("pretty: %s", PrettyModel("claude-sonnet-5"))
	}
}
