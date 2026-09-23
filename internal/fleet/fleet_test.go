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
	// The summary can be rewritten after the reply; the live flag still wins.
	answered := &Agent{Job: claude.Job{State: "blocked", Needs: "want that?", UpdatedAt: now}}
	answered.applyStatus(claude.Session{Status: "busy", StatusMs: now.Add(-time.Minute).UnixMilli()})
	if answered.State != "working" || answered.Needs != "" {
		t.Fatalf("reply not surfaced: %+v", answered.Job)
	}
	tending := &Agent{Job: claude.Job{State: "done", InFlight: 2, Background: []string{"shell\x00pnpm test"}, UpdatedAt: now.Add(-time.Minute)}}
	tending.applyStatus(claude.Session{Status: "busy", StatusMs: now.UnixMilli()})
	if tending.State != "done" {
		t.Fatal("a session tending background work is not working on a reply")
	}
	watching := &Agent{Job: claude.Job{State: "done", InFlight: 1, UpdatedAt: now.Add(-time.Minute)}}
	watching.applyStatus(claude.Session{Status: "busy", StatusMs: now.UnixMilli()})
	if watching.State != "working" {
		t.Fatal("a watcher or cron is not background work; a busy session is working")
	}
}

func TestNeedsYouOnlyForLiveUnseenQuestions(t *testing.T) {
	asking := &Agent{Job: claude.Job{State: "blocked"}, PID: 42}
	if !asking.NeedsYou() {
		t.Fatal("a live unseen question needs you")
	}
	asking.Seen = true
	if asking.NeedsYou() || !asking.Waiting() {
		t.Fatal("a seen question waits instead of nagging")
	}
	gone := &Agent{Job: claude.Job{State: "blocked"}}
	if gone.NeedsYou() || gone.Waiting() {
		t.Fatal("a question from a process that has exited needs nobody")
	}
}
