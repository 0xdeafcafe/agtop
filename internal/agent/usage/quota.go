package usage

import (
	"strings"
	"time"
)

// Window is one limit an account's plan puts on it: Claude's five hours
// or week, Codex's primary or secondary, Copilot's month of premium
// requests.
type Window struct {
	ID    string        // the provider's name for it: "five_hour", "seven_day_opus", "primary"
	Label string        // short, for a meter: "5h", "7d", "Opus 7d", "month"
	Name  string        // in words: "5-hour", "weekly", "Opus weekly", "premium requests"
	Span  time.Duration // how long it runs; zero when the provider doesn't say
	// Percent is how much of it is used, 0–100. It is always set, from
	// Used and Limit when the provider counts instead.
	Percent     float64
	Used, Limit float64 // when the provider counts: 212 of 300 requests
	ResetsAt    time.Time
	Scope       Scope
}

// Scope is which models a window limits. The zero Scope limits them all.
type Scope struct {
	// Models are matched as substrings of a model's id, so "opus" covers
	// every Opus.
	Models []string
}

// Covers is whether the window limits model. An empty model is any.
func (s Scope) Covers(model string) bool {
	if len(s.Models) == 0 || model == "" {
		return true
	}
	model = strings.ToLower(model)
	for _, m := range s.Models {
		if strings.Contains(model, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// Source is where a reading came from.
type Source int

const (
	Cached  Source = iota // the agent's own cache of it
	Fetched               // asked of the provider by agtop
	Live                  // reported by a session as it ran
)

// Quota is an account's limits as last read. An account belongs to one
// provider, but any number of agents and sessions can spend it.
type Quota struct {
	Account string // the key it is kept under: "claude:login:<uuid>", "codex:chatgpt:<id>"
	Email   string // who the account is, when the reading says
	Plan    string
	// Balance is what's left on a pay-as-you-go account, in its own
	// currency and words: "¥110.00 left". Such an account has no windows.
	Balance   string
	Windows   []Window
	FetchedAt time.Time
	Source    Source
	Problem   string // why there is no fresh reading: not signed in, expired, rate-limited
}

// Tightest is the fullest window that limits model: the one that stops it
// first. ok is false when no window limits it.
func (q Quota) Tightest(model string) (w Window, ok bool) {
	for _, c := range q.Windows {
		if c.Scope.Covers(model) && (!ok || c.Percent > w.Percent) {
			w, ok = c, true
		}
	}
	return w, ok
}

// Used is how full the tightest window for model is, 0–100.
func (q Quota) Used(model string) float64 {
	w, _ := q.Tightest(model)
	return w.Percent
}

// Window is the window called id, if the reading has it.
func (q Quota) Window(id string) (Window, bool) {
	for _, w := range q.Windows {
		if w.ID == id {
			return w, true
		}
	}
	return Window{}, false
}

// Since empties the windows that have reset since the reading was made:
// nothing has been used of them yet, as far as the reading knows.
func (q Quota) Since(now time.Time) Quota {
	ws := make([]Window, len(q.Windows))
	for i, w := range q.Windows {
		if !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
			w.Percent, w.Used, w.ResetsAt = 0, 0, time.Time{}
		}
		ws[i] = w
	}
	q.Windows = ws
	return q
}
