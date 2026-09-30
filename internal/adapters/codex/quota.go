package codex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
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
	Credits         *struct {
		HasCredits bool `json:"hasCredits"`
		Unlimited  bool `json:"unlimited"`
	} `json:"credits"`
	ReachedType string `json:"rateLimitReachedType"`
}

// rateLimitsResponse is account/rateLimits/read's answer.
type rateLimitsResponse struct {
	RateLimits          rateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]rateLimitSnapshot `json:"rateLimitsByLimitId"`
	AccountID           string                       `json:"accountId"`
	ResetCredits        *struct {
		AvailableCount int `json:"availableCount"`
		Credits        []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"`
			GrantedAt   int64  `json:"grantedAt"`
			ExpiresAt   *int64 `json:"expiresAt"`
		} `json:"credits"`
	} `json:"rateLimitResetCredits"`
}

// accountResponse is account/read's answer.
type accountResponse struct {
	Account *struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
}

// billing is how a snapshot says the account pays: past a limit with
// credits on hand, they pay; otherwise the plan does. ok is false when the
// snapshot doesn't say, as a partial update may not.
func (s rateLimitSnapshot) billing() (b usage.Billing, ok bool) {
	if s.Primary == nil && s.Secondary == nil && s.Credits == nil {
		return "", false
	}
	if strings.HasSuffix(s.PlanType, "_usage_based") {
		return usage.Metered, true
	}
	spent := s.ReachedType != ""
	for _, w := range []*rateLimitWindow{s.Primary, s.Secondary} {
		spent = spent || w != nil && w.UsedPercent >= 100
	}
	if spent && s.Credits != nil && (s.Credits.HasCredits || s.Credits.Unlimited) {
		return usage.Overage, true
	}
	return usage.Plan, true
}

// accountBilling is how an account/read answer pays: an API key by the
// token, a ChatGPT sign-in out of its plan.
func accountBilling(a accountResponse) (usage.Billing, bool) {
	if a.Account == nil {
		return "", false
	}
	switch {
	case a.Account.Type == "apiKey":
		return usage.Metered, true
	case strings.HasSuffix(a.Account.PlanType, "_usage_based"):
		return usage.Metered, true
	case a.Account.Type == "chatgpt":
		return usage.Plan, true
	}
	return "", false
}

// mainLimit is the limit Codex's own snapshot reports on: its windows are
// "primary" and "secondary", and other limits' are prefixed with their id.
const mainLimit = "codex"

// quotaFrom is a snapshot as rush's windows. A window the snapshot
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
	if rc := r.ResetCredits; rc != nil {
		q.Resets = &usage.Resets{Available: rc.AvailableCount}
		for _, c := range rc.Credits {
			if c.Status != "" && c.Status != "available" {
				continue
			}
			one := usage.ResetCredit{ID: c.ID, Title: c.Title, About: c.Description, Granted: time.Unix(c.GrantedAt, 0)}
			if c.ExpiresAt != nil {
				one.Expires = time.Unix(*c.ExpiresAt, 0)
			}
			q.Resets.Credits = append(q.Resets.Credits, one)
		}
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

// resetOutcomes say each answer of account/rateLimitResetCredit/consume.
var resetOutcomes = map[string]string{
	"reset":           "its limits are reset",
	"nothingToReset":  "nothing to reset: none of its limits can be reset now",
	"noCredit":        "it has no resets left",
	"alreadyRedeemed": "that reset was already used",
}

// UseReset spends one of the account's earned resets (id, or the next
// when ""), in its own app-server, and says what came of it. Asked once:
// the key makes a retry of the same attempt safe, and there's none.
func UseReset(ctx context.Context, p agent.Profile, binary, id string) (string, error) {
	c, err := spawn(binary, p.Dir, nil, nil, nil)
	if err != nil {
		return "", err
	}
	defer c.close()
	if _, err := c.initialize(ctx); err != nil {
		return "", err
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	params := map[string]any{"idempotencyKey": hex.EncodeToString(key)}
	if id != "" {
		params["creditId"] = id
	}
	var r struct {
		Outcome string `json:"outcome"`
	}
	if err := c.call(ctx, "account/rateLimitResetCredit/consume", params, &r); err != nil {
		return "", err
	}
	if w, ok := resetOutcomes[r.Outcome]; ok {
		return w, nil
	}
	return r.Outcome, nil
}
