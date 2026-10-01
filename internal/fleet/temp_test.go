package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A running agent's temp work is measured again after ten minutes, or after
// fifty times as long as the last walk took when that was long.
func TestTempDueBacksOff(t *testing.T) {
	now := time.Now()
	quick := &Agent{Key: "quick", PID: 1}
	slow := &Agent{Key: "slow", PID: 2}
	ts := &TempSizes{Sizes: map[string]TempSize{
		"quick": {At: now.Add(-11 * time.Minute), Took: 100 * time.Millisecond},
		"slow":  {At: now.Add(-11 * time.Minute), Took: 30 * time.Second},
	}}
	due := ts.Due([]*Agent{quick, slow}, now)
	if len(due) != 1 || due[0] != quick {
		t.Fatalf("only the quick one is due: %v", due)
	}
	if due := ts.Due([]*Agent{slow}, now.Add(24*time.Minute)); len(due) != 1 {
		t.Fatal("the slow one is due once 25 minutes have passed")
	}
}

// A stopped agent isn't walked again however long it sits: there are
// hundreds, some with millions of files.
func TestStoppedTempNotRewalked(t *testing.T) {
	now := time.Now()
	a := &Agent{Key: "old", Job: agent.Job{UpdatedAt: now.Add(-48 * time.Hour)}}
	ts := &TempSizes{Sizes: map[string]TempSize{"old": {At: now.Add(-24 * time.Hour)}}}
	if len(ts.Due([]*Agent{a}, now)) != 0 {
		t.Fatal("a stopped session's temp was walked again")
	}
}

// The tidy-up empties a gone session's own tmp folder once it has sat
// untouched, keeps the folder, and leaves one changed recently or whose
// host still runs.
func TestCleanStaleTemp(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	now := time.Now()
	a := &Agent{Rush: true, Job: agent.Job{ID: "gone"}}
	dir := host.TempDir(a.ID)
	old, fresh := filepath.Join(dir, "old"), filepath.Join(dir, "fresh")
	for _, d := range []string{filepath.Join(old, "deep"), fresh} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	long := now.Add(-5 * time.Hour)
	_ = os.Chtimes(old, long, long)
	if ok, _ := CleanStaleTemp(a, 3*time.Hour, now); ok {
		t.Fatal("emptied a folder with something changed an hour ago")
	}
	_ = os.Chtimes(fresh, long, long)
	if ok, _ := CleanStaleTemp(&Agent{Rush: true, PID: 1, Job: agent.Job{ID: "gone"}}, 3*time.Hour, now); ok {
		t.Fatal("emptied a running session's folder")
	}
	if ok, err := CleanStaleTemp(a, 3*time.Hour, now); !ok || err != nil {
		t.Fatalf("left a stale folder: %v %v", ok, err)
	}
	if ents, err := os.ReadDir(dir); err != nil || len(ents) != 0 {
		t.Fatalf("folder gone or not emptied: %v %v", ents, err)
	}
}

// One thing in a gone session's temp folder goes alone; nothing outside it
// does, nor anything while the session runs.
func TestRemoveTempEntry(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	a := &Agent{Rush: true, Job: agent.Job{ID: "one"}}
	dir := host.TempDir(a.ID)
	for _, d := range []string{"a/deep", "b"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveTempEntry(&Agent{Rush: true, PID: 1, Job: agent.Job{ID: "one"}}, filepath.Join(dir, "a")); err == nil {
		t.Fatal("deleted from a running session")
	}
	if err := RemoveTempEntry(a, filepath.Join(dir, "a", "deep")); err == nil {
		t.Fatal("deleted something not directly in the temp folder")
	}
	if err := RemoveTempEntry(a, dir); err == nil {
		t.Fatal("deleted the temp folder itself")
	}
	if err := RemoveTempEntry(a, filepath.Join(dir, "a")); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 || ents[0].Name() != "b" {
		t.Fatalf("left %v, want only b", ents)
	}
}
