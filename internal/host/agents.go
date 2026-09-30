package host

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// agentsPrompt tells a session which agents it can start from its shell,
// as rush's stand-ins host them, so it can hand work to another: each
// provider (whose models) in each harness (the program round them) that's
// installed and a stand-in runs, one riding another's harness picked with
// RUSH_AGENT. "" when there's none to tell of.
func agentsPrompt() string {
	all, cfg := Installed(), state.Load().Config
	lines := make([]string, 0, len(all))
	for _, a := range all {
		k, base := a.Kind(), a
		if agent.KeyOnly(k) && !cfg.HasAPIKey(agent.ProviderOf(k)) {
			continue // no key to pay with
		}
		if r, ok := a.(agent.Rider); ok {
			base, _ = agent.Get(r.Rides())
		}
		sp, ok := base.(agent.Spawnable)
		if !ok {
			continue // no stand-in hosts it
		}
		cmd, model := sp.SpawnCommand()
		if k != base.Kind() {
			cmd = "RUSH_AGENT=" + string(k) + " " + cmd
		}
		var ids []string
		if ch, ok := agent.ChoicesOf(k); ok {
			for _, c := range ch.Models {
				ids = append(ids, c.ID)
			}
		}
		models := ", " + model + " <model>"
		switch {
		case len(ids) > 0:
			models = ", " + model + " one of " + strings.Join(ids, ", ")
		case strings.HasPrefix(string(k), "ollama"):
			models = ", " + model + " <a model from `ollama list`>"
		}
		lines = append(lines, "- "+agent.ProviderLabel(agent.ProviderOf(k))+"'s models in "+agent.HarnessLabel(k)+": `"+cmd+"`"+models)
	}
	if len(lines) == 0 {
		return ""
	}
	return "You can hand work to other agents by running them from your shell; rush hosts each one and shows it to the user as your subagent, and its output comes back as the program's own:\n" +
		strings.Join(lines, "\n") +
		"\nPick one by what the work needs: a cheaper or local model for routine work, another provider for a second opinion."
}
