package convo

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/host"
)

// liveAgentSession is a long session whose live turn runs a subagent that
// has taken kids steps, each printing lines, and is running one more.
func liveAgentSession(turns, kids, lines int) (*Session, time.Time) {
	s := benchSession(turns)
	sec := 200000
	now := func() time.Time { sec++; return at(sec) }
	s.Apply(headless.Delta{Text: "\n"}, now())
	s.Apply(headless.Result{Subtype: "success"}, now())
	s.Apply(host.Sent{Text: "go and look"}, now())
	s.Apply(toolUse("ag", "Agent", map[string]any{"subagent_type": "Explore", "prompt": "look", "description": "look around"}), now())
	var out strings.Builder
	for i := range lines {
		fmt.Fprintf(&out, "internal/ui/view.go:%d:\tfunc f%d() { return wrap(s, %d) }\n", i+1, i, i)
	}
	sub := func(m headless.Message) headless.Message { m.ParentToolUseID = "ag"; return m }
	for k := range kids {
		id := fmt.Sprintf("k%d", k)
		s.Apply(sub(toolUse(id, "Bash", map[string]any{"command": fmt.Sprintf("rg -n wrap internal/ui | head -%d", lines)})), now())
		s.Apply(sub(toolResult(id, out.String(), false, map[string]any{"stdout": out.String(), "stderr": ""})), now())
	}
	t := now()
	s.Apply(sub(toolUse("run", "Bash", map[string]any{"command": "go test ./..."})), t)
	return s, t
}

// BenchmarkLiveTickAgents is a frame of a live turn running five
// subagents at once, each 150 steps in, the latest of each just done with
// 2000 lines printed: the second's tick redraws the turn.
func BenchmarkLiveTickAgents(b *testing.B) {
	s := benchSession(300)
	start := at(200000)
	s.Apply(host.Sent{Text: "fan out"}, start)
	var out strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&out, "internal/ui/view.go:%d:\tfunc f%d() { return wrap(s, %d) }\n", i+1, i, i)
	}
	for a := range 5 {
		ag := fmt.Sprintf("ag%d", a)
		s.Apply(toolUse(ag, "Agent", map[string]any{"subagent_type": "Explore", "prompt": "look", "description": "look around"}), start)
		for k := range 150 {
			id := fmt.Sprintf("%s-k%d", ag, k)
			use, res := toolUse(id, "Bash", map[string]any{"command": "rg -n wrap internal/ui"}), toolResult(id, out.String(), false, map[string]any{"stdout": out.String(), "stderr": ""})
			use.ParentToolUseID, res.ParentToolUseID = ag, ag
			s.Apply(use, start)
			s.Apply(res, start)
		}
	}
	o := Options{Width: 153, Now: start.Add(time.Minute), Open: map[string]bool{}}
	s.Render(o)
	b.ReportAllocs()
	for b.Loop() {
		o.Tick++
		s.Render(o)
	}
}

// BenchmarkLiveTick is a frame of a live turn with nothing new in it: the
// fast tick's (the clock a tenth on) and the second's (the spinner on).
func BenchmarkLiveTick(b *testing.B) {
	for _, kids := range []int{10, 80} {
		s, t := liveAgentSession(300, kids, 400)
		o := Options{Width: 153, Now: t.Add(time.Second), Open: map[string]bool{}}
		s.Render(o)
		b.Run(fmt.Sprintf("fast/%dkids", kids), func(b *testing.B) {
			o := o
			b.ReportAllocs()
			for b.Loop() {
				o.Now = o.Now.Add(100 * time.Millisecond)
				if o.Now.Sub(t) > 4*time.Second {
					o.Now = t.Add(time.Second)
				}
				s.Render(o)
			}
		})
		b.Run(fmt.Sprintf("tick/%dkids", kids), func(b *testing.B) {
			o := o
			b.ReportAllocs()
			for b.Loop() {
				o.Tick++
				s.Render(o)
			}
		})
	}
}

// A fast frame redraws the timer's tenths, and keeps asking for the next.
func TestFastFrameRedraws(t *testing.T) {
	s, start := liveAgentSession(2, 2, 5)
	o := Options{Width: 153, Now: start.Add(1200 * time.Millisecond), Open: map[string]bool{}}
	a := s.Render(o)
	if !s.Fast {
		t.Fatal("a timer under 5s should ask for fast frames")
	}
	was := a[len(a)-3].Text
	o.Now = o.Now.Add(300 * time.Millisecond)
	bs := s.Render(o)
	if !s.Fast {
		t.Fatal("a fast frame should keep asking for fast frames")
	}
	found := false
	for _, l := range bs {
		if strings.Contains(stripANSI(l.Text), "1.5s") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the timer should read 1.5s; last rows were %q", was)
	}
}

// A working subagent's steps drawn from the memo are as drawn anew, as
// the one running finishes and another starts, and with one selected.
func TestSubagentStepsMemo(t *testing.T) {
	s, start := liveAgentSession(3, 6, 20)
	o := Options{Width: 153, Now: start.Add(2 * time.Second), Open: map[string]bool{}}
	if d := memoFrames(s, o); d != "" {
		t.Fatal(d)
	}
	sub := func(m headless.Message) headless.Message { m.ParentToolUseID = "ag"; return m }
	s.Apply(sub(toolResult("run", "ok\nFAIL x", true, map[string]any{"stdout": "ok\nFAIL x", "stderr": ""})), start.Add(3*time.Second))
	s.Apply(sub(toolUse("run2", "Read", map[string]any{"file_path": "/work/rush/x.go"})), start.Add(4*time.Second))
	o.Now = start.Add(5 * time.Second)
	if d := memoFrames(s, o); d != "" {
		t.Fatal("after one finished: " + d)
	}
	o.Selected, o.Focused = s.Turns[len(s.Turns)-1].ref+":s:k2", true
	if d := memoFrames(s, o); d != "" {
		t.Fatal("with one selected: " + d)
	}
}
