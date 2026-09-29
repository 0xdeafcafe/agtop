package convo

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A running turn drawn from the unit memo reads exactly as one drawn anew:
// frame after frame, as text streams in, verbose, with a step picked and a
// run opened. $RUSH_BENCH_TRANSCRIPT adds a real session to the check.
func TestUnitMemoDrawsTheSame(t *testing.T) {
	sessions := map[string]*Session{"bench": benchSession(200)}
	if p := os.Getenv("RUSH_BENCH_TRANSCRIPT"); p != "" {
		sessions[p] = settledTranscript(t, p)
	}
	for name, s := range sessions {
		s.Turns[len(s.Turns)-1].Live = true
		for _, w := range []int{80, 120, 250} {
			for _, v := range []string{"plain", "verbose", "picked"} {
				o := Options{Width: w, Now: time.Unix(1e9, 0), Open: map[string]bool{}, Verbose: v == "verbose"}
				if v == "picked" {
					pickInLive(s, &o)
				}
				if msg := memoFrames(s, o); msg != "" {
					t.Fatalf("%s w%d %s: %s", name, w, v, msg)
				}
			}
		}
	}
}

// settledTranscript reads a transcript, and lets the git lookups for its
// commit cards finish: until they do, two draws of it can differ.
func settledTranscript(t *testing.T, p string) *Session {
	tl := NewTail(p)
	if _, err := tl.Read(); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		g := lookupsGen.Load()
		tl.Sess.Render(Options{Width: 120, Now: time.Unix(1e9, 0)})
		time.Sleep(300 * time.Millisecond)
		if lookupsGen.Load() == g {
			break
		}
	}
	return tl.Sess
}

// pickInLive selects a step halfway through the running turn and opens
// its folded runs.
func pickInLive(s *Session, o *Options) {
	live := "t" + strconv.Itoa(s.Turns[len(s.Turns)-1].N) + ":"
	var steps []string
	for _, l := range s.Render(*o) {
		switch {
		case !strings.HasPrefix(l.Ref, live):
		case strings.Contains(l.Ref, ":run:"):
			o.Open[l.Ref] = true
		case strings.Contains(l.Ref, ":s:"):
			steps = append(steps, l.Ref)
		}
	}
	if len(steps) > 0 {
		o.Selected, o.Focused = steps[len(steps)/2], true
	}
}

// memoFrames draws three frames from a cold memo, text streaming into the
// last, each checked against the same frame drawn anew; it says how the
// first that differs does.
func memoFrames(s *Session, o Options) string {
	live := s.Turns[len(s.Turns)-1]
	s.cache = map[*Turn]cached{}
	for _, it := range live.Items {
		it.drawn = nil
	}
	defer func(items []*Item) { live.Items = items }(live.Items)
	for f := range 3 {
		o.Tick = f
		if f == 2 {
			it := &Item{Kind: KText}
			it.grow("some words streaming in")
			live.Items = append(live.Items, it)
			live.ver++
		}
		got := append([]Line(nil), s.Render(o)...)
		noUnitMemo = true
		cache := s.cache
		s.cache = map[*Turn]cached{}
		want := s.Render(o)
		s.cache, noUnitMemo = cache, false
		if len(got) != len(want) {
			return "frame " + strconv.Itoa(f) + ": " + strconv.Itoa(len(got)) + " rows, want " + strconv.Itoa(len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				return "frame " + strconv.Itoa(f) + " row " + strconv.Itoa(i) + ":\n got " + strconv.Quote(got[i].Text) + "\nwant " + strconv.Quote(want[i].Text)
			}
		}
	}
	return ""
}

// A session laid out for a new width within a budget shows some of it as
// it was, says so, and render by render comes to exactly what one render
// with no budget draws.
func TestBudgetRelayoutSettles(t *testing.T) {
	s := benchSession(200)
	if p := os.Getenv("RUSH_BENCH_TRANSCRIPT"); p != "" {
		s = settledTranscript(t, p)
	}
	s.Turns[len(s.Turns)-1].Live = true
	o := Options{Width: 120, Now: time.Unix(1e9, 0), Open: map[string]bool{}}
	s.Render(o)
	o.Width, o.Budget = 97, time.Nanosecond
	n := 0
	var got []Line
	for got = append([]Line(nil), s.Render(o)...); s.Stale(); got = append([]Line(nil), s.Render(o)...) {
		if n++; n > 10000 {
			t.Fatal("never settled")
		}
	}
	if n == 0 {
		t.Fatal("a nanosecond's budget drew it all at once")
	}
	o.Budget = 0
	s.cache = map[*Turn]cached{}
	noUnitMemo = true
	want := s.Render(o)
	noUnitMemo = false
	if len(got) != len(want) {
		t.Fatalf("settled after %d renders with %d rows, want %d", n, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("row %d:\n got %q\nwant %q", i, got[i].Text, want[i].Text)
		}
	}
}
