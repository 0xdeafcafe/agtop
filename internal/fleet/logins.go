package fleet

import (
	"sort"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/state"
)

// LoginView is one login with its plan usage.
type LoginView struct {
	state.Login
	Usage usage.Reading // who it is, and its plan
	// Quota is the plan's limits, as Usage read them.
	Quota   usage.Quota
	Current bool // the one new sessions run as
}

// logins is every saved login with its usage; root is ~/.claude, whose
// saved reading belongs to the login it's signed in as. The one in use is
// the one rush runs sessions as, in its home, or else ~/.claude's.
func (l *Loader) logins(cfg state.Config, root AccountView, now time.Time) []LoginView {
	var out []LoginView
	var using string
	if l.burns == nil {
		l.burns = map[string]usage.Quota{}
	}
	if k, ok := state.Logins(); ok {
		using = k.UsingLogin(cfg)
	}
	for _, lg := range cfg.Logins {
		v := LoginView{Login: lg, Current: lg.ID != "" && lg.ID == root.Usage.AccountID}
		if using != "" {
			v.Current = using == lg.ID
		}
		f, ok := l.fetched[lg.UsageKey()]
		switch {
		case root.Usage.AccountID == lg.ID && (!ok || root.Usage.FetchedAt.After(f.FetchedAt)):
			v.Usage = root.Usage
			v.Usage.Problem = f.Problem
		default:
			v.Usage = f
		}
		v.Usage.Email, v.Usage.Org = lg.Email, lg.Org
		if root.Usage.AccountID == lg.ID {
			v.Usage.Plan, v.Usage.Role, v.Usage.Billing, v.Usage.OrgType, v.Usage.Extra = root.Usage.Plan, root.Usage.Role, root.Usage.Billing, root.Usage.OrgType, root.Usage.Extra
		}
		v.Usage = v.Usage.Since(now)
		v.Quota = usage.Follow(l.burns[lg.ID], v.Usage.Quota(lg.UsageKey()))
		if prev := l.burns[lg.ID]; v.Quota.FetchedAt.Sub(prev.FetchedAt) >= time.Minute {
			l.burns[lg.ID] = v.Quota // the reading its next is measured from
		}
		out = append(out, v)
	}
	return out
}

// NextLogin is the login to switch to when the one in use is nearly out
// of its 5-hour or weekly usage, or stopped says a session already hit a
// limit on it: whichever other login has the most room left, as long as it
// isn't nearly out itself. Once the one in use is out altogether, any
// login with room left will do.
func NextLogin(logins []LoginView, stopped bool) (LoginView, bool) {
	var cur *LoginView
	var others []LoginView
	for i := range logins {
		switch q := logins[i].Quota; {
		case logins[i].Current:
			cur = &logins[i]
		case time.Since(q.FetchedAt) < otherFor && len(q.Windows) > 0:
			// Only a login with a reading: one rush can't read (signed
			// out, expired) would look empty. A login not in use only
			// empties, so an older reading of it still holds.
			others = append(others, logins[i])
		}
	}
	if cur == nil || len(others) == 0 {
		return LoginView{}, false
	}
	used := cur.Quota.Used("")
	fresh := time.Since(cur.Quota.FetchedAt) < 3*usage.Every
	if !stopped && !(fresh && cur.Quota.NearlyOut("", usage.Lead, time.Now())) {
		return LoginView{}, false
	}
	sort.SliceStable(others, func(i, j int) bool { return others[i].Quota.Used("") < others[j].Quota.Used("") })
	best := others[0]
	out := stopped || used >= 100
	switch b := best.Quota.Used(""); {
	case b >= 100, !out && (b >= state.SwitchAt || b >= used):
		return LoginView{}, false
	}
	return best, true
}

// otherFor is how old a reading of a login not in use may be and still be
// switched to.
const otherFor = time.Hour
