package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/charmbracelet/x/ansi"
)

func TestJobActivityEvidence(t *testing.T) {
	now := time.Now()
	j := &convo.Job{Start: now.Add(-10 * time.Minute)}
	c := &hostConn{sess: convo.New(), tails: map[string]*jobTailed{"log": {size: 10, mod: now.Add(-2 * time.Minute)}}}
	if got := ansi.Strip(c.jobActivity(j, "log", now)); !strings.Contains(got, "quiet for 2m") || !strings.Contains(got, "may be waiting") {
		t.Fatalf("quiet: %s", got)
	}
	c.tails["log"].mod = now.Add(-2 * time.Second)
	if got := ansi.Strip(c.jobActivity(j, "log", now)); !strings.Contains(got, "output active") {
		t.Fatalf("active: %s", got)
	}
	c.tails["log"].size = 0
	if got := ansi.Strip(c.jobActivity(j, "log", now)); !strings.Contains(got, "no captured output") {
		t.Fatalf("empty file is not activity: %s", got)
	}
	j.Status = "completed"
	if got := c.jobActivity(j, "log", now); got != "" {
		t.Fatalf("completed task is not quiet: %s", got)
	}
}

func TestBackgroundOutputBeforeOptionalCommand(t *testing.T) {
	now := time.Now()
	s := convo.New()
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "tool", Name: "Bash", Input: []byte(`{"command":"echo setup-marker\necho work-marker","run_in_background":true}`)}}}, now)
	s.Apply(headless.TaskStarted{ID: "job", ToolUseID: "tool", Type: "local_bash", Description: "Run the checks", Backgrounded: true}, now)
	s.Job("job").OutputFile = "cached-output"
	c := &hostConn{kind: "claude", sess: s, open: map[string]bool{"job:job": true}, sel: "job:job", tails: map[string]*jobTailed{"cached-output": {at: now.Add(time.Hour), mod: now, size: 10, lines: []string{"All checks passed"}}}}
	m := &Model{host: c}
	for i, v := range m.views(c) {
		if v == "background" {
			c.view = i
		}
	}
	view := func() string { return ansi.Strip(joinLines(m.jobLines(c, convo.Options{Width: 140, Now: now}))) }
	got := view()
	if !strings.Contains(got, "All checks passed") || !strings.Contains(got, "command details · d to show") || strings.Contains(got, "setup-marker") {
		t.Fatalf("default view must lead with output and hide script:\n%s", got)
	}
	if _, used := m.jobKey(c, "d", true); !used {
		t.Fatal("details shortcut not handled")
	}
	got = view()
	output, command := strings.Index(got, "All checks passed"), strings.Index(got, "setup-marker")
	if command < output || command < 0 {
		t.Fatalf("output must precede command details:\n%s", got)
	}
	if _, used := m.jobKey(c, "enter", true); used {
		t.Fatal("Enter must not activate task rows")
	}
	if _, used := m.jobKey(c, "space", false); !used {
		t.Fatal("Space must open a selected task with retained draft")
	}
	if c.open["job:job"] {
		t.Fatal("Space should close opened task")
	}
}

func TestBackgroundFinishedSummaryRefresh(t *testing.T) {
	now := time.Now()
	s := convo.New()
	s.Apply(headless.TaskStarted{ID: "job", Type: "local_bash", Description: "Tests", Backgrounded: true}, now)
	s.Apply(headless.TaskDone{ID: "job", Status: "completed"}, now)
	j := s.Job("job")
	j.Summary = "old result"
	c := &hostConn{sess: s, open: map[string]bool{}}
	m := &Model{host: c}
	m.jobLines(c, convo.Options{Width: 100, Now: now})
	j.Summary = "new result"
	got := ansi.Strip(joinLines(m.jobLines(c, convo.Options{Width: 100, Now: now})))
	if !strings.Contains(got, "new result") || strings.Contains(got, "old result") {
		t.Fatalf("cached equal-length summary stale:\n%s", got)
	}
}

func TestJobActivityNativeProgressWithoutOutputFile(t *testing.T) {
	now := time.Now()
	c := &hostConn{sess: convo.New()}
	j := &convo.Job{Start: now.Add(-time.Minute), ProgressAt: now.Add(-3 * time.Second)}
	got := ansi.Strip(c.jobActivity(j, "", now))
	if !strings.Contains(got, "progress updated 3s ago") || !strings.Contains(got, "no captured output") {
		t.Fatalf("%s", got)
	}
}
