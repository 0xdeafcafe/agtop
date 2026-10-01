package state

import "github.com/0xdeafcafe/rush/internal/agent"

// Each route (a provider in a harness) has its own defaults: the model,
// effort and mode its new sessions start with. Almost every route is one
// agent kind of its own (Ollama in Pi is ollama-pi), so it keeps them by
// kind, as before routes. Only a split provider's key in its own harness
// shares a kind with the plan (Claude Code on Anthropic's key), so it
// keeps them under the key's id, and starts from the plan's until set.

// StartOn is what a new session on provider id, run by agent k, starts
// with.
func (d Dispatch) StartOn(id string, k agent.Kind) Start {
	if keyInOwn(id, k) {
		if s, ok := d.Starts[id]; ok {
			return s
		}
	}
	return d.StartFor(string(k))
}

// SetStartOn sets what new sessions on provider id, run by agent k,
// start with.
func (d *Dispatch) SetStartOn(id string, k agent.Kind, s Start) {
	if !keyInOwn(id, k) {
		d.SetStartFor(string(k), s)
		return
	}
	if d.Starts == nil {
		d.Starts = map[string]Start{}
	}
	d.Starts[id] = s
}

// keyInOwn is whether id is a split provider's key, run by its own
// program rather than a rider.
func keyInOwn(id string, k agent.Kind) bool {
	p, key := agent.Billed(id)
	return key && k == agent.Kind(p)
}
