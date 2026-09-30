package state

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
)

// LoginKeeper is the logins agent's side of Logins: it keeps each login's
// sign-in, reads each one's plan, and signs its home in as one. Its
// methods read the disk, and some the network.
type LoginKeeper interface {
	// FindLogins keeps the sign-in the agent's home holds now, and takes
	// in the folders an older rush was given: see FoundLogin and
	// Restored. imported is whether every older folder was taken in whole,
	// and cfg can forget them; failed is why the home's sign-in couldn't
	// be kept.
	FindLogins(cfg Config) (found []FoundLogin, restored *Restored, imported bool, failed error)
	// RefreshLogin is l's plan, shared through path like every account's.
	RefreshLogin(path string, cfg Config, l Login, offline bool) usage.Reading
	// RenewLogin refreshes l's sign-in, unless an agent running on it will.
	RenewLogin(cfg Config, l Login) error
	// UsingLogin is the login new sessions run as, in a home of its own;
	// "" runs them in the agent's home as whoever it's signed in as.
	UsingLogin(cfg Config) string
	// UseLogin makes l the login new sessions run as.
	UseLogin(cfg Config, l Login) error
	// AdoptLogin keeps the sign-in just made in p as its login's.
	AdoptLogin(p agent.Profile) (Login, error)
	// ForgetLogin drops what rush keeps of the login with id.
	ForgetLogin(id string) error
}

// FoundLogin is a login signed in somewhere rush looks, and the name it
// would take: its folder's, for one found in an older folder.
type FoundLogin struct {
	Login Login
	Name  string
}

// Restored is what FindLogins put right: an agent started before a switch
// had put its account's sign-in back in the home.
type Restored struct {
	Was, Now string // the ids of the account it had put back, and the one switched to again
	Err      error  // why it couldn't be switched again, if it couldn't
}

// Logins is the logins agent's LoginKeeper, if it has one.
func Logins() (LoginKeeper, bool) {
	return agent.As[LoginKeeper](agent.Kind(LoginsKind))
}
