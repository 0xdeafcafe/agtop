package codex

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// rateLimitWindow is one of a snapshot's windows.
type rateLimitWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

// rateLimitSnapshot is Codex's RateLimitSnapshot: one metered limit with
// up to two windows.
type rateLimitSnapshot struct {
	LimitID         string           `json:"limitId"`
	LimitName       string           `json:"limitName"`
	NormalModelSlug string           `json:"normalModelSlug"`
	Primary         *rateLimitWindow `json:"primary"`
	Secondary       *rateLimitWindow `json:"secondary"`
	PlanType        string           `json:"planType"`
}

// rateLimitsResponse is account/rateLimits/read's answer.
type rateLimitsResponse struct {
	RateLimits          rateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]rateLimitSnapshot `json:"rateLimitsByLimitId"`
	AccountID           string                       `json:"accountId"`
}

// accountResponse is account/read's answer.
type accountResponse struct {
	Account *struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
}

// mainLimit is the limit Codex's own snapshot reports on: its windows are
// "primary" and "secondary", and other limits' are prefixed with their id.
const mainLimit = "codex"

// quotaFrom is a snapshot as agtop's windows. A window the snapshot
// doesn't have is left out rather than read as empty.
func quotaFrom(s rateLimitSnapshot) usage.Quota {
	q := usage.Quota{Plan: s.PlanType, FetchedAt: time.Now(), Source: usage.Live}
	q.Windows = windowsOf(s)
	return q
}

func windowsOf(s rateLimitSnapshot) []usage.Window {
	prefix, scope := "", usage.Scope{}
	if s.LimitID != "" && s.LimitID != mainLimit {
		prefix = s.LimitID + ":"
		if s.NormalModelSlug != "" {
			scope.Models = []string{s.NormalModelSlug}
		}
	}
	var out []usage.Window
	for _, p := range []struct {
		id string
		w  *rateLimitWindow
	}{{"primary", s.Primary}, {"secondary", s.Secondary}} {
		if p.w == nil {
			continue
		}
		w := usage.Window{ID: prefix + p.id, Percent: p.w.UsedPercent, Scope: scope}
		if p.w.WindowDurationMins != nil {
			w.Span = time.Duration(*p.w.WindowDurationMins) * time.Minute
		}
		w.Label, w.Name = spanWords(w.Span)
		if name := s.LimitName; prefix != "" && name != "" {
			w.Label, w.Name = name+" "+w.Label, name+" "+w.Name
		}
		if p.w.ResetsAt != nil {
			w.ResetsAt = time.Unix(*p.w.ResetsAt, 0)
		}
		out = append(out, w)
	}
	return out
}

// spanWords names a window by how long it runs: "5h" and "5-hour", "7d"
// and "weekly".
func spanWords(d time.Duration) (label, name string) {
	switch {
	case d <= 0:
		return "limit", "usage limit"
	case d == 7*24*time.Hour:
		return "7d", "weekly"
	case d == 24*time.Hour:
		return "1d", "daily"
	case d%(24*time.Hour) == 0:
		n := int(d / (24 * time.Hour))
		return fmt.Sprintf("%dd", n), fmt.Sprintf("%d-day", n)
	case d%time.Hour == 0:
		n := int(d / time.Hour)
		return fmt.Sprintf("%dh", n), fmt.Sprintf("%d-hour", n)
	default:
		n := int(d / time.Minute)
		return fmt.Sprintf("%dm", n), fmt.Sprintf("%d-minute", n)
	}
}

// quotaFromResponse is a whole rate-limit reading: the main limit, then
// every other one by id.
func quotaFromResponse(r rateLimitsResponse) usage.Quota {
	main := r.RateLimits
	if m, ok := r.RateLimitsByLimitID[mainLimit]; ok {
		main = m
	}
	if main.LimitID == "" {
		main.LimitID = mainLimit
	}
	q := quotaFrom(main)
	ids := make([]string, 0, len(r.RateLimitsByLimitID))
	for id := range r.RateLimitsByLimitID {
		if id != mainLimit {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := r.RateLimitsByLimitID[id]
		if s.LimitID == "" {
			s.LimitID = id
		}
		q.Windows = append(q.Windows, windowsOf(s)...)
		if q.Plan == "" {
			q.Plan = s.PlanType
		}
	}
	if r.AccountID != "" {
		q.Account = "codex:" + r.AccountID
	}
	q.Source = usage.Fetched
	return q
}

// readQuota asks a connected app-server for the account's limits. Neither
// call spends model quota.
func readQuota(ctx context.Context, c *client) (usage.Quota, error) {
	var rl rateLimitsResponse
	if err := c.call(ctx, "account/rateLimits/read", nil, &rl); err != nil {
		return usage.Quota{Problem: err.Error()}, err
	}
	q := quotaFromResponse(rl)
	var acct accountResponse
	if err := c.call(ctx, "account/read", map[string]any{}, &acct); err == nil && acct.Account != nil {
		if acct.Account.PlanType != "" {
			q.Plan = acct.Account.PlanType
		}
		q.Email = acct.Account.Email
		if q.Account == "" && acct.Account.Email != "" {
			q.Account = "codex:" + acct.Account.Email
		}
	}
	return q, nil
}

// Quota reads the limits of the account profile p is signed in to, by
// starting its own app-server. binary is the codex program; empty is
// "codex" on PATH.
func Quota(ctx context.Context, p agent.Profile, binary string) (usage.Quota, error) {
	c, err := spawn(binary, p.Dir, nil, nil, nil)
	if err != nil {
		return usage.Quota{Problem: err.Error()}, err
	}
	defer c.close()
	if _, err := c.initialize(ctx); err != nil {
		return usage.Quota{Problem: err.Error()}, err
	}
	return readQuota(ctx, c)
}
