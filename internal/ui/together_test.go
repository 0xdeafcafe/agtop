package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/usage"
)

// Accounts together average each window's use over those with a reading,
// and reset when the first of them does.
func TestTogether(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	q := func(p5, p7 float64, r5 time.Duration) usage.Quota {
		return usage.Quota{FetchedAt: t0, Windows: []usage.Window{
			{Label: "5h", Percent: p5, ResetsAt: t0.Add(r5)},
			{Label: "7d", Percent: p7, ResetsAt: t0.Add(72 * time.Hour)},
		}}
	}
	all, n := together([]acctRow{{q: q(12, 99, 3*time.Hour)}, {q: q(0, 82, 5*time.Hour)}, {q: q(98, 59, time.Hour)}, {}})
	if n != 3 || len(all.Windows) != 2 {
		t.Fatalf("n=%d windows=%v", n, all.Windows)
	}
	if w := all.Windows[0]; w.Percent < 36.6 || w.Percent > 36.7 || !w.ResetsAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("5h: %+v", w)
	}
	if got := resetsText(all, t0); got != "5h resets "+t0.Add(time.Hour).Local().Format("15:04")+", in 1h · 7d resets "+t0.Add(72*time.Hour).Local().Format("Mon 15:04")+", in 3d" {
		t.Errorf("resets: %q", got)
	}
}

// Earned resets are counted across accounts, and each account says what
// its own do and when they run out.
func TestResetCredits(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	a := usage.Quota{Resets: &usage.Resets{Available: 2, Credits: []usage.ResetCredit{
		{Title: "Limit reset", About: "resets your weekly limit", Expires: t0.Add(48 * time.Hour)}}}}
	b := usage.Quota{Resets: &usage.Resets{Available: 1}}
	all, _ := together([]acctRow{{q: a}, {q: b}, {}})
	if all.Resets == nil || all.Resets.Available != 3 {
		t.Fatalf("together: %+v", all.Resets)
	}
	got := resetCredits(a, t0)
	for _, want := range []string{"↺ 2 limit resets earned", "u uses one", "Limit reset: resets your weekly limit", "in 2d"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if resetCredits(usage.Quota{}, t0) != "" || resetsChip(usage.Quota{}) != "" {
		t.Error("no resets, nothing said")
	}
}
