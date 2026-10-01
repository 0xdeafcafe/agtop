package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// Same replay and first visible frame, changing only the prehistory reader.
// File generation, fleet construction and provider/network startup are excluded.
func BenchmarkHostedCodexFirstFrame(b *testing.B) {
	path := b.TempDir() + "/rollout.jsonl"
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fmt.Fprintln(f, `{"timestamp":"2026-09-01T00:00:00Z","type":"session_meta","payload":{"id":"bench","cwd":"/tmp"}}`)
	for i := 0; i < 4096; i++ {
		stamp := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":%q}}\n", stamp, fmt.Sprint(i))
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"request %d\"}]}}\n", stamp, i)
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}}\n", stamp, strings.Repeat("response ", 1024))
		fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n", stamp)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	agent.NeverWait()
	for _, bounded := range []bool{false, true} {
		name := "WholePrefix"
		if bounded {
			name = "BoundedTail"
		}
		b.Run(name, func(b *testing.B) {
			m, _ := benchModel(160, 40)
			source := agent.Session{ID: "bench", Transcript: path}
			before := base.Add(3072 * time.Second)
			info := host.Info{Kind: "codex", State: "idle", SessionID: "bench", Cwd: "/tmp", StartedAt: before}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				pre := &hostPre{at: -1, info: &info, other: func() *convo.Session { return agentHistory("codex", source, before) }}
				if bounded {
					pre.other = func() *convo.Session { return agentHistoryTailBefore("codex", source, before) }
				}
				lines := make(chan []byte, 3)
				lines <- []byte(`{"agtop_sent":true,"message":{"content":"latest visible request","role":"user"},"type":"user"}`)
				lines <- []byte(`{"info":{"state":"idle","kind":"codex"},"type":"agtop_info"}`)
				c := &hostConn{key: m.sel, kind: "codex", pre: pre, client: &host.Client{Lines: lines}, sess: pre.base(), open: map[string]bool{}}
				m.host = c
				cmd := m.onPre(c.readPre(m.warmOpts())().(preMsg))
				result := cmd().(replayMsg)
				// Leave older-history hydration out: this benchmark ends at the first frame.
				c.sess, c.ready = result.sess, result.whole
				m.View()
				if !c.ready || c.sess.Turns[len(c.sess.Turns)-1].Prompt != "latest visible request" {
					b.Fatal("first frame not at latest turn")
				}
			}
		})
	}
}
