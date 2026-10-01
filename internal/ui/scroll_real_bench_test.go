package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// BenchmarkScrollReal is the wheel over a whole real transcript
// (RUSH_BENCH_TRANSCRIPT), every turn open, once its lookups have landed:
// a wheel tick and its frame, and a clock tick's frame, which redraws.
func BenchmarkScrollReal(b *testing.B) {
	for _, path := range benchPaths(b) {
		m, _ := benchModel(250, 70)
		c := m.host
		c.sess, c.bodyBuf, c.view = convo.History(path, time.Time{}), nil, 0
		if n := len(c.sess.Turns); n > 0 {
			c.sess.Turns[n-1].Live = true
		}
		at := time.Now()
		paneNow = func() time.Time { return at }
		t0, calm := time.Now(), 0
		for calm < 20 && time.Since(t0) < 20*time.Second { // commit cards, file heads land
			m.tick++
			m.View()
			if calm++; c.sess.Stale() {
				calm = 0
			}
			time.Sleep(20 * time.Millisecond)
		}
		b.Logf("%s: %d turns, %d rows, settled in %v", benchName(path), len(c.sess.Turns), c.shownTotal, time.Since(t0).Round(time.Millisecond))
		b.Run(benchName(path)+"/wheel", func(b *testing.B) {
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
		// A lookup lands every tenth wheel tick, as commit cards, file heads
		// and the list's names do in a busy fleet (SetPeers moves the gen),
		// and the clock's frame after it draws the body again.
		b.Run(benchName(path)+"/wheel+lookups", func(b *testing.B) {
			b.ReportAllocs()
			i, stale := 0, 0
			for b.Loop() {
				btn := tea.MouseWheelUp
				if i/200%2 == 1 {
					btn = tea.MouseWheelDown
				}
				if i%10 == 0 { // and its frame, which draws the body again
					convo.SetPeers(map[string]string{"bench": strconv.Itoa(i)})
					m.tick++
					c.scrollOnly = false // as Update has it for anything but the wheel
					m.View()
				}
				i++
				m.Update(tea.MouseWheelMsg{X: 245, Y: 35, Button: btn})
				m.View()
				if c.sess.Stale() {
					stale++
				}
			}
			b.ReportMetric(float64(stale)/float64(i), "stale/op")
		})
		b.Run(benchName(path)+"/tick", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				at = at.Add(time.Second)
				c.scrollOnly = false
				m.View()
			}
		})
		paneNow = time.Now
	}
}

// BenchmarkScrollScale is a wheel tick and its frame over sessions of 50,
// 500 and 2000 turns, every turn open: it should cost the same at each.
func BenchmarkScrollScale(b *testing.B) {
	agent.NeverWait()
	for _, turns := range []int{50, 500, 2000} {
		m, _ := benchModel(250, 70)
		c := m.host
		c.sess, c.bodyBuf, c.view = benchConvo(turns), nil, 0
		for m.View(); c.sess.Stale(); m.View() {
		}
		b.Run(fmt.Sprintf("%dturns/wheel", turns), func(b *testing.B) {
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
			b.ReportMetric(float64(c.shownTotal), "rows")
		})
		b.Run(fmt.Sprintf("%dturns/frame", turns), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				c.scrollOnly = false
				m.View()
			}
		})
	}
}

// BenchmarkExpandAll is "open all" over a long session read from its
// middle: the frames until it's all drawn, the slowest, and the rows the
// row at the top of the view moved (0 when it's held in place).
func BenchmarkExpandAll(b *testing.B) {
	agent.NeverWait()
	for _, turns := range []int{500, 2000} {
		m, _ := benchModel(250, 70)
		c := m.host
		c.sess, c.bodyBuf, c.view = benchConvo(turns), nil, 0
		frame := func() time.Duration {
			t := time.Now()
			c.scrollOnly = false
			m.View()
			return time.Since(t)
		}
		settle := func() (n int, total, slowest time.Duration) {
			for n = 1; ; n++ {
				d := frame()
				total, slowest = total+d, max(slowest, d)
				if !c.sess.Stale() || n > 2000 {
					return n, total, slowest
				}
			}
		}
		b.Run(fmt.Sprintf("%dturns", turns), func(b *testing.B) {
			var frames, moved int
			var total, slowest time.Duration
			for b.Loop() {
				m.setHistoryFold(false)
				settle()
				c.scroll = c.shownTotal / 2
				settle()
				ref, off := c.top.ref, c.top.off // the row at the top: so far into what
				m.setHistoryFold(true)
				n, t, s := settle()
				frames, total, slowest = frames+n, total+t, max(slowest, s)
				at := rowOf(c.shown, c.shownBase, ref, c.sess, true)
				moved += abs(at + off - (c.shownTotal - c.scroll - c.bodyRows))
			}
			b.ReportMetric(float64(frames)/float64(b.N), "frames")
			b.ReportMetric(float64(total.Microseconds())/float64(b.N)/1000, "total-ms")
			b.ReportMetric(float64(slowest.Microseconds())/1000, "max-ms")
			b.ReportMetric(float64(moved)/float64(b.N), "moved-rows")
		})
	}
}

// uniqueRow is the first row from i on whose text no other row has, and
// how far below i it is: something to find again once rows move.
func uniqueRow(rows []convo.Line, i int) (string, int) {
	for j := i; j < len(rows); j++ {
		t := rows[j].Text
		if strings.TrimSpace(ansi.Strip(t)) != "" && slices.IndexFunc(rows[j+1:], func(l convo.Line) bool { return l.Text == t }) < 0 && slices.IndexFunc(rows[:j], func(l convo.Line) bool { return l.Text == t }) < 0 {
			return t, j - i
		}
	}
	return "", 0
}
