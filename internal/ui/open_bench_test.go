package ui

import (
	"context"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// TestOpenReal opens real sessions as the pane does and logs what each
// part took: RUSH_OPEN=claude:/path.jsonl,codex:/rollout.jsonl.
func TestOpenReal(t *testing.T) {
	spec := os.Getenv("RUSH_OPEN")
	if spec == "" {
		t.Skip("RUSH_OPEN names the transcripts")
	}
	agent.NeverWait()
	for _, kp := range strings.Split(spec, ",") {
		kind, path, _ := strings.Cut(kp, ":")
		m, _ := benchModel(250, 70)
		m.host = nil
		a := m.focused()
		a.Rush, a.PID, a.State, a.Kind = false, 0, "done", kind
		if kind == "claude" {
			a.TranscriptPath = path
		} else {
			a.History = path
		}
		t0 := time.Now()
		since := func() time.Duration { return time.Since(t0).Round(time.Millisecond) }
		// Each phase labelled, for pprof -tagfocus=phase=first.
		phase := func(name string, f func()) {
			pprof.Do(context.Background(), pprof.Labels("phase", name), func(context.Context) { f() })
		}
		var tail, first time.Duration
		var whole tea.Cmd
		phase("first", func() {
			msg := m.syncHost()()
			tail = since()
			whole = m.onHostOpen(msg.(hostOpenMsg))
			m.View()
			first = since()
		})
		c := m.host
		// The runs listed, then (or for Codex, the whole history) the rest.
		phase("rest", func() {
			m.onPane(m.refreshSubs()().(paneMsg))
			m.View()
		})
		subs := since()
		full := subs
		if whole != nil {
			phase("rest", func() {
				m.onWhole(whole().(wholeMsg))
				m.View()
			})
			full = since()
		}
		phase("rest", func() {
			if sc := m.refreshSubs(); sc != nil {
				m.onPane(sc().(paneMsg))
			}
		})
		runs := since()
		var batch time.Duration
		if c.view = slices.Index(m.views(c), "subagents"); c.view >= 0 && os.Getenv("RUSH_OPEN_STATS") != "" {
			c.subReading = false // onPane's own read, for the running ones, dropped above
			for cmd := m.readUnread(c); cmd != nil; {
				msg := cmd()
				if batch == 0 {
					batch = time.Since(t0)
				}
				cmd = m.onSubStats(msg.(subStatsMsg))
			}
		}
		stats := since()
		var ms runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&ms)
		t.Logf("%s %s: tail %v, first frame %v, runs listed %v, whole %v (partial %v), run states %v, first stats %v, all %d/%d stats %v, heap %dMB",
			kind, path[strings.LastIndex(path, "/")+1:], tail, first, subs, full, c.sess.Partial, runs, batch.Round(time.Millisecond), len(c.subTails), len(c.subs), stats, ms.HeapAlloc>>20)
		runtime.KeepAlive(m)
	}
}
