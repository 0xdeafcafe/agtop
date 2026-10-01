package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
)

func usedRow(k agent.Kind, name string, p float64, at time.Time) acctRow {
	r := acctRow{kind: k, head: name == "", acct: agent.Account{Name: name}}
	r.q = usage.Quota{FetchedAt: at, Windows: []usage.Window{{Label: "5h", Percent: p}}}
	return r
}

// The other providers' meters go the least room first, the free tier says
// so, one without limits isn't there, and those that don't fit are left out.
func TestMeterRow(t *testing.T) {
	now := time.Now()
	gem := usedRow("gemini", "", 30, now)
	gem.q.Plan = "free"
	rows := []acctRow{usedRow("codex", "alex", 20, now), gem, usedRow("copilot", "gh", 90, now), {kind: "vibe", head: true}}
	got := ansi.Strip(meterRow(rows, 200))
	if want := "  ◈ ━━━━━ 90%  ✦ free ━━─── 30%  ◎ ━──── 20%"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := ansi.Strip(meterRow(rows, 30)); got != "  ◈ ━━━━━ 90%" {
		t.Fatalf("narrow: %q", got)
	}
}

// A session's account says it's low from 80%, and where there's most
// room, of any provider; nothing while it has room or its reading is old.
func TestSwitchNote(t *testing.T) {
	now := time.Now()
	cur := usedRow("claude", "home", 92, now).q
	others := []acctRow{usedRow("claude", "work", 85, now), usedRow("codex", "alex", 30, now), usedRow("copilot", "gh", 10, now.Add(-2*time.Hour))}
	if got := switchNote(cur, others, now); got != "low: 92% of 5h used · OpenAI (Codex) · alex has 70% left" {
		t.Fatalf("got %q", got)
	}
	if got := switchNote(cur, others[:1], now); got != "low: 92% of 5h used" {
		t.Fatalf("nowhere with room: %q", got)
	}
	if got := switchNote(usedRow("claude", "home", 60, now).q, others, now); got != "" {
		t.Fatalf("room left: %q", got)
	}
	if got := switchNote(usedRow("claude", "home", 92, now.Add(-time.Hour)).q, others, now); strings.Contains(got, "low") {
		t.Fatalf("old reading: %q", got)
	}
}

func TestSessionLabelUsesHostIdentityOverStaleFleetRow(t *testing.T) {
	m, a, c := barAgentFixture(t)
	a.Kind = "claude"
	c.kind = "claude"
	c.sess.Info.Kind = "codex"
	got := ansi.Strip(m.agentLabel(a, c, "gpt-6-sol"))
	if !strings.HasPrefix(got, lookOf("codex").glyph+" ") || !strings.Contains(got, "Codex") {
		t.Fatalf("stale session identity: %q", got)
	}
}
