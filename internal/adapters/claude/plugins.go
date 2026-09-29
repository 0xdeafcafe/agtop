package claude

import (
	"fmt"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
)

// Plugins are the installed plugins and what the marketplaces offer, from
// `claude plugin list`.
func (Adapter) Plugins(p agent.Profile, dir string) (installed, available []agent.Plugin, err error) {
	return claude.Plugins(claude.AccountOf(p), dir)
}

// Marketplaces are the account's plugin marketplaces.
func (Adapter) Marketplaces(p agent.Profile, dir string) ([]agent.Marketplace, error) {
	return claude.Marketplaces(claude.AccountOf(p), dir)
}

// PluginCost is what `claude plugin details` says a plugin always adds.
func (Adapter) PluginCost(p agent.Profile, dir, id string) (string, error) {
	return claude.PluginCost(claude.AccountOf(p), dir, id)
}

// ChangePlugins makes a change through `claude plugin`, so it's exactly
// what Claude Code's own screen would do.
func (Adapter) ChangePlugins(p agent.Profile, dir string, c agent.PluginChange) error {
	scope := []string{"--scope", firstOf(c.Scope, "user")}
	var args []string
	switch c.Op {
	case agent.PluginEnable:
		args = append([]string{"enable", c.ID}, scope...)
	case agent.PluginDisable:
		args = append([]string{"disable", c.ID}, scope...)
	case agent.PluginUpdate:
		args = append([]string{"update", c.ID}, scope...)
	case agent.PluginRemove:
		args = append([]string{"uninstall", c.ID}, scope...)
	case agent.PluginInstall:
		args = append([]string{"install", c.ID}, scope...)
	case agent.MarketAdd:
		args = []string{"marketplace", "add", c.ID}
	case agent.MarketUpdate:
		args = []string{"marketplace", "update"}
		if c.ID != "" {
			args = append(args, c.ID)
		}
	case agent.MarketRemove:
		args = []string{"marketplace", "remove", c.ID}
	default:
		return fmt.Errorf("no such plugin change: %d", c.Op)
	}
	_, err := claude.PluginCLI(claude.AccountOf(p), dir, args...)
	return err
}

var _ agent.Plugger = Adapter{}
