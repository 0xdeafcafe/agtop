package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Manual refresh is informational. It never answers the limit card, retries a
// turn or invokes the automatic account-switch/handoff handlers.
type limitUsageState struct {
	reading    usage.Quota
	refreshing bool
	attempted  time.Time
	problem    string
}

func (m *Model) limitQuota(a *fleet.Agent, c *hostConn) usage.Quota {
	q := c.limitUsage.reading
	newer := func(next usage.Quota) {
		if q.FetchedAt.IsZero() || next.FetchedAt.After(q.FetchedAt) {
			q = next
		}
	}
	if a != nil {
		newer(m.quotas[a.Acct.Dir])
		if m.snap != nil {
			for _, av := range m.snap.Accounts {
				if av.ConfigDir == a.Acct.Dir {
					newer(av.Quota)
				}
			}
			if sessionAgent(c) == loginsKind {
				for _, lv := range m.snap.Logins {
					if lv.Name == a.Acct.Name {
						newer(lv.Quota)
					}
				}
			}
		}
	}
	return q
}

func (m *Model) refreshLimitUsage(c *hostConn) tea.Cmd {
	if c.limitUsage.refreshing {
		return nil
	}
	a := m.agentByKey(c.key)
	if a == nil {
		c.limitUsage.problem = "The session's account is unavailable"
		c.limitUsage.attempted = time.Now()
		return nil
	}
	p := a.Acct
	p.Kind = sessionAgent(c)
	if p.Dir == "" {
		c.limitUsage.problem = "The session's account location is unknown"
		c.limitUsage.attempted = time.Now()
		return nil
	}
	ad, ok := agent.Get(p.Kind)
	if !ok {
		c.limitUsage.problem = "This harness has no usage reader"
		c.limitUsage.attempted = time.Now()
		return nil
	}
	q := m.limitQuota(a, c)
	// Capture only immutable input for the worker; retain the last successful
	// reading independently of a failed attempt's timestamp.
	c.limitUsage.reading = q
	offline := m.offline
	var read func() usage.Quota
	if plan, ok := ad.(agent.PlanReader); ok {
		path := filepath.Join(state.Dir(), "usage.json")
		read = func() usage.Quota { r := plan.RefreshPlan(path, p, offline); return r.Quota(fleet.ReadingKey(p, r)) }
	} else if source, ok := ad.(agent.QuotaSource); ok {
		account := agent.Account{Kind: p.Kind, Key: q.Account}
		for _, r := range m.accountRows() {
			if r.kind == p.Kind && r.acct.Key != "" && r.acct.Key == q.Account {
				account = r.acct
				break
			}
		}
		path, key := host.QuotasPath(), host.QuotaKey(p)
		if _, any := ad.(agent.AnyAccountQuota); any && account.Key != "" {
			key = account.Key
		}
		read = func() usage.Quota {
			return usage.Refresh(path, key, offline, func(ctx context.Context) (usage.Quota, error) { return source.Quota(ctx, p, account) })
		}
	}
	if read == nil {
		c.limitUsage.problem = "This harness does not provide usage readings"
		c.limitUsage.attempted = time.Now()
		return nil
	}
	c.limitUsage.refreshing = true
	return sheetDo(func() (usage.Quota, error) { return read(), nil }, func(_ *Model, q usage.Quota, _ error) tea.Cmd {
		c.limitUsage.apply(q, time.Now(), offline)
		return nil
	})
}

func (s *limitUsageState) apply(q usage.Quota, now time.Time, offline bool) {
	s.refreshing = false
	s.attempted = now
	s.problem = q.Problem
	if offline {
		s.problem = "Offline · showing the cached reading"
	}
	if q.Problem == "" && !q.FetchedAt.IsZero() && !q.FetchedAt.Before(s.reading.FetchedAt) {
		s.reading = q
	}
	if s.problem == "" && q.FetchedAt.IsZero() {
		s.problem = "No usage reading available; the session remains paused"
	}
}

func quotaWindowName(w usage.Window) string {
	return firstNonEmpty(w.Name, w.Label, w.ID, "Provider quota")
}
func limitCountdown(reset, now time.Time) string {
	if reset.IsZero() {
		return "Reset time unknown"
	}
	if !reset.After(now) {
		return "Reset time passed · refresh to check usage"
	}
	seconds := int64(reset.Sub(now).Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("Reset in %02d:%02d:%02d · %s", seconds/3600, seconds/60%60, seconds%60, reset.Local().Format("Mon 15:04"))
}

// limitUsageLines never infers a fresh reading from a refresh attempt, or that
// quota is available merely because its advertised reset time has passed.
func (m *Model) limitUsageLines(a *fleet.Agent, c *hostConn, now time.Time) []string {
	q := m.limitQuota(a, c)
	l := c.sess.Info.Limit
	if l == nil {
		return nil
	}
	var out []string
	selected, known := q.Window(l.Window)
	if !known && l.Window == "" {
		selected, known = q.Tightest(argRunning(c, "model"))
	}
	reset := l.ResetsAt
	if known && !selected.ResetsAt.IsZero() && (l.Window == "" || selected.ID == l.Window) && (reset.IsZero() || q.FetchedAt.After(c.sess.Info.UpdatedAt)) {
		reset = selected.ResetsAt
	}
	if known {
		number := fmt.Sprintf("%.0f%% used", selected.Percent)
		if selected.Limit > 0 {
			number += fmt.Sprintf(" · %g / %g", selected.Used, selected.Limit)
		}
		out = append(out, paint(cText, quotaWindowName(selected)+" · "+number))
	} else {
		out = append(out, paint(cText, "Provider quota reached · usage numbers unavailable"))
	}
	out = append(out, paint(cYellow, limitCountdown(reset, now)))
	if q.FetchedAt.IsZero() {
		out = append(out, dim("Last checked: no successful usage reading"))
	} else {
		age := now.Sub(q.FetchedAt)
		if age < 0 {
			age = 0
		}
		out = append(out, dim("Last checked "+q.FetchedAt.Local().Format("15:04:05")+" · "+dur(age)+" ago"))
	}
	for _, w := range q.Windows {
		if known && w.ID == selected.ID {
			continue
		}
		out = append(out, dim(fmt.Sprintf("%s · %.0f%% used", quotaWindowName(w), w.Percent)))
	}
	switch {
	case c.limitUsage.refreshing:
		out = append(out, dim("Refreshing usage…"))
	case c.limitUsage.problem != "":
		at := ""
		if !c.limitUsage.attempted.IsZero() {
			at = " at " + c.limitUsage.attempted.Local().Format("15:04:05")
		}
		out = append(out, paint(cYellow, "Refresh"+at+": "+c.limitUsage.problem))
	case !c.limitUsage.attempted.IsZero():
		out = append(out, dim("Refresh checked at "+c.limitUsage.attempted.Local().Format("15:04:05")+" · provider cache limits apply"))
	case q.Problem != "":
		out = append(out, paint(cYellow, "Usage status: "+q.Problem))
	}
	return out
}

func (m *Model) limitCardRows(a *fleet.Agent, c *hostConn, w, maxH int) []string {
	now := time.Now()
	var out []string
	add := func(line string) {
		for _, row := range wrap(line, max(8, w-4)) {
			out = append(out, "  "+row)
		}
	}
	for _, line := range m.limitUsageLines(a, c, now) {
		add(line)
	}
	// Keep both decisions and refresh visible even on a short terminal.
	footer := []string{"  " + keysFit(w-4, "y", "continue", "n", "wait for me"),
		"  " + keysFit(w-4, "r", "refresh usage", "esc", "later")}
	if maxH <= 0 || maxH >= 7 {
		var prompt []string
		for _, row := range wrap(dim("Continue at reset? New messages stay queued."), max(8, w-4)) {
			prompt = append(prompt, "  "+row)
		}
		footer = append(prompt, footer...)
	}
	if maxH > 0 && len(out)+len(footer) > maxH {
		out = out[:max(0, maxH-len(footer))]
	}
	out = append(out, footer...)
	if !c.inModal {
		for i, line := range out {
			out[i] = onBg(qCard, strings.TrimRight(line, " "), w)
		}
	}
	return out
}
