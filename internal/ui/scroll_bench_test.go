package ui

import (
	"os"
	"time"

	"fmt"
	"github.com/0xdeafcafe/rush/internal/convo"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// BenchmarkScroll is the wheel over the Session: each notch through Update,
// then the frame, as scrolling does.
func BenchmarkScroll(b *testing.B) {
	for _, sz := range [][2]int{{120, 40}, {250, 70}} {
		b.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(b *testing.B) {
			m, _ := benchModel(sz[0], sz[1])
			m.listW = 40
			up := tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: sz[0] - 10, Y: 10}
			down := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: sz[0] - 10, Y: 10}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				ev := up
				if i/50%2 == 1 {
					ev = down
				}
				i++
				m.Update(ev)
				m.View()
			}
		})
	}
}

// BenchmarkScrollReal is the wheel over a real session whose last turn
// runs: $RUSH_BENCH_TRANSCRIPT.
func BenchmarkScrollReal(b *testing.B) {
	p := os.Getenv("RUSH_BENCH_TRANSCRIPT")
	if p == "" {
		b.Skip("no $RUSH_BENCH_TRANSCRIPT")
	}
	t := convo.NewTail(p)
	if _, err := t.Read(); err != nil {
		b.Fatal(err)
	}
	t.Sess.Turns[len(t.Sess.Turns)-1].Live = true
	for _, sz := range [][2]int{{120, 40}, {250, 70}} {
		b.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(b *testing.B) {
			m, _ := benchModel(sz[0], sz[1])
			m.host.sess = t.Sess
			m.listW = 40
			m.View()
			up := tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: sz[0] - 10, Y: 10}
			down := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: sz[0] - 10, Y: 10}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				ev := up
				if i/200%2 == 1 {
					ev = down
				}
				i++
				m.tick++
				m.Update(ev)
				m.View()
			}
		})
	}
}

// BenchmarkDragReal is dragging the list's edge beside a real session:
// $RUSH_BENCH_TRANSCRIPT. Each frame the pane is a column wider or
// narrower.
func BenchmarkDragReal(b *testing.B) {
	p := os.Getenv("RUSH_BENCH_TRANSCRIPT")
	if p == "" {
		b.Skip("no $RUSH_BENCH_TRANSCRIPT")
	}
	t := convo.NewTail(p)
	if _, err := t.Read(); err != nil {
		b.Fatal(err)
	}
	m, _ := benchModel(250, 70)
	m.host.sess = t.Sess
	m.View()
	b.ReportAllocs()
	x := 60
	for b.Loop() {
		m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.listW, Y: 10})
		x++
		if x > 140 {
			x = 60
		}
		m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: 10})
		m.View()
		m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: 10})
	}
}

// TestRelayoutSettles is how a drag's relayout goes: the slowest frame, and
// how many frames until it's all drawn afresh. $RUSH_BENCH_TRANSCRIPT.
func TestRelayoutSettles(t *testing.T) {
	p := os.Getenv("RUSH_BENCH_TRANSCRIPT")
	if p == "" {
		t.Skip("no $RUSH_BENCH_TRANSCRIPT")
	}
	tl := convo.NewTail(p)
	if _, err := tl.Read(); err != nil {
		t.Fatal(err)
	}
	m, _ := benchModel(250, 70)
	m.host.sess = tl.Sess
	m.View()
	var worst time.Duration
	frame := func() {
		at := time.Now()
		m.View()
		worst = max(worst, time.Since(at))
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.listW, Y: 10})
	for x := 60; x < 140; x += 2 {
		m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: 10})
		frame()
	}
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 140, Y: 10})
	t.Logf("dragging: slowest frame %v", worst)
	worst = 0
	n := 0
	for m.host.stale && n < 1000 {
		m.Update(relayoutMsg{})
		frame()
		n++
	}
	t.Logf("after release: %d frames to settle, slowest %v", n, worst)
	if m.host.stale {
		t.Fatal("never settled")
	}
}
