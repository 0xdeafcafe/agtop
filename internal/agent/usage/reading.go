package usage

import "time"

// Reading is one reading of an account's plan: who the account is, and
// how much of its limits it has used.
type Reading struct {
	AccountID        string // the signed-in account's own id
	Email, Org, Plan string
	// Role, Billing and OrgType are what else the provider says of the
	// account; Extra is whether use past its limits is billed.
	Role, Billing, OrgType string
	Extra                  bool
	Windows                []Window
	FetchedAt              time.Time
	Problem                string // why no fresh reading: not signed in, expired, rate-limited
	Fetched                bool   // asked of the provider by rush, not the agent's own cache
}

// Quota is the reading's limits, kept under key.
func (r Reading) Quota(key string) Quota {
	q := Quota{Account: key, Plan: r.Plan, Windows: r.Windows, FetchedAt: r.FetchedAt, Problem: r.Problem}
	if r.Fetched {
		q.Source = Fetched
	}
	return q
}

// Since empties the windows that have reset since the reading was made.
func (r Reading) Since(now time.Time) Reading {
	if len(r.Windows) > 0 {
		r.Windows = Quota{Windows: r.Windows}.Since(now).Windows
	}
	return r
}
