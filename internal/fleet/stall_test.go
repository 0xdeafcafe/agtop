package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

func TestAFinishedTurnIsAnAnswerOrAnError(t *testing.T) {
	now := time.Now()
	finished := func() *Agent {
		a := &Agent{PID: 1}
		a.State, a.UpdatedAt = "done", now.Add(-10*time.Minute)
		return a
	}

	a := finished()
	if !a.YourTurn(now) || a.Halted() || a.ContinueText() != "keep going" {
		t.Fatalf("a quiet finish: your turn %v, halted %v", a.YourTurn(now), a.Halted())
	}
	a.Seen = true
	if a.YourTurn(now) {
		t.Fatal("still your turn once looked at")
	}

	a = finished()
	a.Spend.Halt = &claude.Halt{Kind: "rate_limit", Text: "You've hit your session limit · resets 5am (Europe/London)"}
	if !a.Halted() || a.YourTurn(now) || a.ContinueText() != "continue" {
		t.Fatalf("a limit: halted %v, your turn %v", a.Halted(), a.YourTurn(now))
	}
	if got := a.HaltReason(); got != "session limit · resets 5am" {
		t.Fatalf("reason %q", got)
	}
	a.Spend.Halt = &claude.Halt{Kind: "server_error", Text: "API Error: Can't reach the API server — check your connection"}
	if got := a.HaltReason(); got != "Can't reach the API server — check your connection" {
		t.Fatalf("reason %q", got)
	}

	// Working again, or put away, it is neither.
	for _, mod := range []func(*Agent){
		func(a *Agent) { a.State = "working" },
		func(a *Agent) { a.Done = true },
		func(a *Agent) { a.Past = true },
		func(a *Agent) { a.InFlight, a.Background = 1, []string{"agent\x00lane"} },
	} {
		a := finished()
		a.Spend.Halt = &claude.Halt{Kind: "server_error"}
		mod(a)
		b := finished()
		mod(b)
		if a.Halted() || b.YourTurn(now) {
			t.Fatalf("%+v: halted %v, your turn %v", a.Job, a.Halted(), b.YourTurn(now))
		}
	}
	if old := finished(); func() bool { old.UpdatedAt = now.Add(-25 * time.Hour); return old.YourTurn(now) }() {
		t.Fatal("a day-old finish is still your turn")
	}
}
