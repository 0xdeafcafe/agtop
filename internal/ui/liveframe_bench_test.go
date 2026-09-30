package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/adapters/codex"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// withSubs gives the open session n finished subagent runs, as a long
// session collects.
func withSubs(m *Model, n int) {
	c := m.host
	c.subTails = map[string]*convo.Tail{}
	for i := range n {
		sa := convo.Subagent{ID: fmt.Sprintf("a%04d", i), Type: "Explore", Description: "look around the renderer", ToolUseID: fmt.Sprintf("tu%d", i)}
		c.subs = append(c.subs, sa)
		c.subTails[sa.ID] = convo.SubagentTail("")
	}
}

// BenchmarkLiveFrame is the whole frame while a turn is live in a long
// session: each a second's tick on (the spinner and the pulse move), or
// nothing new (the fast tick's frame), in each of the pane's views.
func BenchmarkLiveFrame(b *testing.B) {
	agent.NeverWait()
	time.Sleep(200 * time.Millisecond)
	for _, turns := range []int{300, 3000} {
		s := benchConvo(turns)
		for _, view := range []int{0, 1, 2} {
			m, _ := benchModel(250, 70)
			withSubs(m, 200)
			m.host.sess, m.host.bodyBuf, m.host.view = s, nil, view
			m.View()
			name := fmt.Sprintf("%s/%dturns", m.viewName(m.host), turns)
			b.Run(name+"/tick", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					m.tick++
					m.View()
				}
			})
			b.Run(name+"/fast", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					m.View()
				}
			})
		}
	}
}

// BenchmarkTabStat is the stat beside each of the pane's tabs, per frame.
func BenchmarkTabStat(b *testing.B) {
	m, _ := benchModel(250, 70)
	c := m.host
	c.sess.Info.Cwd = b.TempDir()
	for _, v := range []string{"overview", "changes"} {
		b.Run(v, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tabStat(c, v)
			}
		})
	}
}

// BenchmarkListMany is the list alone with many agents on it.
func BenchmarkListMany(b *testing.B) {
	agent.NeverWait()
	for _, n := range []int{30, 300} {
		m, _ := benchModel(250, 70)
		now := time.Now()
		for i := len(m.snap.Agents); i < n; i++ {
			a := &fleet.Agent{Key: fmt.Sprintf("default/b:%d", i), DisplayName: fmt.Sprintf("agent number %d doing things", i), Acct: m.snap.Agents[0].Acct}
			a.ID, a.Cwd, a.Repo, a.Branch = fmt.Sprintf("%08x", i+1000), "/work/rush", "/work/rush", "main"
			a.UpdatedAt, a.State = now.Add(-time.Duration(i)*time.Minute), "done"
			m.snap.Agents = append(m.snap.Agents, a)
		}
		m.rebuild()
		m.preview, m.host = false, nil
		m.View()
		b.Run(fmt.Sprintf("%dagents", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				m.View()
			}
		})
	}
}

// benchPaths are RUSH_BENCH_TRANSCRIPT's transcripts, comma-separated:
// Claude Code's, or Codex rollouts (under .codex). Local only.
func benchPaths(b *testing.B) []string {
	env := os.Getenv("RUSH_BENCH_TRANSCRIPT")
	if env == "" {
		b.Skip("RUSH_BENCH_TRANSCRIPT names transcripts")
	}
	agent.NeverWait()
	return strings.Split(env, ",")
}

// benchName is a transcript's name in a benchmark's: its id's start and size.
func benchName(path string) string {
	fi, _ := os.Stat(path)
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	base = base[max(0, len(base)-8):]
	return fmt.Sprintf("%s-%dMB", base, fi.Size()>>20)
}

// openBench opens a transcript as the pane does (openTail, openHistory)
// with its subagents' rows read, its last turn live, on a model at 250x70;
// subs is how long the rows took.
func openBench(path string) (m *Model, subs time.Duration) {
	m, _ = benchModel(250, 70)
	c := m.host
	if strings.Contains(path, "/.codex/") {
		c.sess = agentHistory(codex.Kind, agent.Session{Transcript: path}, time.Time{})
	} else {
		t := convo.NewTail(path)
		_, _ = t.Read()
		c.sess, c.path = t.Sess, path
		t0 := time.Now()
		defer func() { subs = time.Since(t0) }()
		c.subs = convo.ListSubagents(path)
		if showView(m, "subagents") { // every run's row is read
			if msg, ok := m.readUnread(c)().(subStatsMsg); ok {
				c.subTails = msg.tails
			}
		}
		c.view = 0
	}
	if n := len(c.sess.Turns); n > 0 {
		c.sess.Turns[n-1].Live = true
	}
	c.bodyBuf = nil
	return m, 0
}

// showView turns the pane to the named view, false when it has none.
func showView(m *Model, name string) bool {
	c := m.host
	i := slices.Index(m.views(c), name)
	c.view, c.scroll = max(i, 0), 0
	return i >= 0
}

// BenchmarkTranscriptLoad is opening a real transcript cold, up to its
// first frame: read and built off the UI's goroutine and drawn once there
// (warmed), then the first frame on it. Run with -benchtime=1x.
func BenchmarkTranscriptLoad(b *testing.B) {
	for _, path := range benchPaths(b) {
		b.Run(benchName(path), func(b *testing.B) {
			var ms0, ms1 runtime.MemStats
			for b.Loop() {
				runtime.GC()
				runtime.ReadMemStats(&ms0)
				t0 := time.Now()
				m, subs := openBench(path)
				read := time.Since(t0) - subs
				_, paneW, _ := m.layout() // as syncHost warms it
				m.host.sess.Render(convo.Options{Width: paneW - 3, Now: time.Now(), Open: map[string]bool{}, Focused: m.paneFocus, Wide: m.hostedAlone()})
				warm := time.Since(t0) - read - subs
				t1 := time.Now()
				m.View()
				first := time.Since(t1)
				runtime.GC()
				runtime.ReadMemStats(&ms1)
				b.ReportMetric(float64(read.Milliseconds()), "read-ms")
				b.ReportMetric(float64(subs.Milliseconds()), "subrows-ms")
				b.ReportMetric(float64(warm.Milliseconds()), "warm-ms")
				b.ReportMetric(float64(first.Microseconds())/1000, "firstframe-ms")
				b.ReportMetric(float64(ms1.TotalAlloc-ms0.TotalAlloc)/(1<<20), "alloc-MB")
				b.ReportMetric(float64(int64(ms1.HeapInuse)-int64(ms0.HeapInuse))/(1<<20), "heap-MB")
				b.ReportMetric(float64(len(m.host.sess.Turns)), "turns")
				b.ReportMetric(float64(len(m.host.subs)), "subagents")
				runtime.KeepAlive(m)
			}
		})
	}
}

// BenchmarkTranscriptFrame is BenchmarkLiveFrame over real transcripts,
// each view's frame on the second's tick and on a fast one.
func BenchmarkTranscriptFrame(b *testing.B) {
	for _, path := range benchPaths(b) {
		m, _ := openBench(path)
		at := time.Now()
		paneNow = func() time.Time { return at }
		for _, view := range []string{"conversation", "overview", "changes", "subagents"} {
			if !showView(m, view) {
				continue
			}
			m.View()
			b.Run(benchName(path)+"/"+view+"/tick", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					m.tick++
					at = at.Add(time.Second)
					m.View()
				}
			})
			b.Run(benchName(path)+"/"+view+"/fast", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					at = at.Add(100 * time.Millisecond)
					m.View()
				}
				if view == "conversation" && !m.host.sess.Fast {
					b.Log("no fast frames asked for: nothing on the clock")
				}
			})
		}
		paneNow = time.Now
	}
}

// BenchmarkTranscriptScroll pages up a real conversation to its top and
// down again, a frame a key, and reports the mean and slowest frame. Also
// the first frame of each other view opened. Run with -benchtime=1x.
func BenchmarkTranscriptScroll(b *testing.B) {
	up, down := tea.KeyPressMsg{Code: tea.KeyPgUp}, tea.KeyPressMsg{Code: tea.KeyPgDown}
	for _, path := range benchPaths(b) {
		m, _ := openBench(path)
		m.View()
		b.Run(benchName(path)+"/pages", func(b *testing.B) {
			for b.Loop() {
				c := m.host
				var total, slowest time.Duration
				frames := 0
				frame := func(k tea.KeyPressMsg) {
					t := time.Now()
					m.Update(k)
					m.View()
					d := time.Since(t)
					total, slowest, frames = total+d, max(slowest, d), frames+1
				}
				for prev := -1; c.scroll != prev; {
					prev = c.scroll
					frame(up)
				}
				for c.scroll > 0 {
					frame(down)
				}
				b.ReportMetric(float64(frames), "frames")
				b.ReportMetric(float64(total.Microseconds())/float64(frames)/1000, "mean-ms")
				b.ReportMetric(float64(slowest.Microseconds())/1000, "max-ms")
			}
		})
		for _, view := range []string{"overview", "subagents", "changes"} {
			b.Run(benchName(path)+"/open-"+view, func(b *testing.B) {
				for b.Loop() {
					if !showView(m, view) {
						b.Skip("no " + view)
					}
					t := time.Now()
					m.View()
					b.ReportMetric(float64(time.Since(t).Microseconds())/1000, "ms")
					showView(m, "conversation")
					m.View()
				}
			})
		}
	}
}

// BenchmarkRebuild is what a fleet reading costs the UI when it lands
// (snapMsg): the list sorted and grouped again, with many agents.
func BenchmarkRebuild(b *testing.B) {
	for _, n := range []int{30, 300} {
		m, _ := benchModel(250, 70)
		now := time.Now()
		for i := len(m.snap.Agents); i < n; i++ {
			a := &fleet.Agent{Key: fmt.Sprintf("default/b:%d", i), DisplayName: fmt.Sprintf("agent number %d doing things", i), Acct: m.snap.Agents[0].Acct}
			a.ID, a.Cwd, a.Repo, a.Branch = fmt.Sprintf("%08x", i+1000), "/work/rush", "/work/rush", "main"
			a.UpdatedAt, a.State = now.Add(-time.Duration(i)*time.Minute), "done"
			m.snap.Agents = append(m.snap.Agents, a)
		}
		b.Run(fmt.Sprintf("%dagents", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.rebuild()
			}
		})
	}
}
