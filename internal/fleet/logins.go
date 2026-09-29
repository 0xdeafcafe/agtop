package fleet

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// LoginView is one login with its plan usage.
type LoginView struct {
	claude.Login
	Usage claude.Usage // who it is, and its plan
	// Quota is the plan's limits, as Usage read them.
	Quota   usage.Quota
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
		v.Quota = v.Usage.Quota(lg.UsageKey())
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

// Restored is what FindLogins put right: a Claude Code started before a
// switch had put its account's sign-in back in ~/.claude.
type Restored struct {
	Was, Now string // the uuids of the account it had put back, and the one switched to again
	Err      error  // why it couldn't be switched again, if it couldn't
}

// FindLogins keeps the sign-in ~/.claude holds now in the vault (Claude
// Code replaces it as it refreshes) and reports it. It also takes in the
// folders an older agtop was given: each one's login is reported, its
// sign-in put in the vault unless cfg says that was done already, and its
// past sessions copied into ~/.claude, where they're found and resumed
// like any other. imported is whether every older folder was taken in
// whole, and cfg can forget them.
// It fails when ~/.claude's sign-in can't be kept: without a copy agtop
// never switches away from it.
//
// A sign-in in ~/.claude that isn't the account it names was put back by
// a Claude Code started before a switch, as it refreshed its own: the
// switch is made again, and restored says so.
func FindLogins(cfg state.Config) (found []Found, restored *Restored, imported bool, failed error) {
	v := state.Vault()
	root := cfg.ActiveAccount()
	lg, owner, ok, err := v.Keep(root)
	if ok && err == nil {
		found = append(found, Found{Login: lg})
	}
	failed = err
	if ok && err == nil && owner != "" && putBack(owner, lg.ID) {
		restored = &Restored{Was: owner, Now: lg.ID, Err: v.Use(root, lg)}
		if restored.Err != nil {
			// It can't be switched back: it's signed in as owner,
			// so it says so.
			for _, l := range cfg.Logins {
				if l.ID == owner && len(l.Profile) > 0 {
					_ = claude.Name(root, l)
				}
			}
		}
	}
	imported = true
	for _, a := range cfg.OldFolders() {
		if claude.MergeHistory(a, root) != nil {
			imported = false
		}
		if cfg.FoldersImported {
			continue
		}
		lg, cred, ok := claude.Signed(a)
		if !ok {
			continue
		}
		if _, err := v.Get(lg.ID); err != nil {
			if v.Put(lg.ID, cred) != nil {
				imported = false
				continue
			}
		}
		found = append(found, Found{Login: lg, Name: a.Name})
	}
	return found, restored, imported, failed
}

// mismatch is the sign-in found in ~/.claude as another account's than
// it names, and when.
var mismatch struct {
	sync.Mutex
	was, now string
	at       time.Time
}

// putBack is whether ~/.claude signed in as was while naming now is a
// switch put back rather than a sign-in under way (claude /login writes
// the sign-in a moment before the name): it has to be seen twice, at least
// half a minute apart.
func putBack(was, now string) bool {
	mismatch.Lock()
	defer mismatch.Unlock()
	if mismatch.was != was || mismatch.now != now || time.Since(mismatch.at) > 10*time.Minute {
		mismatch.was, mismatch.now, mismatch.at = was, now, time.Now()
		return false
	}
	return time.Since(mismatch.at) >= 30*time.Second
}

// RefreshLogin is a login's plan usage, shared through path like every
// account's. The login in use is asked with ~/.claude's own sign-in, the
// freshest; the others with the one the vault kept.
func RefreshLogin(path string, cfg state.Config, lg claude.Login, offline bool) claude.Usage {
	root := cfg.ActiveAccount()
	if claude.SignedInAs(root) == lg.ID {
		return claude.RefreshUsage(path, root, offline)
	}
	return claude.RefreshUsageFor(path, lg.UsageKey(), offline, func(ctx context.Context) (claude.Usage, error) {
		cred, err := state.Vault().Get(lg.ID)
		if err != nil {
			return claude.Usage{}, claude.ErrNotSignedIn
		}
		return claude.FetchUsageAs(ctx, cred, lg.ID)
	})
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
			// Only a login with a reading: one agtop can't read (signed
			// out, expired) would look empty. A login not in use only
			// empties, so an older reading of it still holds.
			others = append(others, logins[i])
		}
	}
	if cur == nil || len(others) == 0 {
		return LoginView{}, false
	}
	used := cur.Quota.Used("")
	fresh := time.Since(cur.Quota.FetchedAt) < 3*claude.UsageEvery
	if !stopped && !(fresh && used >= state.SwitchAt) {
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
