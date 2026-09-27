package ui

import (
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// realTasks is the agtop session with the most task output files under
// ~/.config/agtop/sessions, its short id and the files; skips without one.
func realTasks(b *testing.B) (string, string, []string) {
	var best, bestID, bestSess string
	n := 0
	// TestMain moved HOME; the sessions are under the real one.
	u, err := user.Current()
	if err != nil {
		b.Skip(err)
	}
	b.Setenv("AGTOP_HOME", filepath.Join(u.HomeDir, ".config", "agtop"))
	dirs, _ := filepath.Glob(filepath.Join(host.Root(), "*", "tmp", "claude-*", "*", "*", "tasks"))
	for _, d := range dirs {
		fs, _ := filepath.Glob(filepath.Join(d, "*.output"))
		if len(fs) > n {
			n, best = len(fs), d
			rel, _ := filepath.Rel(host.Root(), d)
			bestID = strings.Split(rel, string(filepath.Separator))[0]
			bestSess = filepath.Base(filepath.Dir(d))
		}
	}
	if n < 20 {
		b.Skip("no agtop session with many tasks under " + host.Root())
	}
	fs, _ := filepath.Glob(filepath.Join(best, "*.output"))
	sort.Strings(fs)
	return bestID, bestSess, fs
}

// benchJobsModel is benchModel with a real session's tasks: the ones
// Claude Code said finished know their output file, the few still running
// are found under the session's temp folder.
func benchJobsModel(b *testing.B, w, h int) *Model {
	id, sid, files := realTasks(b)
	m, _ := benchModel(w, h)
	c := m.host
	c.id = id
	s := c.sess
	s.Info.SessionID, s.Info.Proto = sid, 3
	t0 := time.Now().Add(-2 * time.Hour)
	for i, f := range files {
		jid := filepath.Base(f[:len(f)-len(".output")])
		typ := "local_bash"
		if len(jid) > 10 {
			typ = "local_agent"
		}
		at := t0.Add(time.Duration(i) * time.Second)
		s.Apply(headless.TaskStarted{ID: jid, Type: typ, Description: "run the tests " + jid, Backgrounded: true}, at)
		if i < len(files)-4 {
			s.Apply(headless.TaskDone{ID: jid, Status: "completed", OutputFile: f}, at.Add(time.Second))
		}
	}
	for i, v := range m.views(c) {
		if v == "background" {
			c.view = i
		}
	}
	m.View()
	return m
}

func BenchmarkBackgroundView(b *testing.B) {
	m := benchJobsModel(b, 200, 60)
	b.Run("frame", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.tick++
			m.View()
		}
	})
	b.Run("key", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			m.View()
		}
	})
	b.Run("conversation+dock", func(b *testing.B) {
		m.host.view = 0
		b.ReportAllocs()
		for b.Loop() {
			m.tick++
			m.View()
		}
	})
}

// BenchmarkSwitch is the first frame after switching to an agent whose
// long conversation has just been read: nothing of it drawn yet.
func BenchmarkSwitch(b *testing.B) {
	u, err := user.Current()
	if err != nil {
		b.Skip(err)
	}
	paths, _ := filepath.Glob(filepath.Join(u.HomeDir, ".claude", "projects", "*", "*.jsonl"))
	var best string
	var most int64
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Size() > most && st.Size() < 9<<20 {
			best, most = p, st.Size()
		}
	}
	if best == "" {
		b.Skip("no transcript")
	}
	m, _ := benchModel(200, 60)
	key := m.host.key
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warmed"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				t := convo.NewTail(best)
				t.Read()
				open := func() tea.Msg {
					return hostOpenMsg{key: key, c: &hostConn{key: key, tail: t, sess: t.Sess, open: map[string]bool{}, ready: true}}
				}
				if warm {
					_, paneW := m.widths()
					open = warmed(open, convo.Options{Width: paneW - 3, Open: map[string]bool{}, Focused: m.paneFocus, Wide: m.solo != ""})
				}
				m.host = open().(hostOpenMsg).c
				b.StartTimer()
				m.View()
			}
		})
	}
}
