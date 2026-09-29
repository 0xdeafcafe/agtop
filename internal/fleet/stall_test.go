package fleet

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
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
	a.Spend.Halt = &agent.Halt{Kind: "rate_limit", Text: "You've hit your session limit · resets 5am (Europe/London)"}
	if !a.Halted() || a.YourTurn(now) || a.ContinueText() != "continue" {
		t.Fatalf("a limit: halted %v, your turn %v", a.Halted(), a.YourTurn(now))
	}
	if got := a.HaltReason(); got != "session limit · resets 5am" {
		t.Fatalf("reason %q", got)
	}
	a.Spend.Halt = &agent.Halt{Kind: "server_error", Text: "API Error: Can't reach the API server — check your connection"}
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
		a.Spend.Halt = &agent.Halt{Kind: "server_error"}
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

// A session an API error stopped today continues once the API can be
// reached; one a limit stopped, one rush hosts (its host retries itself),
// or one stopped days ago doesn't.
func TestAPIErrorsContinue(t *testing.T) {
	now := time.Now()
	halted := func(text string, at time.Time) *Agent {
		a := &Agent{PID: 1}
		a.State, a.UpdatedAt = "done", at
		a.Spend.Halt = &agent.Halt{Kind: "server_error", Text: text, At: at}
		return a
	}
	a := halted("API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)", now.Add(-time.Minute))
	if !a.Continues(now) {
		t.Fatal("ENOTFOUND should be cut off")
	}
	if got := a.HaltReason(); got != "offline · continues when the network is back" {
		t.Fatalf("reason %q", got)
	}
	for _, text := range []string{"API Error: Connection dropped (ECONNRESET)", "API Error: Response stalled mid-stream. The response above may be incomplete."} {
		a := halted(text, now)
		if !a.Continues(now) || a.Offline() {
			t.Fatalf("%q should continue, online", text)
		}
		if got, want := a.HaltReason(), strings.TrimPrefix(text, "API Error: ")+" · continues by itself"; got != want {
			t.Fatalf("reason %q", got)
		}
	}
	a = halted("You've hit your session limit · resets 5am (Europe/London)", now)
	a.Spend.Halt.Kind = "rate_limit"
	if a.Continues(now) {
		t.Fatal("a limit waits for its reset")
	}
	a = halted("Not logged in · Please run /login", now)
	a.Spend.Halt.Kind = "authentication_failed"
	if a.Continues(now) {
		t.Fatal("logging in is yours to do")
	}
	if a := halted("API Error: Unable to connect to API (ENOTFOUND)", now.Add(-48*time.Hour)); a.Continues(now) {
		t.Fatal("two days ago is too long ago")
	}
	a = halted("API Error: Unable to connect to API (ENOTFOUND)", now)
	a.Rush = true
	if a.Continues(now) {
		t.Fatal("rush's own sessions wait for the network themselves")
	}
}
