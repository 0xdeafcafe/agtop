package convo

import (
	"strings"
	"testing"
)

// A paragraph with many inline spans, wrapped: every span's codes used to
// carry onto every row after it.
func BenchmarkWrapSpans(b *testing.B) {
	p := strings.Repeat("the **renderer** calls `wrap` on each _paragraph_ and ", 40)
	s := paint(cSub, inline(p, cSub))
	b.ReportAllocs()
	var n int
	for b.Loop() {
		n = 0
		for _, r := range wrap(s, 100) {
			n += len(r)
		}
	}
	b.ReportMetric(float64(n), "bytes/para")
}
