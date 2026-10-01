package codex

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
	home := b.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-09-01-bench.jsonl")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	fmt.Fprintln(f, `{"timestamp":"2026-09-01T00:00:00Z","type":"session_meta","payload":{"id":"bench","cwd":"/tmp"}}`)
	for i := 0; i < 4096; i++ {
		stamp := time.Date(2026, 9, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339)
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":%q}}\n", stamp, fmt.Sprint(i))
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"request\"}]}}\n", stamp)
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}}\n", stamp, strings.Repeat("response ", 1024))
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n", stamp)
	}
	f.Close()
	stat, _ := os.Stat(path)
	b.Logf("synthetic bytes=%d, turns=4096", stat.Size())
	s := agent.Session{Transcript: path}
	b.Run("Whole", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := (Adapter{}).History(s, time.Time{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Tail2MiB", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := (Adapter{}).HistoryTail(s, 2<<20); err != nil {
				b.Fatal(err)
			}
		}
	})
	before := time.Date(2026, 9, 1, 0, 0, 3072, 0, time.UTC)
	b.Run("PrefixBefore", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := (Adapter{}).History(s, before); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("TailBefore2MiB", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := (Adapter{}).HistoryTailBefore(s, 2<<20, before); err != nil {
				b.Fatal(err)
			}
		}
	})
	for i := 0; i < 365; i++ {
		d := time.Date(2025, 1, 1+i, 0, 0, 0, 0, time.UTC)
		os.MkdirAll(filepath.Join(home, "sessions", d.Format("2006"), d.Format("01"), d.Format("02")), 0700)
	}
	b.Run("ResolveByID365Days", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f, err := openRollout(agent.Session{ID: "bench", Profile: agent.Profile{Dir: home}})
			if err != nil {
				b.Fatal(err)
			}
			f.Close()
		}
	})
	b.Run("ResolveKnownPath", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f, err := openRollout(s)
			if err != nil {
				b.Fatal(err)
			}
			f.Close()
		}
	})

}
