package usage

import (
	"testing"
	"time"
)

func TestTightestFollowsTheModel(t *testing.T) {
	q := Quota{Windows: []Window{
		{ID: "five_hour", Percent: 40},
		{ID: "seven_day", Percent: 55},
		{ID: "seven_day_opus", Percent: 90, Scope: Scope{Models: []string{"opus"}}},
	}}
	cases := map[string]string{
		"claude-opus-5-5[1m]": "seven_day_opus",
		"claude-sonnet-5":     "seven_day",
		"":                    "seven_day_opus", // any model: the fullest of all
	}
	for model, want := range cases {
		if w, ok := q.Tightest(model); !ok || w.ID != want {
			t.Errorf("Tightest(%q) = %q, %v; want %q", model, w.ID, ok, want)
		}
	}
	if got := q.Used("claude-sonnet-5"); got != 55 {
		t.Errorf("Used(sonnet) = %v, want 55", got)
	}
}

func TestTightestWithNoWindows(t *testing.T) {
	if _, ok := (Quota{}).Tightest("gpt-5"); ok {
		t.Error("a quota with no windows limits nothing")
	}
}

func TestSinceEmptiesResetWindows(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	q := Quota{Windows: []Window{
		{ID: "five_hour", Percent: 80, ResetsAt: now.Add(-time.Minute)},
		{ID: "seven_day", Percent: 30, ResetsAt: now.Add(time.Hour)},
	}}
	got := q.Since(now)
	if w, _ := got.Window("five_hour"); w.Percent != 0 || !w.ResetsAt.IsZero() {
		t.Errorf("five_hour after its reset = %+v, want empty", w)
	}
	if w, _ := got.Window("seven_day"); w.Percent != 30 {
		t.Errorf("seven_day = %v, want 30", w.Percent)
	}
	if w, _ := q.Window("five_hour"); w.Percent != 80 {
		t.Error("Since changed the reading it was given")
	}
}
