package host

import (
	"strings"
	"testing"
	"time"
)

func TestOverdueTasks(t *testing.T) {
	now := time.Now()
	tasks := []Task{
		{ID: "a", Label: "wait for queue 40", Type: "monitor", StartedAt: now.Add(-150 * time.Minute)},
		{ID: "b", Label: "fresh", StartedAt: now.Add(-5 * time.Minute)},
		{ID: "c", Label: "asked lately", StartedAt: now.Add(-3 * time.Hour)},
	}
	due := overdueTasks(tasks, map[string]time.Time{"c": now.Add(-10 * time.Minute)}, now)
	if len(due) != 1 || due[0].ID != "a" {
		t.Fatalf("due: %+v", due)
	}
	if n := longTaskNote(due, now); !strings.Contains(n, `"wait for queue 40"`) || !strings.Contains(n, "2h30m") {
		t.Fatalf("note: %s", n)
	}
	if due := overdueTasks(tasks, map[string]time.Time{"c": now.Add(-2 * time.Hour)}, now); len(due) != 2 {
		t.Fatalf("asked over an hour ago, it's asked again: %+v", due)
	}
}
