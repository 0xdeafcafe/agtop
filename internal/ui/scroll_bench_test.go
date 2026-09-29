package ui

import (
	"os"

	"fmt"
	"github.com/0xdeafcafe/agtop/internal/convo"
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
// runs: $AGTOP_BENCH_TRANSCRIPT.
func BenchmarkScrollReal(b *testing.B) {
	p := os.Getenv("AGTOP_BENCH_TRANSCRIPT")
	if p == "" {
		b.Skip("no $AGTOP_BENCH_TRANSCRIPT")
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
