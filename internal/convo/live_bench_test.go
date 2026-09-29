package convo

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkRenderLive is a real session's frames while its last turn runs:
// the clock ticks each frame, as it does while you scroll.
func BenchmarkRenderLive(b *testing.B) {
	t := NewTail(realTranscript(b))
	if _, err := t.Read(); err != nil {
		b.Fatal(err)
	}
	s := t.Sess
	last := s.Turns[len(s.Turns)-1]
	last.Live = true
	b.Logf("turns %d, last turn items %d", len(s.Turns), len(last.Items))
	for _, w := range []int{120, 250} {
		o := Options{Width: w, Now: time.Now(), Open: map[string]bool{}}
		s.Render(o)
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				i++
				o.Tick = i
				o.Now = o.Now.Add(100 * time.Millisecond)
				s.Render(o)
			}
		})
	}
}
