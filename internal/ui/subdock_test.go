package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A running subagent shows in the dock with what it's doing and what it
// just did; ↑ from the box picks it, enter watches it and ← comes back to
// its row.
func TestSubagentDock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"user","isSidechain":true,"timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"find the pane"}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-23T20:00:01Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"g1","name":"Grep","input":{"pattern":"previewLines"}}]}}`,
		`{"type":"user","isSidechain":true,"timestamp":"2026-09-23T20:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"g1","content":"view.go"}]}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-23T20:00:03Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"/w/view.go"}}]}}`,
	}, "\n")+"\n"), 0o644)
	st := convo.SubagentStats(path)
	st.Read()
	sa := convo.Subagent{ID: "a1", Type: "Explore", Description: "find the pane", Path: path, Mod: time.Now().UnixNano()}
	c := &hostConn{key: "k", client: &host.Client{}, sess: convo.New(), open: map[string]bool{},
		subs: []convo.Subagent{sa}, subTails: map[string]*convo.Tail{"a1": st}}
	m := &Model{snap: &fleet.Snapshot{}, host: c, paneFocus: true}

	out := ansi.Strip(strings.Join(m.runningPreview(c, c.runningSubs(), 100), "\n"))
	for _, w := range []string{"1 subagent working", "Explore", "find the pane", "› reading view.go", "‹ searching for previewLines", "↑ pick one to watch"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if strings.Contains(out, "alt") {
		t.Errorf("no alt keys:\n%s", out)
	}
	m.moveSel(c, -1)
	if c.sel != "run:a1" {
		t.Fatalf("↑ picked %q", c.sel)
	}
	m.paneKey(tea.KeyPressMsg{}, "enter")
	if c.subOpen != "a1" || m.viewName(c) != "subagents" {
		t.Fatalf("enter: open %q in %s", c.subOpen, m.viewName(c))
	}
	// Watching, it's plain whose conversation this is, where typing goes,
	// and how to get back.
	banner := ansi.Strip(m.subBanner(c, 120))
	for _, w := range []string{"WATCHING SUBAGENT", "Explore", "find the pane", "running", "2 steps", "esc back to the conversation"} {
		if !strings.Contains(banner, w) {
			t.Errorf("banner missing %q: %s", w, banner)
		}
	}
	m.paneKey(tea.KeyPressMsg{}, "left")
	if c.subOpen != "" || m.viewName(c) != "conversation" || c.sel != "run:a1" {
		t.Fatalf("←: open %q in %s on %q", c.subOpen, m.viewName(c), c.sel)
	}
	if m.subBanner(c, 120) != "" {
		t.Fatal("banner stays after leaving")
	}
	// esc leaves it too.
	m.paneKey(tea.KeyPressMsg{}, "enter")
	c.sel = ""
	m.paneKey(tea.KeyPressMsg{}, "esc")
	if c.subOpen != "" || m.viewName(c) != "conversation" {
		t.Fatalf("esc: open %q in %s", c.subOpen, m.viewName(c))
	}

	// A card waiting sits just above the box, below the subagents: ↑ from
	// the box goes straight onto it, ↑ again to the subagent above, ↓ back
	// onto the card, and ↓ again to the box.
	c.sess.Apply(host.Sent{Text: "go"}, time.Now())
	c.sess.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "b1", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)}}}, time.Now())
	c.sess.Apply(headless.PermissionRequest{ID: "r1", Tool: "Bash", ToolUseID: "b1"}, time.Now())
	key := func(s string) { m.paneKey(tea.KeyPressMsg{}, s) }
	c.sel = ""
	key("up")
	if !c.cardFocus || c.sel != "" {
		t.Fatalf("↑ from the box: on %q, card %v", c.sel, c.cardFocus)
	}
	key("up")
	if c.cardFocus || c.sel != "run:a1" {
		t.Fatalf("↑ off the card: on %q, card %v", c.sel, c.cardFocus)
	}
	key("down")
	if !c.cardFocus || c.sel != "" {
		t.Fatalf("↓ onto the card: on %q, card %v", c.sel, c.cardFocus)
	}
	key("down")
	if c.cardFocus || c.sel != "" {
		t.Fatalf("↓ off the card: on %q, card %v", c.sel, c.cardFocus)
	}
}

// In the subagents view the run under the pointer lights up, the pointer
// says it can be clicked, and resting on one in the wide view shows its
// conversation beside the list in place of the picked one's.
func TestSubagentHover(t *testing.T) {
	dir := t.TempDir()
	write := func(id, said string) convo.Subagent {
		path := filepath.Join(dir, "agent-"+id+".jsonl")
		os.WriteFile(path, []byte(strings.Join([]string{
			`{"type":"user","isSidechain":true,"timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"go"}}`,
			`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-23T20:00:01Z","message":{"id":"m` + id + `","role":"assistant","content":[{"type":"text","text":"` + said + `"}]}}`,
		}, "\n")+"\n"), 0o644)
		return convo.Subagent{ID: id, Type: "Explore", Description: "run " + id, Path: path, Mod: time.Now().UnixNano()}
	}
	subs := []convo.Subagent{write("a1", "first run speaking"), write("a2", "second run speaking")}
	c := &hostConn{key: "k", client: &host.Client{}, sess: convo.New(), open: map[string]bool{}, subs: subs,
		subTails: map[string]*convo.Tail{}}
	m := &Model{snap: &fleet.Snapshot{}, host: c, paneFocus: true}
	for i, v := range m.views(c) {
		if v == "subagents" {
			c.view = i
		}
	}
	c.sel = "sub:a2"
	c.rowRefs = []string{"", "", "", "sub:a2", "sub:a2", "sub:a1", "sub:a1"}

	if r := m.subHoverAt(10, 5); r != "sub:a1" {
		t.Fatalf("hover at a1's row: %q", r)
	}
	if r := m.subHoverAt(10, 1); r != "" {
		t.Fatalf("hover on the heading: %q", r)
	}
	if _, cmd := m.subMouseMove(10, 6); c.subHover != "sub:a1" || cmd == nil {
		t.Fatalf("moved onto a1: %q, rest tick %v", c.subHover, cmd != nil)
	}
	if changed, _ := m.subMouseMove(10, 5); changed {
		t.Fatal("another row of the same run is no change")
	}
	_, cmd := m.Update(tea.MouseMotionMsg{X: 10, Y: 5})
	if m.pointer != "pointer" || cmd == nil {
		t.Fatalf("pointer shape %q", m.pointer)
	}

	o := convo.Options{Width: 100, Now: time.Now(), Selected: c.sel, Focused: true}
	lines := m.subagentList(c, o)
	for _, l := range lines {
		if hovered := strings.Contains(l.Text, hoverBG); hovered != (l.Ref == "sub:a1") {
			t.Errorf("row of %q hovered=%v: %q", l.Ref, hovered, ansi.Strip(l.Text))
		}
	}

	c.paneW = 160
	if r := m.subHoverAt(100, 5); r != "" {
		t.Fatalf("the conversation beside the list is no run's: %q", r)
	}
	o.Width = 160
	side := func() string {
		var b strings.Builder
		for _, l := range m.subagentLines(c, o) {
			b.WriteString(ansi.Strip(l.Text) + "\n")
		}
		return b.String()
	}
	if out := side(); !strings.Contains(out, "second run speaking") || strings.Contains(out, "first run speaking") {
		t.Fatalf("before resting, the picked run shows beside:\n%s", out)
	}
	c.subHoverAt = time.Now().Add(-time.Second)
	if out := side(); !strings.Contains(out, "first run speaking") {
		t.Fatalf("rested on a1, its conversation shows beside:\n%s", out)
	}

	m.host = c // Update let go of it, having no agent in the snapshot
	m.key(tea.KeyPressMsg{Code: tea.KeyDown})
	if c.subHover != "" {
		t.Fatal("the keyboard takes over from the pointer")
	}
}

// Filling the screen, the Session's header needs no SESSION label, says how
// to get back to the list, and ends where the conversation does.
func TestPaneHeaderAlone(t *testing.T) {
	m, _ := benchModel(150, 30)
	m.full = true
	m.View()
	a := m.focused()
	head := m.paneHeader(a, m.host, 150)
	row1, row3 := ansi.Strip(head[0]), ansi.Strip(head[2])
	if strings.Contains(row1, "SESSION") {
		t.Errorf("label shows alone: %q", row1)
	}
	if !strings.Contains(row3, "esc back to the list") {
		t.Errorf("no way back: %q", row3)
	}
	if w := ansi.StringWidth(strings.TrimRight(row1, " ")); w > maxPane-3 {
		t.Errorf("header runs to %d, past the conversation", w)
	}
	m.full = false
	m.View()
	if m.listW == 0 {
		t.Fatal("no split at 150")
	}
	if head := ansi.Strip(strings.Join(m.paneHeader(a, m.host, 100), "\n")); !strings.Contains(head, "SESSION") || strings.Contains(head, "esc back") {
		t.Errorf("beside the list:\n%s", head)
	}
}

// x on a running subagent's row, or ctrl+x while watching it, stops that
// one alone; ctrl+x elsewhere still stops the turn.
func TestStopOneSubagent(t *testing.T) {
	sa := convo.Subagent{ID: "a1", Type: "Explore", Description: "look", Mod: time.Now().UnixNano()}
	c := &hostConn{key: "k", client: &host.Client{}, sess: convo.New(), open: map[string]bool{}, subs: []convo.Subagent{sa}}
	m := &Model{snap: &fleet.Snapshot{}, host: c, paneFocus: true}
	c.sel = "run:a1"
	if _, live, ok := m.pickedSub(c); !ok || !live {
		t.Fatalf("picked: ok %v live %v", ok, live)
	}
	if cmd := m.paneKey(tea.KeyPressMsg{Text: "x"}, "x"); cmd == nil || !strings.Contains(m.status, "stopping Explore") {
		t.Fatalf("x on the row: cmd %v, status %q", cmd != nil, m.status)
	}
	c.sess.TaskStatus["a1"] = "stopped"
	if _, live, _ := m.pickedSub(c); live {
		t.Fatal("stopped, it still counts as running")
	}
}
