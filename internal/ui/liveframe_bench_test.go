package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// withSubs gives the open session n finished subagent runs, as a long
// session collects.
func withSubs(m *Model, n int) {
	c := m.host
	c.subTails = map[string]*convo.Tail{}
	for i := range n {
		sa := convo.Subagent{ID: fmt.Sprintf("a%04d", i), Type: "Explore", Description: "look around the renderer", ToolUseID: fmt.Sprintf("tu%d", i)}
		c.subs = append(c.subs, sa)
		c.subTails[sa.ID] = convo.SubagentTail("")
	}
}

// BenchmarkLiveFrame is the whole frame while a turn is live in a long
// session: each a second's tick on (the spinner and the pulse move), or
// nothing new (the fast tick's frame), in each of the pane's views.
func BenchmarkLiveFrame(b *testing.B) {
	agent.NeverWait()
	time.Sleep(200 * time.Millisecond)
	for _, turns := range []int{300, 3000} {
		s := benchConvo(turns)
		for _, view := range []int{0, 1, 2} {
			m, _ := benchModel(250, 70)
			withSubs(m, 200)
			m.host.sess, m.host.bodyBuf, m.host.view = s, nil, view
			m.View()
			name := fmt.Sprintf("%s/%dturns", m.viewName(m.host), turns)
			b.Run(name+"/tick", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					m.tick++
					m.View()
				}
			})
			b.Run(name+"/fast", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					m.View()
				}
			})
		}
	}
}

// BenchmarkTabStat is the stat beside each of the pane's tabs, per frame.
func BenchmarkTabStat(b *testing.B) {
	m, _ := benchModel(250, 70)
	c := m.host
	c.sess.Info.Cwd = b.TempDir()
	for _, v := range []string{"overview", "changes"} {
		b.Run(v, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				tabStat(c, v)
			}
		})
	}
}

// BenchmarkListMany is the list alone with many agents on it.
func BenchmarkListMany(b *testing.B) {
	agent.NeverWait()
	for _, n := range []int{30, 300} {
		m, _ := benchModel(250, 70)
		now := time.Now()
		for i := len(m.snap.Agents); i < n; i++ {
			a := &fleet.Agent{Key: fmt.Sprintf("default/b:%d", i), DisplayName: fmt.Sprintf("agent number %d doing things", i), Acct: m.snap.Agents[0].Acct}
			a.ID, a.Cwd, a.Repo, a.Branch = fmt.Sprintf("%08x", i+1000), "/work/rush", "/work/rush", "main"
			a.UpdatedAt, a.State = now.Add(-time.Duration(i)*time.Minute), "done"
			m.snap.Agents = append(m.snap.Agents, a)
		}
		m.rebuild()
		m.preview, m.host = false, nil
		m.View()
		b.Run(fmt.Sprintf("%dagents", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				m.View()
			}
		})
	}
}

// BenchmarkTranscriptFrame is BenchmarkLiveFrame over a real transcript
// (RUSH_BENCH_TRANSCRIPT) and its subagents, its last turn live. Local only.
func BenchmarkTranscriptFrame(b *testing.B) {
	path := os.Getenv("RUSH_BENCH_TRANSCRIPT")
	if path == "" {
		b.Skip("RUSH_BENCH_TRANSCRIPT names a transcript")
	}
	agent.NeverWait()
	t := convo.NewTail(path)
	if _, err := t.Read(); err != nil {
		b.Fatal(err)
	}
	t.Sess.Turns[len(t.Sess.Turns)-1].Live = true
	subs := convo.ListSubagents(path)
	tails := map[string]*convo.Tail{}
	for _, sa := range subs {
		st := convo.SubagentStats(sa.Path)
		_, _ = st.Read()
		tails[sa.ID] = st
	}
	b.Logf("turns %d, subagents %d", len(t.Sess.Turns), len(subs))
	for _, view := range []int{0, 1, 2, 3} {
		m, _ := benchModel(250, 70)
		c := m.host
		c.sess, c.bodyBuf, c.subs, c.subTails, c.view = t.Sess, nil, subs, tails, view
		m.View()
		b.Run(m.viewName(c)+"/tick", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.tick++
				m.View()
			}
		})
		b.Run(m.viewName(c)+"/fast", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.View()
			}
		})
	}
}

// BenchmarkRebuild is what a fleet reading costs the UI when it lands
// (snapMsg): the list sorted and grouped again, with many agents.
func BenchmarkRebuild(b *testing.B) {
	for _, n := range []int{30, 300} {
		m, _ := benchModel(250, 70)
		now := time.Now()
		for i := len(m.snap.Agents); i < n; i++ {
			a := &fleet.Agent{Key: fmt.Sprintf("default/b:%d", i), DisplayName: fmt.Sprintf("agent number %d doing things", i), Acct: m.snap.Agents[0].Acct}
			a.ID, a.Cwd, a.Repo, a.Branch = fmt.Sprintf("%08x", i+1000), "/work/rush", "/work/rush", "main"
			a.UpdatedAt, a.State = now.Add(-time.Duration(i)*time.Minute), "done"
			m.snap.Agents = append(m.snap.Agents, a)
		}
		b.Run(fmt.Sprintf("%dagents", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.rebuild()
			}
		})
	}
}
