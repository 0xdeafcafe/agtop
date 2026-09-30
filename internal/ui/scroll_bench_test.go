package ui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// BenchmarkScroll is one wheel tick over the Session pane, then the frame,
// in each of its views, over a long session and a longer one.
func BenchmarkScroll(b *testing.B) {
	agent.NeverWait() // as cmd/rush does: a frame never stats an agent's home
	time.Sleep(200 * time.Millisecond)
	for _, turns := range []int{300, 3000} {
		s := benchConvo(turns)
		for _, view := range []int{0, 1, 2} {
			m, _ := benchModel(250, 70)
			m.host.sess, m.host.bodyBuf, m.host.view = s, nil, view
			m.View()
			b.Run(fmt.Sprintf("%s/%dturns", m.viewName(m.host), turns), func(b *testing.B) {
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					btn := tea.MouseWheelUp
					if i/20%2 == 1 {
						btn = tea.MouseWheelDown
					}
					i++
					m.Update(tea.MouseWheelMsg{X: 245, Y: 35, Button: btn})
					m.View()
				}
			})
		}
	}
}
