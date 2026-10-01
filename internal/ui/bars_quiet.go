package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/netwatch"
	"github.com/0xdeafcafe/rush/internal/sysinfo"
	"github.com/charmbracelet/x/ansi"
)

// quietPlan keeps the default provider fixed. Detailed meters for all
// accounts, reset times, and switch forecasts remain in the usage segment.
func (m *Model) quietPlan() string {
	q, found := m.startQuota()
	account := ""
	for _, r := range m.accountRows() {
		if string(r.kind) == m.startKind() && r.current && !r.head {
			account = r.name()
			break
		}
	}
	if !found {
		for _, a := range m.snap.Accounts {
			if a.Current {
				q = a.Quota
				account = a.Name
				break
			}
		}
		for _, a := range m.snap.Logins {
			if a.Current {
				q = a.Quota
				account = a.Name
				break
			}
		}
	}
	name := providerName(agent.ProviderOf(agent.Kind(m.startKind())))
	if account != "" {
		name += " · " + ansi.Truncate(account, 12, "…")
	}
	return planSummary(name, q, m.snap.At)
}

// planSummary orders windows by identity/duration, never by their usage,
// so changing percentages do not reorder the header on each update.
func planSummary(name string, q usage.Quota, now time.Time) string {
	if len(q.Windows) == 0 {
		if q.Problem != "" {
			return dim(name+" limits ") + paint(cYellow, "unavailable")
		}
		return ""
	}
	less := func(a, b usage.Window) bool {
		if a.Span != b.Span {
			if a.Span == 0 {
				return false
			}
			if b.Span == 0 {
				return true
			}
			return a.Span < b.Span
		}
		return a.Label < b.Label
	}
	var chosen [2]usage.Window
	n := 0
	expired := false
	for _, win := range q.Windows {
		if !win.ResetsAt.IsZero() && !now.Before(win.ResetsAt) {
			expired = true
		}
		if n == 0 {
			chosen[0] = win
			n = 1
			continue
		}
		if less(win, chosen[0]) {
			chosen[1], chosen[0] = chosen[0], win
			n = 2
			continue
		}
		if n == 1 || less(win, chosen[1]) {
			chosen[1] = win
			n = 2
		}
	}
	var parts []string
	for _, win := range chosen[:n] {
		label := win.Label

		if label == "" {
			label = "plan"
		}
		p := win.Percent
		parts = append(parts, dim(ansi.Truncate(label, 12, "…")+" ")+paint(usageColor(p), fmt.Sprintf("%3.0f%%", p))+
			runsOutFirst(p, win.Rate(now), win.ResetsAt, now))
	}
	out := dim(name+" · ") + strings.Join(parts, dim(" · "))
	if len(q.Windows) > n {
		out += dim(fmt.Sprintf(" +%d limits", len(q.Windows)-n))
	}
	if expired {
		out += paint(cYellow, " refresh needed")
	} else if q.Problem != "" || !q.FetchedAt.IsZero() && now.Sub(q.FetchedAt) > 3*usage.Every {
		out += paint(cYellow, " stale")
	}
	return out
}

// runsOutFirst is runsOut kept quiet: only the warning that the window runs
// out before it resets, so the summary holds its width otherwise.
func runsOutFirst(pct, rate float64, resets, now time.Time) string {
	if s := runsOut(pct, rate, resets, now); !strings.Contains(s, "ok") {
		return strings.ReplaceAll(s, "⌛", "fills in ")
	}
	return ""
}

// systemAlerts shows only conditions that need attention; healthy battery,
// disk and network readings do not compete with the conversation.
func systemAlerts(b sysinfo.Battery, d sysinfo.Disk, known, up, steady bool) string {
	var out []string
	if known {
		if !up {
			out = append(out, paint(cRed, "network offline"))
		} else if !steady {
			out = append(out, paint(cYellow, "network unstable"))
		}
	}
	if d.Total > 0 && d.Free < 20<<30 {
		color := cYellow
		if d.Free < 5<<30 {
			color = cRed
		}
		out = append(out, paint(color, "disk low"))
	}
	if b.Present && !b.Charging && b.Percent < 30 {
		color := cYellow
		if b.Percent < 15 {
			color = cRed
		}
		out = append(out, paint(color, "battery low"))
	}
	return strings.Join(out, dim(" · "))
}

func quietSystem() string {
	b, d := sysinfo.Now()
	n := netwatch.Now()
	return systemAlerts(b, d, n.Known, n.Up, n.Steady)
}
