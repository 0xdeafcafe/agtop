package host

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// UseAgent sets cfg to run kind, in the agent's first config folder, and
// reports an agent rush doesn't know, can't run or can't find installed.
// An account already set is kept.
func (cfg *Config) UseAgent(kind string) error {
	a, ok := agent.Get(agent.Kind(kind))
	if !ok {
		var kinds []string
		for _, a := range agent.All() {
			kinds = append(kinds, string(a.Kind()))
		}
		return fmt.Errorf("rush doesn't know the agent %q: it knows %s", kind, strings.Join(kinds, ", "))
	}
	if _, ok := a.(agent.Driver); !ok {
		return fmt.Errorf("rush can't run %s sessions", a.Name())
	}
	if cfg.Kind != "" && cfg.Kind != kind {
		return fmt.Errorf("session %s is a %s session, not %s", cfg.ID, cfg.Kind, kind)
	}
	if !agent.Installed(a.Kind()) {
		agent.Recheck() // it may have been installed since the last look
		if !agent.Installed(a.Kind()) {
			return fmt.Errorf("%s isn't installed: rush can't find its program", a.Name())
		}
	}
	if !agent.Runs(a.Kind()) {
		agent.Recheck()
		if !agent.Runs(a.Kind()) {
			return fmt.Errorf("%s can't run sessions here yet: %s", a.Name(), agent.Hint(a.Kind()))
		}
	}
	cfg.Kind = kind
	if cfg.Account.Dir == "" {
		ps := agent.ProfilesOf(a)
		if len(ps) == 0 {
			return fmt.Errorf("%s isn't installed: rush can't find its program", a.Name())
		}
		cfg.Account = ps[0]
	}
	return nil
}

// Installed are the agents rush can run here: their program is on the
// machine.
func Installed() []agent.Adapter {
	var out []agent.Adapter
	for _, a := range agent.InstalledAll() {
		if agent.CurrentKind(a.Kind()) != a.Kind() {
			continue
		}
		if _, ok := a.(agent.Driver); ok && agent.Runs(a.Kind()) && len(agent.ProfilesOf(a)) > 0 {
			out = append(out, a)
		}
	}
	return out
}

// QuotasPath is where every rush and session keeps other agents' plan
// limits as last read.
func QuotasPath() string { return filepath.Join(state.Dir(), "quotas.json") }

// QuotaKey is where the limits of the account a profile is signed in to
// are kept.
func QuotaKey(p agent.Profile) string { return string(p.Kind) + ":" + p.Dir }

// SignInIfOut signs profile p of agent kind in as the first account rush
// keeps for it, when it's signed in as no one: a run started from a shell
// then works as the app's own do. It says why not when it can't, and
// reads and writes the disk.
func SignInIfOut(kind string, p agent.Profile) error {
	a, ok := agent.Get(agent.Kind(kind))
	if !ok || kind == state.LoginsKind {
		return nil
	}
	acc, ok := a.(agent.Accounts)
	if !ok {
		return nil
	}
	if cur, err := acc.Current(p); err == nil && cur.ID != "" {
		return nil
	}
	kept := state.Load().Config.SignInsOf(kind)
	if len(kept) == 0 {
		return nil // it may sign in where rush can't see (a keyring)
	}
	var last error
	for _, s := range kept {
		if last = acc.Switch(p, agent.Account{Kind: agent.Kind(kind), ID: s.ID, Name: s.Name, Email: s.Email, Plan: s.Plan}); last == nil {
			return nil
		}
	}
	return fmt.Errorf("%s isn't signed in, and rush couldn't sign it in (%v): in rush, Settings, %s, press l to sign in again", a.Name(), last, a.Name())
}
