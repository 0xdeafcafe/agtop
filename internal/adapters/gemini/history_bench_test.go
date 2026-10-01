package gemini

import (
	"fmt"
	"github.com/0xdeafcafe/rush/internal/agent"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func BenchmarkHistoryLarge(b *testing.B) {
	path := filepath.Join(b.TempDir(), "session.jsonl")
	f, _ := os.Create(path)
	fmt.Fprintln(f, `{"sessionId":"bench"}`)
	for i := 0; i < 4096; i++ {
		fmt.Fprintf(f, "{\"id\":\"u%d\",\"type\":\"user\",\"content\":\"request\"}\n{\"id\":\"a%d\",\"type\":\"gemini\",\"content\":%q}\n", i, i, strings.Repeat("response ", 1024))
	}
	f.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := (Adapter{}).History(agent.Session{Transcript: path}, time.Time{}); err != nil {
			b.Fatal(err)
		}
	}
}
