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
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// What Claude Code runs shows in the dock: a command the turn waits on,
// once it's run a while, and what runs in the background with its latest
// output. ↑ picks one; b backgrounds it, x stops it, and ctrl+b
// backgrounds what the turn waits on without picking.
func TestJobDock(t *testing.T) {
	out := filepath.Join(t.TempDir(), "b2.output")
	os.WriteFile(out, []byte("building\r50%\r100%\nlistening on :3000\n\n"), 0o644)
	s := convo.New()
	now := time.Now()
	s.Apply(host.InfoEvent{Info: host.Info{Proto: 3, ClaudePID: 1, State: "working"}}, now)
	s.Apply(headless.TaskStarted{ID: "b1", ToolUseID: "t1", Type: "local_bash", Description: "go test ./..."}, now.Add(-10*time.Second))
	s.Apply(headless.TaskStarted{ID: "b2", ToolUseID: "t2", Type: "local_bash", Description: "npm run dev", Backgrounded: true}, now.Add(-time.Minute))
	s.Apply(headless.TaskStarted{ID: "b3", ToolUseID: "t3", Type: "local_bash", Description: "ls"}, now)
	s.Job("b2").OutputFile = out

	c := &hostConn{kind: "claude", key: "k", client: &host.Client{}, sess: s, open: map[string]bool{}}
	m := &Model{snap: &fleet.Snapshot{}, host: c, paneFocus: true}
	jobs := c.dockJobs()
	if len(jobs) != 2 {
		t.Fatalf("a quick command shows, or a long one doesn't: %d", len(jobs))
	}
	got := ansi.Strip(strings.Join(m.jobsPreview(c, jobs, 110), "\n"))
	for _, want := range []string{"2 running · 1 in the background", "go test ./...", "waiting on it", "npm run dev", "in background", "› listening on :3000", "ctrl+b background"} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "› ls") {
		t.Fatalf("a quick command shows:\n%s", got)
	}

	// ↑ from the box: the nearest first.
	m.moveSel(c, -1)
	if c.sel != "job:b2" {
		t.Fatalf("↑ picked %q", c.sel)
	}
	if m.paneKey(tea.KeyPressMsg{Text: "b"}, "b"); !strings.Contains(m.status, "already in the background") {
		t.Fatalf("b on a background one: %q", m.status)
	}
	if cmd := m.paneKey(tea.KeyPressMsg{Text: "x"}, "x"); cmd == nil || !strings.Contains(m.status, "stopping the shell") {
		t.Fatalf("x: cmd %v, status %q", cmd != nil, m.status)
	}
	m.moveSel(c, -1)
	if c.sel != "job:b1" {
		t.Fatalf("↑↑ picked %q", c.sel)
	}
	if cmd := m.paneKey(tea.KeyPressMsg{Text: "b"}, "b"); cmd == nil || !strings.Contains(m.status, "moved the shell to the background") {
		t.Fatalf("b: cmd %v, status %q", cmd != nil, m.status)
	}
	c.sel = ""
	if cmd := m.paneKey(tea.KeyPressMsg{}, "ctrl+b"); cmd == nil || !strings.Contains(m.status, "to the background") {
		t.Fatalf("ctrl+b: cmd %v, status %q", cmd != nil, m.status)
	}

	// Once it's in the background, ctrl+b is /btw's again.
	s.Apply(headless.TaskUpdated{ID: "b1", Backgrounded: new(true)}, now)
	s.Apply(headless.TaskUpdated{ID: "b3", Status: "completed"}, now)
	if _, used := m.jobKey(c, "ctrl+b", true); used {
		t.Fatal("ctrl+b taken with nothing to background")
	}

	// The background view has them all; enter opens one's output.
	s.Apply(headless.TaskDone{ID: "b1", Status: "completed"}, now)
	c.sel = "job:b2"
	view := ansi.Strip(joinLines(m.jobLines(c, convo.Options{Width: 110, Selected: c.sel})))
	for _, want := range []string{"Running", "npm run dev", "Finished", "go test ./...", "✓ done"} {
		if !strings.Contains(view, want) {
			t.Fatalf("no %q in\n%s", want, view)
		}
	}
	if strings.Contains(view, "│ 100%") {
		t.Fatalf("closed, it shows more than its last line:\n%s", view)
	}
	c.open["job:b2"] = true
	if view := ansi.Strip(joinLines(m.jobLines(c, convo.Options{Width: 110}))); !strings.Contains(view, "│ 100%") {
		t.Fatalf("opened, no output:\n%s", view)
	}
}

func joinLines(ls []convo.Line) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}
