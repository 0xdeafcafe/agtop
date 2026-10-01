package convo

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkLookupLanded is a frame after a commit card, file head or
// thumbnail lands (lookupsGen moves) over a whole real session, every turn
// open: only turns that read a lookup still out should draw again.
func BenchmarkLookupLanded(b *testing.B) {
	s := History(realTranscript(b), time.Time{})
	o := Options{Width: 200, Now: time.Now(), Open: map[string]bool{}, HideActivity: true}
	for range 100 { // the session's own lookups land
		g := lookupsGen.Load()
		s.Render(o)
		time.Sleep(50 * time.Millisecond)
		if lookupsGen.Load() == g {
			break
		}
	}
	b.Run(fmt.Sprintf("%dturns", len(s.Turns)), func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			lookupsGen.Add(1)
			s.Render(o)
		}
	})
}
