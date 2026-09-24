package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
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
	m.paneKey(tea.KeyPressMsg{}, "left")
	if c.subOpen != "" || m.viewName(c) != "conversation" || c.sel != "run:a1" {
		t.Fatalf("←: open %q in %s on %q", c.subOpen, m.viewName(c), c.sel)
	}
}
