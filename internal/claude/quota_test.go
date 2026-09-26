package claude

import (
	"testing"
	"time"
)

func TestUsageAsQuota(t *testing.T) {
	reset := time.Date(2026, 9, 26, 17, 0, 0, 0, time.UTC)
	u := Usage{Plan: "max", FiveHour: Window{Present: true, Percent: 62, ResetsAt: reset}, SevenDay: Window{Present: true, Percent: 20}, Fetched: true}
	q := u.Quota("login:abc")
	if q.Account != "login:abc" || q.Plan != "max" || len(q.Windows) != 2 {
		t.Fatalf("Quota = %+v", q)
	}
	w, ok := q.Window("five_hour")
	if !ok || w.Percent != 62 || !w.ResetsAt.Equal(reset) || w.Span != 5*time.Hour {
		t.Errorf("five_hour = %+v", w)
	}
	if q.Used("claude-sonnet-5") != u.Used() {
		t.Errorf("Used = %v, Usage.Used = %v", q.Used("claude-sonnet-5"), u.Used())
	}
	if n := len((Usage{SevenDay: Window{Present: true}}).Quota("").Windows); n != 1 {
		t.Errorf("a missing window became a window: %d", n)
	}
}
