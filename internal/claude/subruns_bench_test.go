package claude

import (
	"os"
	"testing"
	"time"
)

// BenchmarkSubagentRunsStats reads a real session's runs from scratch, as
// the first load after agtop starts does: AGTOP_SUBRUNS names its transcript.
func BenchmarkSubagentRunsStats(b *testing.B) {
	path := os.Getenv("AGTOP_SUBRUNS")
	if path == "" {
		b.Skip("AGTOP_SUBRUNS names a transcript with subagents")
	}
	st, _ := os.Stat(path)
	b.SetBytes(st.Size())
	b.ReportAllocs()
	for b.Loop() {
		var r SubagentRuns
		r.Stats(path, time.Now())
	}
}
