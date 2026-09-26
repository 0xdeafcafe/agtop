package claude

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// keyPrefix marks a Claude login's account key: the rest is where its
// reading is kept in usage.json.
const keyPrefix = "claude:"

// accountOf is a saved login as agtop's own account.
func accountOf(l claude.Login) agent.Account {
	return agent.Account{Kind: Kind, Key: keyPrefix + l.UsageKey(), Name: l.Name, Email: l.Email, Org: l.Org}
}

// Accounts are the logins agtop keeps, which ~/.claude can be signed in as.
func (a Adapter) Accounts() []agent.Account {
	var out []agent.Account
	for _, l := range a.config().Logins {
		out = append(out, accountOf(l))
	}
	return out
}

func (a Adapter) login(acct agent.Account) (claude.Login, bool) {
	for _, l := range a.config().Logins {
		if keyPrefix+l.UsageKey() == acct.Key {
			return l, true
		}
	}
	return claude.Login{}, false
}

// Current is the login p is signed in as.
func (a Adapter) Current(p agent.Profile) (agent.Account, error) {
	id := claude.SignedInAs(Account(p))
	if id == "" {
		return agent.Account{}, claude.ErrNotSignedIn
	}
	for _, l := range a.config().Logins {
		if l.ID == id {
			return accountOf(l), nil
		}
	}
	return agent.Account{Kind: Kind, Key: keyPrefix + claude.Login{ID: id}.UsageKey()}, nil
}

// Switch signs p in as acct, from the sign-in agtop keeps for it.
func (a Adapter) Switch(p agent.Profile, acct agent.Account) error {
	l, ok := a.login(acct)
	if !ok {
		return errors.New("agtop has no sign-in for " + acct.Name)
	}
	return state.Vault().Use(Account(p), l)
}

// SignIn is Claude Code's own sign-in, in p.
func (Adapter) SignIn(p agent.Profile) *exec.Cmd {
	c := exec.Command("claude", "auth", "login")
	c.Env = Account(p).Env()
	return c
}

// Quota asks Anthropic for acct's limits with the sign-in agtop keeps
// for it. It doesn't share or cache the reading: see claude.RefreshUsageFor.
func (a Adapter) Quota(ctx context.Context, acct agent.Account) (usage.Quota, error) {
	id := strings.TrimPrefix(strings.TrimPrefix(acct.Key, keyPrefix), "login:")
	cred, err := state.Vault().Get(id)
	if err != nil {
		return usage.Quota{}, claude.ErrNotSignedIn
	}
	if owner, err := claude.Owner(ctx, cred); err == nil && owner != id {
		return usage.Quota{}, errors.New("agtop's sign-in for it is another account's; sign in again")
	}
	u, err := claude.FetchUsageWith(ctx, cred)
	if err != nil {
		return usage.Quota{}, err
	}
	q := u.Quota(acct.Key)
	q.Source = usage.Fetched
	return q, nil
}

var (
	_ agent.Accounts    = Adapter{}
	_ agent.QuotaSource = Adapter{}
)
