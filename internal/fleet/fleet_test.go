package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

func TestSessionStatusBeatsAStaleJobFile(t *testing.T) {
	now := time.Now()
	ended := &Agent{Job: claude.Job{State: "working", UpdatedAt: now.Add(-20 * time.Second)}}
	ended.applyStatus(claude.Session{Status: "idle", StatusMs: now.UnixMilli()})
	if ended.State != "blocked" || !ended.Checking {
		t.Fatalf("turn end not surfaced: %+v", ended.Job)
	}
	answered := &Agent{Job: claude.Job{State: "blocked", Needs: "want that?", UpdatedAt: now.Add(-20 * time.Second)}}
	answered.applyStatus(claude.Session{Status: "busy", StatusMs: now.UnixMilli()})
	if answered.State != "working" || answered.Needs != "" {
		t.Fatalf("reply not surfaced: %+v", answered.Job)
	}
	fresh := &Agent{Job: claude.Job{State: "blocked", UpdatedAt: now}}
	fresh.applyStatus(claude.Session{Status: "busy", StatusMs: now.Add(-time.Minute).UnixMilli()})
	if fresh.State != "blocked" {
		t.Fatal("an older session status must not override a newer job file")
	}
}
