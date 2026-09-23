package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
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
	if cmd, _ := m.questionKey(c, req, "enter", true); cmd != nil || c.qIdx != 2 {
		t.Fatalf("with two questions, the last answer goes to the review, not straight out: idx=%d", c.qIdx)
	}
	want := map[string]string{"Which rules first?": "no-floating-promises", "Which packages?": "mcp, skills"}
	for k, v := range want {
		if c.qAnswer[k] != v {
			t.Errorf("%q: got %q want %q", k, c.qAnswer[k], v)
		}
	}
	// From the review, a digit goes back to a question to change it; the
	// cursor sits on what was chosen, and answering returns to the review.
	c.cardFocus = true
	m.questionKey(c, req, "1", true)
	if c.qIdx != 0 || c.qCursor != 0 {
		t.Fatalf("back to question 1: idx=%d cursor=%d", c.qIdx, c.qCursor)
	}
	m.questionKey(c, req, "down", true)
	m.questionKey(c, req, "enter", true)
	if c.qIdx != 2 || c.qAnswer["Which rules first?"] != "explicit return types" {
		t.Fatalf("changed answer: idx=%d %v", c.qIdx, c.qAnswer)
	}
	// ← from the review is the last question, with its ticks kept.
	m.questionKey(c, req, "left", true)
	if c.qIdx != 1 || !c.picks(1)[0] || !c.picks(1)[1] || c.picks(1)[2] {
		t.Fatalf("ticks kept: idx=%d %v", c.qIdx, c.qPicks)
	}
	m.questionKey(c, req, "right", true)
	if cmd, used := m.questionKey(c, req, "enter", true); !used || cmd == nil || c.qFor != "" {
		t.Fatal("enter on the review should reply")
	}
	// A lone question sends as soon as it's answered.
	one, _ := json.Marshal(map[string]any{"questions": []map[string]any{{"question": "Go?", "options": []map[string]any{{"label": "yes"}, {"label": "no"}}}}})
	c1 := &hostConn{}
	if cmd, _ := m.questionKey(c1, &headless.PermissionRequest{ID: "q2", Tool: "AskUserQuestion", Input: one}, "2", true); cmd == nil {
		t.Fatal("a single question should reply on its answer")
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

func TestSlashQueueTasks(t *testing.T) {
	c := &hostConn{sess: convo.New(), open: map[string]bool{}}
	c.sess.Commands = []headless.Command{{Name: "compact", Description: "summarise"}, {Name: "context"}, {Name: "code-review"}}
	c.input = []rune("/co")
	names := func() (out []string) {
		for _, x := range slashMatches(c) {
			out = append(out, x.Name)
		}
		return
	}
	if got := names(); len(got) != 3 || got[0] != "compact" {
		t.Fatalf("matches for /co: %v", got)
	}
	c.input = []rune("/cle")
	if got := names(); len(got) != 1 || got[0] != "clear" {
		t.Fatalf("agtop's /clear should match: %v", got)
	}
	c.input = []rune("/compact now")
	if len(slashMatches(c)) != 0 {
		t.Fatal("the picker closes once arguments start")
	}

	// A transcript-backed session can't take /model; agtop says so rather
	// than sending it to Claude.
	m := &Model{snap: &fleet.Snapshot{}}
	if _, ok := m.runAgtopCommand(c, "/model haiku"); !ok || m.status == "" {
		t.Fatal("/model should be handled by agtop")
	}
	if _, ok := m.runAgtopCommand(c, "/compact"); ok {
		t.Fatal("/compact belongs to Claude Code")
	}

	// Enter on a queued message pulls it into the box for editing.
	c.sess.Info.Queue = []string{"first", "second"}
	c.sel = "q:1"
	if _, used := m.queueKey(c, "enter"); !used || string(c.input) != "second" || c.editQ != 2 || c.sel != "" {
		t.Fatalf("edit queued: used=%v input=%q editQ=%d", used, string(c.input), c.editQ)
	}

	// Tasks group into now, next and done.
	c.sess.Tasks = []convo.Task{{Subject: "a", Status: "completed"}, {Subject: "b", Active: "Doing b", Status: "in_progress"}, {Subject: "c", Status: "pending"}}
	var out string
	for _, l := range m.taskLines(c, convo.Options{Width: 80}) {
		out += ansi.Strip(l.Text) + "\n"
	}
	for _, want := range []string{"1 of 3 done", "Now  1", "■ Doing b", "Next  1", "Done  1", "✓ a"} {
		if !strings.Contains(out, want) {
			t.Errorf("tasks view missing %q:\n%s", want, out)
		}
	}
}

func TestSlashMidMessage(t *testing.T) {
	c := &hostConn{sess: convo.New(), open: map[string]bool{}}
	c.local = []headless.Command{{Name: "design:design-critique"}, {Name: "pdf"}}
	c.input = []rune("please /crit this")
	c.back = len(" this")
	got := slashMatches(c)
	if len(got) != 1 || got[0].Name != "design:design-critique" {
		t.Fatalf("mid-message matches: %v", got)
	}
	m := &Model{snap: &fleet.Snapshot{}}
	if _, ok := m.slashKey(c, "enter"); !ok {
		t.Fatal("enter should complete")
	}
	if string(c.input) != "please /design:design-critique this" {
		t.Fatalf("completed to %q", string(c.input))
	}
	c.input, c.back = []rune("see /var/folders/x"), 0
	if slashMatches(c) != nil {
		t.Fatal("a path is not a command")
	}
	c.input = []rune("/cl")
	for _, x := range slashMatches(c) {
		if x.Name == "clear" {
			return
		}
	}
	t.Fatal("agtop's own commands still show at the start")
}

func TestLocalQueue(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}}
	m.queueLocal("k", "one")
	m.queueLocal("k", "two")
	if q := m.queueOf(c); !q.local || len(q.items) != 2 {
		t.Fatalf("queue = %+v", q)
	}
	c.sel = "q:0"
	if _, ok := m.localQueueKey(c, "alt+m"); !ok || m.localQ["k"].items[0] != "one\n\ntwo" {
		t.Fatalf("merge: %q", m.localQ["k"].items)
	}
	m.editLocal(c, 0, "one\n\ntwo", "edited")
	if m.localQ["k"].items[0] != "edited" {
		t.Fatal("edit didn't save")
	}
	if got := withImages("look", []string{"/a.png"}); got != "look\n[image: /a.png]" {
		t.Fatalf("withImages = %q", got)
	}
}

func TestArgPicker(t *testing.T) {
	c := &hostConn{sess: convo.New(), open: map[string]bool{}}
	c.sess.Info.Model = "claude-sonnet-5"
	c.input = []rune("/model ")
	got := argMatches(c)
	if len(got) != 6 {
		t.Fatalf("all models offered: %v", got)
	}
	for _, g := range got {
		if g.Name == "model sonnet" && !strings.Contains(g.Description, "now") {
			t.Fatal("current model not marked")
		}
	}
	c.input = []rune("/effort x")
	if got := argMatches(c); len(got) != 1 || got[0].Name != "effort xhigh" {
		t.Fatalf("effort x: %v", got)
	}
	c.input = []rune("/model")
	if argMatches(c) != nil {
		t.Fatal("no argument picker before the space")
	}
}

func TestArtifacts(t *testing.T) {
	s := convo.New()
	now := time.Now()
	pub := func(id, ver string) {
		in, _ := json.Marshal(map[string]string{"file_path": "/tmp/agtop-mode.html", "description": "design review"})
		s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: id, Name: "Artifact", Input: in}}}, now)
		s.Apply(headless.Message{Role: "user", Blocks: []headless.Block{{Type: "tool_result", ToolUseID: id,
			Text: "Published /tmp/agtop-mode.html at https://claude.ai/artifact/2pdtkfBi4he8cVWra7qYq6 (Version " + ver + ")"}}}, now)
	}
	pub("a1", "1")
	pub("a2", "2")
	arts := artifacts(s)
	if len(arts) != 1 || arts[0].Version != 2 || arts[0].Versions != 2 || arts[0].About != "design review" {
		t.Fatalf("artifacts = %+v", arts)
	}
}

func TestLocalQueueSends(t *testing.T) {
	a := &fleet.Agent{Key: "k"}
	a.State = "working"
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{a}}}
	m.queueLocal("k", "hi")
	if m.flushLocalQueues() != nil {
		t.Fatal("sent straight away while it works; should gather for a moment")
	}
	m.localQ["k"].since = time.Now().Add(-20 * time.Second)
	if m.flushLocalQueues() == nil || len(m.localQ["k"].items) != 0 {
		t.Fatal("a busy agent never got its queue")
	}
	a.State = "blocked"
	m.queueLocal("k", "later")
	m.localQ["k"].since = time.Now().Add(-time.Minute)
	if m.flushLocalQueues() != nil {
		t.Fatal("sent while it waits on you")
	}
}

func TestQuestionCardDraws(t *testing.T) {
	in := map[string]any{"questions": []map[string]any{
		{"question": "Which layout?", "header": "Layout", "options": []map[string]any{
			{"label": "Split (Recommended)", "description": "Agents left, Session right", "preview": "```\n┌────┬────────┐\n│ A  │ S      │\n└────┴────────┘\n```"},
			{"label": "Stacked", "preview": "┌────────┐\n│ A      │\n├────────┤\n│ S      │\n└────────┘"},
		}},
		{"question": "Theme?", "header": "Theme", "options": []map[string]any{{"label": "dark"}, {"label": "light"}}},
	}}
	b, _ := json.Marshal(in)
	req := &headless.PermissionRequest{ID: "q9", Tool: "AskUserQuestion", Input: b}
	m := &Model{}
	c := &hostConn{cardFocus: true}
	draw := func(w int) string {
		var sb strings.Builder
		for _, l := range m.questionCard(c, req, w) {
			if cellw.String(l) > w {
				t.Fatalf("row wider than %d: %q", w, ansi.Strip(l))
			}
			sb.WriteString(ansi.Strip(l) + "\n")
		}
		return sb.String()
	}
	wide := draw(120)
	for _, want := range []string{"● Layout", "○ Theme", "○ send", "0 of 2 answered", "Split  recommended", "╭─ Split ─", "│ A  │ S      │", "←→ questions"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("wide card missing %q:\n%s", want, wide)
		}
	}
	// Side by side: the option and the preview share a row.
	sideBySide := false
	for _, l := range strings.Split(wide, "\n") {
		if strings.Contains(l, "Split  recommended") && strings.Contains(l, "╭─ Split") {
			sideBySide = true
		}
	}
	if !sideBySide || strings.Contains(wide, "```") {
		t.Fatalf("preview beside the options, fences dropped:\n%s", wide)
	}
	// Narrow: the preview goes under, and the option says it has one.
	narrow := draw(70)
	if !strings.Contains(narrow, "◇ preview") || !strings.Contains(narrow, "│ A  │ S      │") {
		t.Fatalf("narrow card:\n%s", narrow)
	}
	// Answered, the strip shows the answer and the review lists them.
	m.questionKey(c, req, "1", true)
	m.questionKey(c, req, "2", true)
	review := draw(120)
	for _, want := range []string{"✓ Layout Split", "● send", "Layout   Split", "Theme    light", "sends your answers"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review missing %q:\n%s", want, review)
		}
	}
}

func TestAnswersCarryPreview(t *testing.T) {
	in := map[string]any{"questions": []map[string]any{
		{"question": "Which?", "options": []map[string]any{{"label": "a", "preview": "A!"}, {"label": "b"}}},
		{"question": "And?", "options": []map[string]any{{"label": "x"}}},
	}}
	b, _ := json.Marshal(in)
	req := &headless.PermissionRequest{ID: "q3", Tool: "AskUserQuestion", Input: b}
	_, qs := questions(req)
	var got struct {
		Questions   []any                        `json:"questions"`
		Answers     map[string]string            `json:"answers"`
		Annotations map[string]map[string]string `json:"annotations"`
	}
	_ = json.Unmarshal(answerInput(req, qs, map[string]string{"Which?": "a", "And?": "my own"}), &got)
	if len(got.Questions) != 2 || got.Answers["Which?"] != "a" || got.Answers["And?"] != "my own" {
		t.Fatalf("answers: %+v", got)
	}
	if got.Annotations["Which?"]["preview"] != "A!" || got.Annotations["And?"] != nil {
		t.Fatalf("annotations: %+v", got.Annotations)
	}
}
