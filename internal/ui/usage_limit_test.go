package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/charmbracelet/x/ansi"
)

func TestLimitCardShowsUsageAgeAndCountdown(t *testing.T) {
	m, c := draftModel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	c.sess.Info.Limit = &host.Limit{Ask: true, Window: "five_hour", ResetsAt: now.Add(65 * time.Minute)}
	c.limitUsage.reading = usage.Quota{FetchedAt: now.Add(-2 * time.Minute), Windows: []usage.Window{{ID: "five_hour", Name: "5-hour", Percent: 100, Used: 300, Limit: 300, ResetsAt: now.Add(65 * time.Minute)}, {ID: "seven_day", Name: "Weekly", Percent: 72}}}
	text := ansi.Strip(strings.Join(m.limitUsageLines(nil, c, now), "\n"))
	for _, want := range []string{"100% used", "300 / 300", "01:05:00", "Last checked", "2m", "Weekly · 72%"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	c.limitUsage.apply(usage.Quota{Problem: "provider unreachable"}, now, false)
	text = ansi.Strip(strings.Join(m.limitUsageLines(nil, c, now), "\n"))
	if !strings.Contains(text, "provider unreachable") || !strings.Contains(text, "100% used") || !c.limitUsage.reading.FetchedAt.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("failure lost successful reading or changed its time: %s", text)
	}
}

func TestLimitCountdownDoesNotInventReset(t *testing.T) {
	now := time.Now()
	if got := limitCountdown(time.Time{}, now); got != "Reset time unknown" {
		t.Fatal(got)
	}
	if got := limitCountdown(now.Add(-time.Minute), now); !strings.Contains(got, "refresh to check") {
		t.Fatal(got)
	}
	if len(limitCountdown(now.Add(time.Hour), now)) != len(limitCountdown(now.Add(time.Hour-time.Second), now)) {
		t.Fatal("countdown width jitters across minute boundary")
	}
}

func TestLimitRefreshIsPassiveAndOffline(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, c := draftModel()
	m.offline = true
	a := m.agentByKey(c.key)
	a.Acct = agent.Profile{Kind: "claude", Dir: t.TempDir()}
	c.sess.Info.Limit = &host.Limit{Ask: true, Continue: false, Window: "five_hour"}
	c.cardFocus = true
	c.input = []rune("queued draft")
	cmd, used := m.cardKey(c, "r", false)
	if !used || cmd == nil || !c.limitUsage.refreshing {
		t.Fatal("r should schedule background usage refresh")
	}
	if second, _ := m.cardKey(c, "r", false); second != nil {
		t.Fatal("duplicate refresh was scheduled")
	}
	result, ok := cmd().(sheetMsg)
	if !ok {
		t.Fatal("refresh sent a host/continuation command")
	}
	if follow := result.apply(m); follow != nil {
		t.Fatal("usage refresh invoked continuation or account policy")
	}
	if !c.sess.Info.Limit.Ask || c.sess.Info.Limit.Continue || !c.cardFocus || string(c.input) != "queued draft" {
		t.Fatal("refresh changed continuation choice, focus, or draft")
	}
	if c.limitUsage.refreshing || !strings.Contains(c.limitUsage.problem, "Offline") {
		t.Fatal("offline result not explained")
	}
}

func TestShortLimitCardKeepsRefreshAndChoices(t *testing.T) {
	m, c := draftModel()
	c.inModal = true
	c.sess.Info.Limit = &host.Limit{Ask: true}
	rows := m.limitCardRows(nil, c, 60, 4)
	if len(rows) > 4 {
		t.Fatalf("card exceeds height: %d", len(rows))
	}
	text := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"y continue", "n wait for me", "r refresh usage"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
