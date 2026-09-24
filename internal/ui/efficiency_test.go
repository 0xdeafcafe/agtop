package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

// effModel is a model in Efficiency with a view read from a made-up
// account: a day of sessions, some using rtk.
func effModel(t *testing.T, w, h int) *Model {
	t.Helper()
	t.Setenv("AGTOP_HOME", t.TempDir())
	t.Setenv("AGTOP_CACHE", t.TempDir())
	acct := claude.Account{Name: "test", ConfigDir: t.TempDir()}
	proj := filepath.Join(acct.ProjectsDir(), "-work-repo")
	_ = os.MkdirAll(proj, 0o700)
	now := time.Now().UTC()
	for s := range 6 {
		var lines []string
		at := now.Add(-time.Duration(s*5+1) * time.Hour)
		for i := range 8 {
			ts := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
			cmd := "git status"
			if s%2 == 0 {
				cmd = "rtk git status"
			}
			lines = append(lines,
				fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s%d","cwd":"/work/repo","message":{"id":"m%d","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t%d","name":"Bash","input":{"command":%q}}],"usage":{"input_tokens":5,"cache_read_input_tokens":%d,"cache_creation_input_tokens":2000,"output_tokens":300}}}`, ts, s, i, i, cmd, 40000+i*10000),
				fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":%q}]}}`, ts, i, strings.Repeat("x", 4000)))
		}
		_ = os.WriteFile(filepath.Join(proj, fmt.Sprintf("s%d.jsonl", s)), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	}
	m, _ := benchModel(w, h)
	m.store.Config.Accounts = nil
	st := efficiency.Open()
	st.Refresh([]claude.Account{acct})
	m.setView(placeEff)
	e := &m.eff
	q := efficiency.NewQuery(efficiency.Ranges[e.rng], time.Now())
	m.onEffLoaded(effLoadedMsg{store: st, view: st.View(q), found: map[string]efficiency.Found{"rtk": {Status: efficiency.Partial, Wants: "its hook isn't in settings.json"}},
		events: []efficiency.Event{{At: now.Add(-12 * time.Hour), Kind: "setting", Saver: "autocompact", Detail: "autoCompactWindow = 400000", Source: "agtop"}}})
	return m
}

// Every page draws at a wide and a narrow width, without a line too long.
func TestEfficiencyPagesDraw(t *testing.T) {
	for _, w := range []int{140, 80} {
		m := effModel(t, w, 50)
		for p := range effPages {
			m.setEffPage(p)
			out := m.render()
			for i, l := range strings.Split(out, "\n") {
				if n := ansiWidth(l); n > w {
					t.Fatalf("%s at %d wide: line %d is %d wide: %q", effPages[p], w, i, n, l)
				}
			}
			if !strings.Contains(out, effPages[p]) {
				t.Fatalf("%s isn't drawn", effPages[p])
			}
		}
	}
}

func ansiWidth(s string) int {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return len([]rune(b.String()))
}

func TestEfficiencyKeys(t *testing.T) {
	m := effModel(t, 140, 50)
	key := func(s string) { m.key(tea.KeyPressMsg{Code: []rune(s)[0], Text: s}) }
	if m.eff.view.Total.Req == 0 || m.eff.view.Uses["rtk"] == nil {
		t.Fatalf("the view should have the sessions and rtk's use: %+v", m.eff.view.Uses)
	}
	if len(m.eff.findings) == 0 || m.eff.findings[0].Fix != "rtk" {
		t.Fatalf("half set up rtk should be the first finding: %+v", m.eff.findings)
	}
	m.setEffPage(effTimeline)
	key("m")
	if m.eff.metric != efficiency.MetricCost {
		t.Fatal("m should go to the next metric")
	}
	key("E")
	if m.eff.cursor < 0 {
		t.Fatal("E should jump back to an event")
	}
	key("b")
	if m.eff.cmp == nil || m.eff.cmp.Saver == nil || m.eff.cmp.With.Sessions+m.eff.cmp.Without.Sessions == 0 {
		t.Fatalf("b on an event compares either side of it: %+v", m.eff.cmp)
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.eff.cmp != nil || m.view != placeEff {
		t.Fatal("esc closes the comparison first")
	}
	key("n")
	if !m.eff.typing() {
		t.Fatal("n writes a note")
	}
	key(".")
	if m.view != placeEff || string(m.eff.note) != "." {
		t.Fatal(". while writing a note is typed, not a move")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Findings: enter shows the fix's plan, which waits on y.
	m.setEffPage(effFindings)
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.eff.plan == nil || m.eff.plan.Saver.ID != "rtk" {
		t.Fatal("enter on a finding shows what fixing it would do")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.eff.page != effFindings {
		t.Fatal("tab doesn't leave a plan waiting on an answer")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.eff.plan != nil {
		t.Fatal("esc cancels the plan")
	}
}
