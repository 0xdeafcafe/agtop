package host

import (
	"context"
	"strings"
	"time"

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
		k, base, prov := a.Kind(), a, agent.ProviderOf(a.Kind())
		if agent.KeyOnly(k) && !cfg.HasAPIKey(prov) {
			continue // no key to pay with
		}
		if want, _ := agent.KindFor(prov, agent.Kind(cfg.RunsIn[prov])); want != k {
			continue // each provider in the harness it's set to run in
		}
		if r, ok := a.(agent.Rider); ok {
			base, _ = agent.Get(r.Rides())
		}
		sp, ok := base.(agent.Spawnable)
		if !ok {
			continue // no stand-in hosts it
		}
		if !signedIn(base) {
			continue // told of only once it can run
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
		if len(ids) == 0 {
			ids = localModels(prov)
		}
		models := ", " + model + " <model>"
		switch {
		case len(ids) > 0:
			models = ", " + model + " one of " + strings.Join(ids, ", ")
		case prov == "ollama":
			models = ", " + model + " <a model from `ollama list`>"
		}
		lines = append(lines, "- "+prov+": "+agent.ProviderLabel(prov)+"'s models in "+agent.HarnessLabel(k)+", `"+cmd+"`"+models)
	}
	if len(lines) == 0 {
		return ""
	}
	return "You can hand work to other agents by running them from your shell; rush hosts each one, signed in, and shows it to the user as your subagent, and its output comes back as the program's own:\n" +
		strings.Join(lines, "\n") +
		"\nWhen the user asks for one by name (\"an ollama lane\"), run that line as given. Pick one by what the work needs: a cheaper or local model for routine work, another provider for a second opinion. Treat them as you treat your own subagent types: if one fails to start, use another and tell the user; never debug its sign-in or setup."
}

// signedIn is whether a's first profile can run now, rush signing it in
// with the sign-in it keeps when it's out.
func signedIn(a agent.Adapter) bool {
	ps := agent.ProfilesOf(a)
	return len(ps) == 0 || SignInIfOut(string(a.Kind()), ps[0]) == nil
}

// localModels are the models provider prov has on this machine, as its
// own agent's Summarizer lists them (Ollama's), else none.
func localModels(prov string) []string {
	a, _ := agent.Get(agent.Kind(prov))
	sm, ok := a.(agent.Summarizer)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ms, _ := sm.SummaryModels(ctx)
	return ms
}
