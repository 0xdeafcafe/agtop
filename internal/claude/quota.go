package claude

import (
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// Claude's plan windows, by the names Anthropic gives them.
var windows = []struct {
	id, label, name string
	span            time.Duration
	scope           usage.Scope
}{
	{"five_hour", "5h", "5-hour", 5 * time.Hour, usage.Scope{}},
	{"seven_day", "7d", "weekly", 7 * 24 * time.Hour, usage.Scope{}},
	{"seven_day_opus", "Opus 7d", "Opus weekly", 7 * 24 * time.Hour, usage.Scope{Models: []string{"opus"}}},
}

// Quota is the reading as agtop's own model of a plan's limits, kept
// under key.
func (u Usage) Quota(key string) usage.Quota {
	q := usage.Quota{Account: key, Plan: u.Plan, FetchedAt: u.FetchedAt, Problem: u.Problem}
	if u.Fetched {
		q.Source = usage.Fetched
	}
	for _, d := range windows {
		var w Window
		switch d.id {
		case "five_hour":
			w = u.FiveHour
		case "seven_day":
			w = u.SevenDay
		}
		if !w.Present {
			continue
		}
		q.Windows = append(q.Windows, usage.Window{ID: d.id, Label: d.label, Name: d.name, Span: d.span, Percent: w.Percent, ResetsAt: w.ResetsAt, Scope: d.scope})
	}
	return q
}
