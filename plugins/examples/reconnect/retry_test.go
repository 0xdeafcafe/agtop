package main

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

func cfg() Config { return Config{Wait: 15 * time.Second, Tries: 3} }

func TestParseConfig(t *testing.T) {
	if c := ParseConfig(nil); c.Wait != 15*time.Second || c.Tries != 5 {
		t.Fatalf("defaults = %+v", c)
	}
	if c := ParseConfig(map[string]string{"wait": "60s", "tries": "8"}); c.Wait != time.Minute || c.Tries != 8 {
		t.Fatalf("parsed = %+v", c)
	}
	if c := ParseConfig(map[string]string{"wait": "soon", "tries": "0"}); c.Wait != 15*time.Second || c.Tries != 5 {
		t.Fatalf("bad values should fall back: %+v", c)
	}
}

func TestBackoffDoublesUpToTheCap(t *testing.T) {
	c := Config{Wait: 15 * time.Second}
	for n, want := range map[int]time.Duration{1: 15 * time.Second, 2: 30 * time.Second, 3: time.Minute, 4: 2 * time.Minute, 20: MaxDelay} {
		if got := c.Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestOnlyOfflineAndRetryableAreRetried(t *testing.T) {
	for _, kind := range []string{"limit", "auth", "other", "too-long", ""} {
		r := NewRetrier(cfg())
		r.Stopped("s1", "", kind, t0)
		r.Up(t0)
		if r.Waiting() != 0 || len(r.Due(t0.Add(time.Hour))) != 0 {
			t.Errorf("kind %q: would retry", kind)
		}
	}
	// A later stop of another kind cancels one waiting.
	r := NewRetrier(cfg())
	r.Stopped("s1", "", "retryable", t0)
	r.Stopped("s1", "", "limit", t0.Add(time.Second))
	if r.Waiting() != 0 {
		t.Fatal("a limit after a retryable error should stop retrying")
	}
}

func TestOfflineWaitsForTheNetwork(t *testing.T) {
	r := NewRetrier(cfg())
	r.Down()
	r.Stopped("s1", "fix bug", "offline", t0)
	if len(r.Due(t0.Add(time.Hour))) != 0 || !r.Next().IsZero() {
		t.Fatal("tried while the network was down")
	}
	up := t0.Add(2 * time.Hour)
	r.Up(up)
	d := r.Due(up)
	if len(d) != 1 || d[0].GiveUp || d[0].Session != "s1" || d[0].Try != 1 || d[0].Name != "fix bug" {
		t.Fatalf("on network.up, Due = %+v; want one continue for s1", d)
	}
}

func TestRetryableWaitsWhenTheNetworkIsUp(t *testing.T) {
	r := NewRetrier(cfg())
	r.Stopped("s1", "", "retryable", t0)
	if len(r.Due(t0.Add(14*time.Second))) != 0 {
		t.Fatal("continued before the wait")
	}
	if d := r.Due(t0.Add(15 * time.Second)); len(d) != 1 || d[0].GiveUp {
		t.Fatalf("after the wait, Due = %+v", d)
	}
}

func TestBacksOffThenGivesUp(t *testing.T) {
	r := NewRetrier(cfg()) // 3 tries, 15s base
	r.Down()
	r.Stopped("s1", "", "offline", t0)
	now := t0.Add(time.Minute)
	r.Up(now)
	var at []time.Duration
	for range 100 {
		next := r.Next()
		if next.IsZero() {
			break
		}
		now = next
		for _, d := range r.Due(now) {
			if d.GiveUp {
				if d.Try != 3 {
					t.Fatalf("gave up after %d tries, want 3", d.Try)
				}
				at = append(at, -now.Sub(t0))
			} else {
				at = append(at, now.Sub(t0))
			}
		}
	}
	// Continues at the network coming back, then 15s and 30s after;
	// giving up 60s after the last with no turn started.
	want := []time.Duration{time.Minute, 75 * time.Second, 105 * time.Second, -165 * time.Second}
	if len(at) != len(want) {
		t.Fatalf("actions at %v, want %v", at, want)
	}
	for i := range want {
		if at[i] != want[i] {
			t.Fatalf("actions at %v, want %v", at, want)
		}
	}
	if r.Waiting() != 0 {
		t.Fatal("still waiting after giving up")
	}
}

func TestTurnStartedStopsRetrying(t *testing.T) {
	r := NewRetrier(cfg())
	r.Stopped("s1", "", "retryable", t0)
	r.Due(t0.Add(15 * time.Second)) // try 1
	r.Started("s1")
	if r.Waiting() != 0 || len(r.Due(t0.Add(time.Hour))) != 0 {
		t.Fatal("kept retrying after the session went again")
	}
}

func TestStoppingAgainKeepsTheTriesSpent(t *testing.T) {
	r := NewRetrier(cfg())
	r.Stopped("s1", "", "retryable", t0)
	r.Due(t0.Add(15 * time.Second)) // try 1
	// The continue failed the same way: it waits the backoff for try 1
	// from now, and counts on from there.
	r.Stopped("s1", "", "retryable", t0.Add(20*time.Second))
	if got := r.Next(); !got.Equal(t0.Add(35 * time.Second)) {
		t.Fatalf("next = %v", got.Sub(t0))
	}
	if d := r.Due(t0.Add(35 * time.Second)); len(d) != 1 || d[0].Try != 2 {
		t.Fatalf("Due = %+v, want try 2", d)
	}
}

func TestNetworkDownPausesAndUpResumesAll(t *testing.T) {
	r := NewRetrier(cfg())
	r.Stopped("s1", "", "retryable", t0)
	r.Stopped("s2", "", "offline", t0)
	r.Down()
	if len(r.Due(t0.Add(time.Hour))) != 0 {
		t.Fatal("tried while down")
	}
	up := t0.Add(2 * time.Hour)
	r.Up(up)
	if d := r.Due(up); len(d) != 2 {
		t.Fatalf("on up, Due = %+v; want both", d)
	}
}
