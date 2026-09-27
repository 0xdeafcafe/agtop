package host

import (
	"fmt"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/claude"
)

// UseAgent sets cfg to run kind, in the agent's first config folder, and
// reports an agent agtop doesn't know, can't run or can't find installed.
// Claude Code, the default, needs nothing.
func (cfg *Config) UseAgent(kind string) error {
	if kind == "" || kind == "claude" {
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
	cfg.Kind = kind
	if cfg.Account.ConfigDir == "" {
		ps := a.Profiles()
		if len(ps) == 0 {
			return fmt.Errorf("%s isn't installed: agtop can't find its program", a.Name())
		}
		cfg.Account = claude.Account{Name: ps[0].Name, ConfigDir: ps[0].Dir}
	}
	return nil
}

// Installed are the agents agtop can run here: their program is on the
// machine.
func Installed() []agent.Adapter {
	var out []agent.Adapter
	for _, a := range agent.All() {
		if _, ok := a.(agent.Driver); ok && len(a.Profiles()) > 0 {
			out = append(out, a)
		}
	}
	return out
}
