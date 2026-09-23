package fleet

import (
	"os"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// On this machine's real agents: AGTOP_REAL=1 go test -bench Real ./internal/fleet
func BenchmarkLoadReal(b *testing.B) {
	if os.Getenv("AGTOP_REAL") == "" {
		b.Skip("set AGTOP_REAL=1")
	}
	l := NewLoader(state.Load())
	l.Load(true)
	b.ReportAllocs()
	for b.Loop() {
		l.Load(true)
	}
}

func BenchmarkScanReal(b *testing.B) {
	if os.Getenv("AGTOP_REAL") == "" {
		b.Skip("set AGTOP_REAL=1")
	}
	l := NewLoader(state.Load())
	snap := l.Load(false)
	var targets []Target
	for _, a := range snap.Agents {
		if a.TranscriptPath != "" {
			targets = append(targets, Target{Key: a.Key, Path: a.TranscriptPath})
		}
	}
	sc := NewScanner()
	sc.Run(targets)
	b.ReportAllocs()
	for b.Loop() {
		sc.Run(targets)
	}
}
