package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRolloutCutoffSeeksExactBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	var content strings.Builder
	var offsets []int64
	for i := 0; i < 100; i++ {
		offsets = append(offsets, int64(content.Len()))
		fmt.Fprintf(&content, "{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"padding\":%q}}\n", t0.Add(time.Duration(i)*time.Second).Format(time.RFC3339), strings.Repeat("x", i*111))
	}
	os.WriteFile(path, []byte(content.String()), 0600)
	f, _ := os.Open(path)
	defer f.Close()
	for i := 0; i <= 100; i++ {
		want := int64(content.Len())
		if i < 100 {
			want = offsets[i]
		}
		got, ok := rolloutCutoff(f, int64(content.Len()), t0.Add(time.Duration(i)*time.Second))
		if !ok || got != want {
			t.Fatalf("cut %d got %d,%v want %d", i, got, ok, want)
		}
	}
}
