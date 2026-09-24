package fleet

import (
	"context"
	"sort"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// LoginView is one login with its plan usage.
type LoginView struct {
	claude.Login
	Usage   claude.Usage
	Current bool // the one ~/.claude is signed in as
}

// logins is every saved login with its usage; root is ~/.claude, whose
// saved reading belongs to the login in use.
func (l *Loader) logins(cfg state.Config, root AccountView, now time.Time) []LoginView {
	var out []LoginView
	for _, lg := range cfg.Logins {
		v := LoginView{Login: lg, Current: lg.ID != "" && lg.ID == root.Usage.AccountID}
		f, ok := l.fetched[lg.UsageKey()]
		switch {
		case v.Current && root.Usage.AccountID == lg.ID && (!ok || root.Usage.FetchedAt.After(f.FetchedAt)):
			v.Usage = root.Usage
			v.Usage.Problem = f.Problem
		default:
			v.Usage = f
		}
		v.Usage.Email, v.Usage.Org = lg.Email, lg.Org
		if v.Current {
			v.Usage.Plan, v.Usage.Role, v.Usage.Billing, v.Usage.OrgType, v.Usage.Extra = root.Usage.Plan, root.Usage.Role, root.Usage.Billing, root.Usage.OrgType, root.Usage.Extra
		}
		v.Usage = v.Usage.Since(now)
		out = append(out, v)
	}
	return out
}

// Found is a login signed in somewhere agtop looks, and the name it would
// take: its folder's, for one found in an older ~/.claude-*.
type Found struct {
	Login claude.Login
	Name  string
}

// FindLogins keeps the sign-in ~/.claude holds now in the vault (Claude
// Code replaces it as it refreshes) and reports it, with the login of each
// older folder the vault doesn't have yet. Only ~/.claude's is kept up to
// date: an older folder's sign-in is taken once, and it goes on working
// there for the sessions it holds.
// It fails when ~/.claude's sign-in can't be kept: without a copy agtop
// never switches away from it.
func FindLogins(cfg state.Config) ([]Found, error) {
	v := state.Vault()
	var out []Found
	var failed error
	for i, a := range cfg.AllAccounts() {
		if i == 0 {
			lg, ok, err := v.Keep(a)
			if ok && err == nil {
				out = append(out, Found{Login: lg, Name: a.Name})
			}
			failed = err
			continue
		}
		lg, cred, ok := claude.Signed(a)
		if !ok {
			continue
		}
		if _, err := v.Get(lg.ID); err != nil {
			if v.Put(lg.ID, cred) != nil {
				continue
			}
		}
		out = append(out, Found{Login: lg, Name: a.Name})
	}
	return out, failed
}

// RefreshLogin is a login's plan usage, shared through path like every
// account's. The login in use is asked with ~/.claude's own sign-in, the
// freshest; the others with the one the vault kept.
func RefreshLogin(path string, cfg state.Config, lg claude.Login, offline bool) claude.Usage {
	root := cfg.ActiveAccount()
	return claude.RefreshUsageFor(path, lg.UsageKey(), offline, func(ctx context.Context) (claude.Usage, error) {
		if claude.SignedInAs(root) == lg.ID {
			return claude.FetchUsage(ctx, root)
		}
		cred, err := state.Vault().Get(lg.ID)
		if err != nil {
			return claude.Usage{}, claude.ErrNotSignedIn
		}
		u, err := claude.FetchUsageWith(ctx, cred)
		u.AccountID = lg.ID
		return u, err
	})
}

// NextLogin is the login to switch to when the one in use is nearly out
// of its 5-hour or weekly usage, or stopped says a session already hit a
// limit on it: whichever other login has the most room left, as long as it
// isn't nearly out itself.
func NextLogin(logins []LoginView, stopped bool) (LoginView, bool) {
	var cur *LoginView
	var others []LoginView
	for i := range logins {
		switch u := logins[i].Usage; {
		case logins[i].Current:
			cur = &logins[i]
		case time.Since(u.FetchedAt) < 3*claude.UsageEvery && (u.FiveHour.Present || u.SevenDay.Present):
			// Only a login with a recent reading: one agtop can't read
			// (signed out, expired) would look empty.
			others = append(others, logins[i])
		}
	}
	if cur == nil || len(others) == 0 {
		return LoginView{}, false
	}
	fresh := time.Since(cur.Usage.FetchedAt) < 3*claude.UsageEvery
	if !stopped && !(fresh && cur.Usage.Used() >= state.SwitchAt) {
		return LoginView{}, false
	}
	sort.SliceStable(others, func(i, j int) bool { return others[i].Usage.Used() < others[j].Usage.Used() })
	best := others[0]
	if best.Usage.Used() >= state.SwitchAt || best.Usage.Used() >= cur.Usage.Used() && !stopped {
		return LoginView{}, false
	}
	return best, true
}
