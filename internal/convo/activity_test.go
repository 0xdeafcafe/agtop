package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
)

func TestActivityMovesOutOfTranscript(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "go"}, at(0))
	s.Apply(event.PartStart{Kind: event.Thinking}, at(1))
	s.Apply(event.Delta{Kind: event.Thinking, Text: "thinking"}, at(1))
	o := Options{Width: 80, Now: at(9)}
	want := pick(musings, at(1))
	if !strings.Contains(plain(s.Render(o)), want) {
		t.Fatalf("standalone transcript lost activity %q: %s", want, plain(s.Render(o)))
	}
	o.HideActivity = true
	if strings.Contains(plain(s.Render(o)), want) {
		t.Fatal("cached transcript duplicated docked activity")
	}
	o.Open = map[string]bool{"t1": false}
	if got := plain(s.Activity(o)); !strings.Contains(got, want) || !strings.Contains(got, "8s") || !s.Fast {
		t.Fatalf("folded turn lost activity: %s", got)
	}
	o.HideActivity = false
	o.Open = nil
	if !strings.Contains(plain(s.Render(o)), want) {
		t.Fatal("returning to inline render reused the hidden status")
	}
	s.Apply(event.TurnEnd{Reason: "done"}, at(10))
	if len(s.Activity(o)) != 0 {
		t.Fatal("idle session retained activity")
	}
}

func TestDockActivityKeepsRunningToolVisible(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "run"}, at(0))
	s.Apply(toolUse("shell", "Bash", map[string]any{"command": "go test ./..."}), at(1))
	out := plain(s.Activity(Options{Width: 80, Now: at(600)}))
	if !strings.Contains(out, "running Bash") || strings.Contains(out, "stalled") {
		t.Fatalf("running tool status: %s", out)
	}
}

func TestDockActivityKeepsRetryAndCompaction(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "go"}, at(0))
	s.Live().Retry = &event.Retry{Attempt: 1, Max: 3, Delay: time.Minute, Err: "overloaded"}
	s.Live().RetryAt = at(1)
	if out := plain(s.Activity(Options{Width: 100, Now: at(2)})); !strings.Contains(out, "↻") {
		t.Fatalf("retry status missing: %s", out)
	}
	s.compacting = at(1)
	if out := plain(s.Activity(Options{Width: 100, Now: at(2)})); !strings.Contains(out, compaction(at(1))) {
		t.Fatalf("compaction status missing: %s", out)
	}
}
