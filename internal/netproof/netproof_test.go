package netproof

import (
	"sync/atomic"
	"testing"
	"time"
)

func fake(t *testing.T, up *atomic.Bool) *atomic.Int32 {
	t.Helper()
	t.Setenv("AGTOP_HOME", t.TempDir())
	var checks atomic.Int32
	was, wasEvery, wasHold := probe, Every, Hold
	t.Cleanup(func() { probe, Every, Hold = was, wasEvery, wasHold })
	probe = func(string) bool { checks.Add(1); return up.Load() }
	Every, Hold = 0, time.Hour
	return &checks
}

// A warm session goes once the API can be reached. A cold one waits for
// proof: checks passing unbroken for Hold, and then only one goes first,
// or a real answer since the failure, and then every one goes.
func TestColdSessionsWaitForProof(t *testing.T) {
	var up atomic.Bool
	fake(t, &up)
	const api = "https://api.example"
	Fail(api, time.Now())
	if MayGo(api, "a", true) || MayGo(api, "a", false) {
		t.Fatal("nothing goes while it's down")
	}
	up.Store(true)
	if !MayGo(api, "a", true) {
		t.Fatal("a warm session goes once it's up")
	}
	if MayGo(api, "a", false) {
		t.Fatal("a cold session waits for it to hold")
	}
	Hold = 0
	time.Sleep(time.Millisecond)
	if !MayGo(api, "a", false) || MayGo(api, "b", false) {
		t.Fatal("once it holds, one cold session goes first, alone")
	}
	if !MayGo(api, "a", false) {
		t.Fatal("the one going first keeps the way")
	}
	Answer(api, time.Now())
	if !MayGo(api, "b", false) {
		t.Fatal("once one is answered, every cold session goes")
	}

	// Failing again sets every one back.
	Hold = time.Hour
	Fail(api, time.Now().Add(time.Millisecond))
	if MayGo(api, "b", false) {
		t.Fatal("a failure should undo the proof")
	}
	if p := Load(api); p.Canary != "" || p.Answering() {
		t.Fatalf("after a failure: %+v", p)
	}
}

// A check younger than Every is shared by everyone asking.
func TestChecksAreShared(t *testing.T) {
	var up atomic.Bool
	up.Store(true)
	checks := fake(t, &up)
	Every = time.Hour
	for range 5 {
		Check("https://api.example")
	}
	if n := checks.Load(); n != 1 {
		t.Fatalf("%d checks, want 1", n)
	}
}

func TestTarget(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	if got := Target(); got != "https://api.anthropic.com" {
		t.Fatalf("default %q", got)
	}
	if got := Target("FOO=1", "ANTHROPIC_BASE_URL=http://localhost:11434/v1"); got != "http://localhost:11434" {
		t.Fatalf("from env %q", got)
	}
}
