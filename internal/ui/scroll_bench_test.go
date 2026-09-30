package ui

import (
	"fmt"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// BenchmarkScroll is one wheel tick over the Session pane, then the frame,
// in each of its views.
func BenchmarkScroll(b *testing.B) {
	agent.NeverWait() // as cmd/rush does: the view never stats a home
	time.Sleep(200 * time.Millisecond)
	agent.NeverWait()
	for _, view := range []int{0, 1, 2} {
		for _, sz := range [][2]int{{120, 40}, {250, 70}} {
			m, _ := benchModel(sz[0], sz[1])
			m.host.view = view
			m.View()
			name := m.viewName(m.host)
			b.Run(fmt.Sprintf("%s/%dx%d/wheel", name, sz[0], sz[1]), func(b *testing.B) {
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					btn := tea.MouseWheelUp
					if i/20%2 == 1 {
						btn = tea.MouseWheelDown
					}
					i++
					m.Update(tea.MouseWheelMsg{X: sz[0] - 5, Y: sz[1] / 2, Button: btn})
					m.View()
				}
			})
			b.Run(fmt.Sprintf("%s/%dx%d/rushPane", name, sz[0], sz[1]), func(b *testing.B) {
				b.ReportAllocs()
				_, paneW, _ := m.layout()
				for b.Loop() {
					m.rushPane(paneW, sz[1]-6)
				}
			})
		}
	}
	m, _ := benchModel(250, 70)
	c, s := m.host, m.host.sess
	o := convo.Options{Width: 180, Now: time.Now(), Open: c.open}
	b.Run("body/RenderInto", func(b *testing.B) {
		b.ReportAllocs()
		oo := o
		oo.Budget = relayBudget
		for b.Loop() {
			c.bodyBuf = s.RenderInto(oo, c.bodyBuf)
		}
	})
	b.Run("body/Overview", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.Overview(o)
		}
	})
	b.Run("body/ChangesView", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.ChangesView(o)
		}
	})
	b.Run("viewName", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.viewName(c)
		}
	})
}

// BenchmarkScrollFlush is the renderer's side of a wheel tick: the new
// frame parsed into cells, diffed against the last, written out.
func BenchmarkScrollFlush(b *testing.B) {
	agent.NeverWait()
	for _, view := range []int{0, 1} {
		m, _ := benchModel(250, 70)
		m.host.view = view
		m.View()
		name := m.viewName(m.host)
		var frames []string
		for i := 0; i < 40; i++ {
			btn := tea.MouseWheelUp
			if i >= 20 {
				btn = tea.MouseWheelDown
			}
			m.Update(tea.MouseWheelMsg{X: 245, Y: 35, Button: btn})
			frames = append(frames, m.View().Content)
		}
		buf := uv.NewScreenBuffer(250, 70)
		scr := uv.NewTerminalRenderer(io.Discard, nil)
		scr.SetFullscreen(true)
		scr.SetScrollOptim(true)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				buf.Clear()
				uv.NewStyledString(frames[i%len(frames)]).Draw(buf, buf.Bounds())
				scr.Render(buf.RenderBuffer)
				_ = scr.Flush()
				i++
			}
		})
	}
}
