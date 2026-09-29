package host

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// UseAgent sets cfg to run kind, in the agent's first config folder, and
// reports an agent agtop doesn't know, can't run or can't find installed.
// The built-in agent, the default, needs nothing.
func (cfg *Config) UseAgent(kind string) error {
	if agent.IsBuiltin(agent.Kind(kind)) {
		return nil
	}
	a, ok := agent.Get(agent.Kind(kind))
	if !ok {
		var kinds []string
		for _, a := range agent.All() {
			kinds = append(kinds, string(a.Kind()))
		}
		return fmt.Errorf("agtop doesn't know the agent %q: it knows %s", kind, strings.Join(kinds, ", "))
	}
	if _, ok := a.(agent.Driver); !ok {
		return fmt.Errorf("agtop can't run %s sessions", a.Name())
	}
	if cfg.Kind != "" && cfg.Kind != kind {
		return fmt.Errorf("session %s is a %s session, not %s", cfg.ID, cfg.Kind, kind)
	}
	if !agent.Installed(a.Kind()) {
		agent.Recheck() // it may have been installed since the last look
		if !agent.Installed(a.Kind()) {
			return fmt.Errorf("%s isn't installed: agtop can't find its program", a.Name())
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
		ps := a.Profiles()
		if len(ps) == 0 {
			return fmt.Errorf("%s isn't installed: agtop can't find its program", a.Name())
		}
		cfg.Account = ps[0]
	}
	return nil
}

// Installed are the agents agtop can run here: their program is on the
// machine.
func Installed() []agent.Adapter {
	var out []agent.Adapter
	for _, a := range agent.InstalledAll() {
		if _, ok := a.(agent.Driver); ok && agent.Runs(a.Kind()) && len(a.Profiles()) > 0 {
			out = append(out, a)
		}
	}
	return out
}

// QuotasPath is where every agtop and session keeps other agents' plan
// limits as last read.
func QuotasPath() string { return filepath.Join(state.Dir(), "quotas.json") }

// QuotaKey is where the limits of the account a profile is signed in to
// are kept.
func QuotaKey(p agent.Profile) string { return string(p.Kind) + ":" + p.Dir }
