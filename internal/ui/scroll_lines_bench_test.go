package ui

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// BenchmarkScrollLines is scrolling a synthetic session of 100, 10k and 1M
// rows, every turn open: a wheel tick and its frame, a screen at a time
// from the end to the top, Home and End, and a minimap drag. Each should
// cost the same at every length. The heap is the session's and, on top,
// what drawing it keeps.
func BenchmarkScrollLines(b *testing.B) {
	agent.NeverWait()
	perTurn := func() int {
		s := benchConvo(20)
		return max(1, len(s.Render(convo.Options{Width: 238, Now: time.Now(), Open: map[string]bool{}}))/21)
	}()
	for _, lines := range []int{100, 10_000, 1_000_000} {
		m, _ := benchModel(250, 70)
		c := m.host
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		before := ms.HeapAlloc
		c.sess, c.bodyBuf, c.view = benchConvo(max(1, lines/perTurn)), nil, 0
		// benchConvo's clock runs a second an event, days past now at 1M rows:
		// a long session's turns ended long ago.
		for _, t := range c.sess.Turns[:len(c.sess.Turns)-1] {
			t.End = time.Now().Add(-24 * time.Hour)
		}
		runtime.GC()
		runtime.ReadMemStats(&ms)
		session := ms.HeapAlloc - before
		t0 := time.Now()
		for m.View(); c.sess.Stale(); m.View() {
		}
		first := time.Since(t0)
		name := fmt.Sprintf("%dlines", lines)
		b.Run(name+"/wheel", func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				btn := tea.MouseWheelUp
				if i/200%2 == 1 {
					btn = tea.MouseWheelDown
				}
				i++
				m.Update(tea.MouseWheelMsg{X: 245, Y: 35, Button: btn})
				m.View()
			}
		})
		// A frame that isn't only the wheel's, as a clock tick or a lookup
		// landing has: the body is drawn again, from the turns kept.
		b.Run(name+"/frame", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				c.scrollOnly = false
				m.View()
			}
		})
		b.Run(name+"/scrollthrough", func(b *testing.B) {
			var steps int
			var total, slowest time.Duration
			for b.Loop() {
				c.scroll, c.scrollOnly = 0, true
				m.View()
				for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
					prev := c.scroll
					c.scroll, c.scrollOnly = c.scroll+c.bodyRows, true
					t := time.Now()
					m.View()
					d := time.Since(t)
					steps, total, slowest = steps+1, total+d, max(slowest, d)
					if c.scroll == prev {
						break
					}
				}
			}
			b.ReportMetric(float64(total.Microseconds())/float64(max(1, steps)), "µs/step")
			b.ReportMetric(float64(slowest.Microseconds()), "max-µs")
			b.ReportMetric(float64(steps)/float64(b.N), "steps")
		})
		b.Run(name+"/home-end", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				c.scroll, c.scrollOnly = 1<<30, true
				m.View()
				c.scroll, c.scrollOnly = 0, true
				m.View()
			}
		})
		b.Run(name+"/minimap-seek", func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				i = (i + 7919) % 1000
				m.seekMinimap(c, c.shownTotal*i/1000)
				m.View()
			}
		})
		runtime.GC()
		runtime.ReadMemStats(&ms)
		b.Logf("%s: %d turns, first frame %v, heap: session %.1f MiB, drawing %.1f MiB", name, len(c.sess.Turns), first.Round(time.Millisecond),
			float64(session)/(1<<20), (float64(ms.HeapAlloc)-float64(before)-float64(session))/(1<<20))
		runtime.KeepAlive(m)
	}
}
