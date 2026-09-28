package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/advisor"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

func TestAdvisorOptIn(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no claude: nothing may run by accident
	m, _ := benchModel(150, 40)
	m.onboard = true
	if !strings.Contains(ansi.Strip(m.render()), "Let the advisor find savings") {
		t.Fatal("Getting started doesn't offer the advisor")
	}
	if m.advTick() != nil {
		t.Fatal("the advisor ran without being turned on")
	}
	m.command(nil, "#advisor on")
	if !m.store.Config.Advisor || !strings.Contains(strings.Join(m.store.Config.Onboarding.Steps, " "), "advisor") {
		t.Fatal("#advisor on doesn't turn it on and tick the step")
	}
	m.onAdvRan(advRanMsg{})
	m.command(nil, "#advisor off")
	if m.store.Config.Advisor {
		t.Fatal("#advisor off leaves it on")
	}
}

func TestAdvisorFindings(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.store.Config.Advisor = true
	m.eff.base = []efficiency.Finding{{Title: "Shell output is 40%"}}
	at := time.Now()
	m.eff.adv.running = true
	rec := &advisor.Record{}
	fresh := rec.Merge(advisor.Result{At: at, Spent: 0.3, Findings: []advisor.Finding{
		{ID: "a", Title: "Guess", Status: advisor.Candidate, At: at},
		{ID: "b", Title: "Checked", Detail: "why", Weekly: 4, Status: advisor.Confirmed, Note: "seen", At: at},
	}})
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	m.onAdvRan(advRanMsg{rec: rec, fresh: fresh})
	if m.eff.adv.running || !strings.Contains(m.status, "Checked") {
		t.Fatalf("a confirmed finding should be said: %q", m.status)
	}
	f := m.eff.findings
	if len(f) != 3 || f[0].Advice != "b" || f[1].Advice != "" || f[2].Advice != "a" {
		t.Fatalf("want confirmed, then agtop's own, then guesses: %+v", f)
	}
	m.setEffPage(effFindings)
	m.eff.view = &efficiency.View{}
	body := ansi.Strip(strings.Join(m.effFindingsBody(120), "\n"))
	for _, want := range []string{"Advisor", "it has cost $0.300", "Checked by Opus: seen", "$4.00 a week", "Haiku's guess, not checked yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("findings lack %q:\n%s", want, body)
		}
	}
	// A pass landing while one is chosen keeps that one chosen.
	m.eff.finding = 1 // agtop's own
	m.advRefresh()
	if m.eff.findings[m.eff.finding].Advice != "" {
		t.Fatal("the choice moved when the findings were put in again")
	}
	m.eff.finding = 0
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if len(m.eff.findings) != 2 || m.eff.findings[0].Advice == "b" {
		t.Fatal("x doesn't put the advisor's finding away")
	}
	if !slices.Contains(advisor.Load().Dismissed, "b") {
		t.Fatal("putting it away isn't kept")
	}
}

func TestAdvisorOffline(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, _ := benchModel(150, 40)
	m.store.Config.Advisor, m.offline = true, true
	m.eff.adv.checked = time.Now().Add(-time.Hour)
	if m.advTick() != nil {
		t.Fatal("the advisor ran offline (--soak)")
	}
	if cmd, why := m.advRun(true); cmd != nil || why == "" {
		t.Fatal("#advisor now ran offline")
	}
}
