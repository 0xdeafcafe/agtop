package copilot

import (
	"context"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// Quota is your premium requests this month: what Copilot's agents, and
// its better models, spend.
func (Adapter) Quota(ctx context.Context, _ agent.Profile, _ agent.Account) (usage.Quota, error) {
	u, err := readUser(ctx)
	if err != nil {
		return usage.Quota{}, err
	}
	return quotaOf(u, time.Now()), nil
}

// quotaOf is your Copilot's limits as agtop's. Chat and completions are
// unlimited on paid plans; premium requests are what run out.
func quotaOf(u user, now time.Time) usage.Quota {
	q := usage.Quota{Plan: u.Plan, FetchedAt: now, Source: usage.Fetched}
	p, ok := u.Quotas["premium_interactions"]
	if !ok || p.Unlimited {
		return q
	}
	w := usage.Window{ID: "premium_interactions", Label: "month", Name: "premium requests", Span: 30 * 24 * time.Hour,
		Percent: 100 - p.Percent, Used: p.Entitlement - p.Remaining, Limit: p.Entitlement}
	if t, err := time.Parse(time.RFC3339, u.ResetsAt); err == nil {
		w.ResetsAt = t
	}
	q.Windows = append(q.Windows, w)
	return q
}
