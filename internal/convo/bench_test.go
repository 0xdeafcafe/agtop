package convo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

type timed struct {
	at time.Time
	ev any
}

// benchEvents is a long session: n finished turns of narration, reads,
// greps, commands with output, edits and a markdown answer, then a live
// turn part way through streaming its answer.
func benchEvents(n int) []timed {
	var out []timed
	sec := 0
	add := func(ev any) {
		out = append(out, timed{at(sec), ev})
		sec++
	}
	var stdout strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&stdout, "ok  \tgithub.com/x/y/pkg%d\t0.%03ds\n", i, i*7)
	}
	answer := "## What changed\n\nThe **renderer** now caches each turn, so `Render` only redraws what moved.\n\n" +
		"- the live turn redraws per frame\n- finished turns come from the cache\n- see https://example.com/docs/render for more\n\n" +
		"1. build it\n2. run the tests\n\n```go\nfunc f() { return }\n```\n\nThat should make streaming feel instant on long sessions, even at wide widths where every row is padded."
	for t := 1; t <= n; t++ {
		id := func(k string) string { return fmt.Sprintf("%s%d", k, t) }
		add(host.Sent{Text: fmt.Sprintf("turn %d: look at /work/agtop/internal/ui/view.go and fix the @internal/convo/render.go wrap, then run /review https://example.com/x", t)})
		add(headless.Message{Role: "assistant", ID: id("m"), Model: "claude-opus-5-5", Usage: &headless.Usage{InputTokens: 10, OutputTokens: 200, CacheReadInputTokens: 50000},
			Blocks: []headless.Block{{Type: "text", Text: "Looking at how the **pane** draws its `rows` and where the wrap happens; the renderer is in render.go."}}})
		add(toolUse(id("r"), "Read", map[string]any{"file_path": "/work/agtop/internal/ui/view.go"}))
		add(toolResult(id("r"), "…", false, map[string]any{"type": "text", "file": map[string]any{"numLines": 400, "startLine": 1, "totalLines": 1589}}))
		add(toolUse(id("g"), "Grep", map[string]any{"pattern": "func wrap", "path": "/work/agtop/internal"}))
		add(toolResult(id("g"), "internal/ui/style.go\ninternal/convo/style.go", false, nil))
		add(toolUse(id("b"), "Bash", map[string]any{"command": "cd /work/agtop && go test ./... 2>&1 | tail -40", "description": "Run the tests"}))
		add(toolResult(id("b"), stdout.String(), false, map[string]any{"stdout": stdout.String(), "stderr": ""}))
		add(toolUse(id("e"), "Edit", map[string]any{"file_path": "/work/agtop/internal/convo/render.go"}))
		add(toolResult(id("e"), "ok", false, map[string]any{"structuredPatch": []map[string]any{{"oldStart": 60, "oldLines": 3, "newStart": 60, "newLines": 4,
			"lines": []string{" \tfor _, t := range s.Turns {", "-\t\tout = append(out, s.turn(t, o)...)", "+\t\tls := s.turn(t, o)", "+\t\tout = append(out, ls...)", " \t}"}}}}))
		add(toolUse(id("f"), "Bash", map[string]any{"command": "go vet ./..."}))
		add(toolResult(id("f"), "Exit code 1\ninternal/ui/editor.go:41:2: unreachable code", true, map[string]any{"stdout": "", "stderr": "internal/ui/editor.go:41:2: unreachable code"}))
		add(headless.Message{Role: "assistant", ID: id("a"), Model: "claude-opus-5-5", Blocks: []headless.Block{{Type: "text", Text: answer}}})
		add(headless.Result{Subtype: "success", CostUSD: 0.42})
	}
	add(host.Sent{Text: "now make streaming quicker"})
	add(toolUse("lr", "Read", map[string]any{"file_path": "/work/agtop/internal/ui/agtopmode.go"}))
	add(toolResult("lr", "…", false, map[string]any{"type": "text", "file": map[string]any{"numLines": 400, "startLine": 1, "totalLines": 1765}}))
	for _, w := range strings.Fields(answer) {
		add(headless.Delta{Text: w + " "})
	}
	return out
}

func benchSession(n int) *Session {
	s := New()
	s.Info.Cwd = "/work/agtop"
	for _, e := range benchEvents(n) {
		s.Apply(e.ev, e.at)
	}
	return s
}

// realTranscript is a real Claude Code transcript for local benchmarks:
// $AGTOP_BENCH_TRANSCRIPT, else the largest under 9MB in ~/.claude/projects.
func realTranscript(b *testing.B) string {
	if p := os.Getenv("AGTOP_BENCH_TRANSCRIPT"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	paths, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	type f struct {
		p string
		n int64
	}
	var fs []f
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Size() < 9<<20 {
			fs = append(fs, f{p, st.Size()})
		}
	}
	if len(fs) == 0 {
		b.Skip("no transcript in ~/.claude/projects")
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].n > fs[j].n })
	return fs[0].p
}

func BenchmarkApply(b *testing.B) {
	evs := benchEvents(300)
	b.ReportAllocs()
	for b.Loop() {
		s := New()
		for _, e := range evs {
			s.Apply(e.ev, e.at)
		}
	}
}

func BenchmarkTailReal(b *testing.B) {
	path := realTranscript(b)
	st, _ := os.Stat(path)
	b.SetBytes(st.Size())
	b.ReportAllocs()
	for b.Loop() {
		if _, err := NewTail(path).Read(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTailIncremental appends one real line at a time and reads it,
// the way a followed transcript grows.
func BenchmarkTailIncremental(b *testing.B) {
	data, err := os.ReadFile(realTranscript(b))
	if err != nil {
		b.Fatal(err)
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	path := filepath.Join(b.TempDir(), "t.jsonl")
	var f *os.File
	var t *Tail
	i := 0
	reset := func() {
		if f != nil {
			f.Close()
		}
		f, _ = os.Create(path)
		t, i = NewTail(path), 0
	}
	reset()
	b.ReportAllocs()
	for b.Loop() {
		if i == len(lines) {
			b.StopTimer()
			reset()
			b.StartTimer()
		}
		f.Write(lines[i])
		i++
		if _, err := t.Read(); err != nil {
			b.Fatal(err)
		}
	}
	f.Close()
}

// BenchmarkTailIdle is a Read with nothing new: what every followed
// transcript costs on each tick.
func BenchmarkTailIdle(b *testing.B) {
	t := NewTail(realTranscript(b))
	t.Read()
	b.ReportAllocs()
	for b.Loop() {
		t.Read()
	}
}

func BenchmarkRender(b *testing.B) {
	for _, w := range []int{120, 250} {
		o := Options{Width: w, Now: at(100000), Open: map[string]bool{}}
		b.Run(fmt.Sprintf("w%d/cold", w), func(b *testing.B) {
			s := benchSession(300)
			b.ReportAllocs()
			for b.Loop() {
				s.cache = map[*Turn]cached{}
				s.Render(o)
			}
		})
		b.Run(fmt.Sprintf("w%d/warm", w), func(b *testing.B) {
			s := benchSession(300)
			s.Render(o)
			b.ReportAllocs()
			for b.Loop() {
				s.Render(o)
			}
		})
		// A delta arrives and a frame is drawn: the streaming hot path.
		b.Run(fmt.Sprintf("w%d/stream", w), func(b *testing.B) {
			s := benchSession(300)
			s.Render(o)
			o := o
			i := 0
			b.ReportAllocs()
			base := s.streaming.Text
			for b.Loop() {
				i++
				// Keep the answer a realistic length: it restarts at 4KB.
				if len(s.streaming.Text) > 4<<10 {
					s.streaming.Text = base
				}
				s.Apply(headless.Delta{Text: "more words arrive "}, o.Now)
				o.Tick = i
				s.Render(o)
			}
		})
	}
}

func BenchmarkRenderReal(b *testing.B) {
	t := NewTail(realTranscript(b))
	if _, err := t.Read(); err != nil {
		b.Fatal(err)
	}
	for _, w := range []int{120, 250} {
		o := Options{Width: w, Now: time.Now(), Open: map[string]bool{}}
		b.Run(fmt.Sprintf("w%d/cold", w), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				t.Sess.cache = map[*Turn]cached{}
				t.Sess.Render(o)
			}
		})
	}
}

func BenchmarkViews(b *testing.B) {
	s := benchSession(300)
	o := Options{Width: 180, Now: at(100000), Open: map[string]bool{}}
	b.Run("overview", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.Overview(o)
		}
	})
	b.Run("recentEdits", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.RecentEdits(Options{Width: 56, Now: o.Now}, 60)
		}
	})
	b.Run("search", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.SearchView("wrap", o)
		}
	})
	b.Run("totals", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.Totals(o.Now)
			s.Pending()
			s.LastWords()
		}
	})
}

func BenchmarkHelpers(b *testing.B) {
	styled := paint(cSub, inline("Looking at how the **pane** draws its `rows` and where the wrap happens; see https://example.com/x for the renderer in render.go and the rest.", cSub))
	b.Run("row", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			row(bgWell, " "+styled, dim("12 steps   4m 03s"), 250, 124)
		}
	})
	b.Run("wrap", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			wrap(styled, 60)
		}
	})
	b.Run("inline", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			inline("Looking at how the **pane** draws its `rows` and where the wrap happens.", cSub)
		}
	})
	b.Run("stripANSI", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			stripANSI(styled)
		}
	})
	b.Run("styledAsk", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			styledAsk("look at /work/agtop/internal/ui/view.go and fix @internal/convo/render.go then /review https://example.com/x", cText)
		}
	})
}

// BenchmarkStreamLong streams a 200KB answer in 20-byte pieces.
func BenchmarkStreamLong(b *testing.B) {
	piece := strings.Repeat("x", 19) + " "
	for b.Loop() {
		s := New()
		s.Apply(host.Sent{Text: "go"}, t0)
		for range 10000 {
			s.Apply(headless.Delta{Text: piece}, t0)
		}
	}
}
