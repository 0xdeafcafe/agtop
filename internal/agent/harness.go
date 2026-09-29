package agent

import "slices"

// A provider is where a session's model comes from (Anthropic, OpenAI,
// Ollama on this machine); a harness is the program that runs the session
// around it (Claude Code, Codex, Pi). Most agents are both at once: Codex
// runs OpenAI's models. A Rider is a provider running in another agent's
// harness, and one provider can ride several: Ollama in Claude Code, in Pi
// and in Codex are each an adapter of their own, all of provider "ollama".

// Provided is a Rider that names the provider it runs, when that isn't its
// own kind: Ollama in Pi is kind "ollama-pi", provider "ollama".
type Provided interface {
	Provider() string
}

// Made is an agent that names the company behind the models it runs,
// when that isn't its own name: Claude Code's are Anthropic's.
type Made interface {
	Maker() string
}

// ProviderName is provider p by the company behind its models, else
// empty: the caller names it as the agent.
func ProviderName(p string) string {
	if a, ok := Get(Kind(p)); ok {
		if m, ok := a.(Made); ok {
			return m.Maker()
		}
	}
	return ""
}

// ProviderOf is the provider agent k runs: a Rider's Provider, else k.
func ProviderOf(k Kind) string {
	if a, ok := Get(k); ok {
		if p, ok := a.(Provided); ok {
			return p.Provider()
		}
	}
	return string(k)
}

// HarnessOf is the program agent k runs in: the agent a Rider rides, else
// k itself.
func HarnessOf(k Kind) Kind {
	if a, ok := Get(k); ok {
		if r, ok := a.(Rider); ok {
			return r.Rides()
		}
	}
	return k
}

// Providers are every provider an adapter runs, by name, each once.
func Providers() []string {
	var out []string
	for _, a := range All() {
		if p := ProviderOf(a.Kind()); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// Harnesses are the agents that run provider p, whether installed or not:
// the one of p's own kind first (its default), then the rest by kind.
func Harnesses(p string) []Kind {
	var out []Kind
	for _, a := range All() {
		if ProviderOf(a.Kind()) == p {
			out = append(out, a.Kind())
		}
	}
	slices.SortStableFunc(out, func(a, b Kind) int {
		switch {
		case string(a) == p:
			return -1
		case string(b) == p:
			return 1
		}
		return 0
	})
	return out
}

// KindFor is the agent that runs provider p in harness h: p's own kind
// when h is empty or p's own harness. When that one isn't installed, the
// first of p's that is. ok is false when p has no adapter at all.
func KindFor(p string, h Kind) (Kind, bool) {
	all := Harnesses(p)
	if len(all) == 0 {
		return "", false
	}
	if h != "" {
		for _, k := range all {
			if HarnessOf(k) == h && Runs(k) {
				return k, true
			}
		}
	}
	for _, k := range all {
		if Runs(k) {
			return k, true
		}
	}
	return all[0], true
}

// ProviderInstalled is whether any of provider p's agents runs here.
func ProviderInstalled(p string) bool {
	return slices.ContainsFunc(Harnesses(p), Runs)
}
