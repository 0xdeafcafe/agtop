package agent

import (
	"slices"
	"strings"
)

// A route is a provider (by id, as Billed reads it: "claude" is
// Anthropic's plan, "claude-key" its API key) in a harness: what a
// session runs on. The agent that runs it is an adapter, found by Compat;
// nothing a user sees or keeps names the adapter itself.
// docs/providers-harnesses.md has the model and the matrix.

// PlanRider is a rider that runs its provider on the plan its harness is
// signed in to, rather than with the provider's API key: Pi on your
// Anthropic plan, through Pi's own /login.
type PlanRider interface {
	OnPlan() bool
}

// onPlan is whether agent k runs its provider's plan in another's harness.
func onPlan(k Kind) bool {
	a, ok := Get(k)
	if !ok {
		return false
	}
	p, ok := a.(PlanRider)
	return ok && p.OnPlan()
}

// Speaker is a harness that speaks one provider's API alone, said for
// the routes it can't run: "Anthropic's API".
type Speaker interface {
	Speaks() string
}

// Worded is an adapter with other words to type it by, beside its kind
// and its name: cc for Claude Code.
type Worded interface {
	Words() []string
}

// HarnessKinds are every harness, each once, in the registry's order.
func HarnessKinds() []Kind {
	var out []Kind
	for _, a := range All() {
		if CurrentKind(a.Kind()) != a.Kind() {
			continue
		}
		if h := HarnessOf(a.Kind()); !slices.Contains(out, h) {
			if _, ok := Get(h); ok {
				out = append(out, h)
			}
		}
	}
	return out
}

// ProviderIDs are every provider as Billed reads it, each split one's
// key after its plan, whether installed or not.
func ProviderIDs() []string {
	var out []string
	for _, p := range Providers() {
		out = append(out, p)
		if Split(p) {
			out = append(out, KeyOf(p))
		}
	}
	return out
}

// Compat is how provider id runs in harness h: the agent that runs it,
// a warning when it does but you'd want to know, and why when it can't.
// k is set whenever a route exists, installed or not.
func Compat(id string, h Kind) (k Kind, warn, why string) {
	p, key := Billed(id)
	for _, c := range RunsFor(id) {
		if HarnessOf(c) == h {
			k = c
			break
		}
	}
	pn, hn := ProviderLabel(p), HarnessLabel(h)
	switch {
	case k == "" && !key && Split(p):
		why = pn + "'s plan runs in " + HarnessLabel(Kind(p)) + strings.Join(planHarnesses(p), "") + ", not " + hn
	case k == "":
		why = "rush can't run " + pn + " in " + hn
		if s, ok := As[Speaker](h); ok {
			why = hn + " speaks only " + s.Speaks()
		}
	case !Runs(k):
		why = hn + " isn't installed"
		if hint := Hint(k); hint != "" {
			why += ": " + hint
		}
	}
	if k != "" && onPlan(k) {
		warn = "runs your " + pn + " plan through " + hn
	}
	return k, warn, why
}

// planHarnesses are the other harnesses provider p's plan runs in, as
// words to follow its own: " or Pi".
func planHarnesses(p string) []string {
	var out []string
	for _, k := range RunsFor(p) {
		if k != Kind(p) {
			out = append(out, " or "+HarnessLabel(k))
		}
	}
	return out
}

// RouteOf is the provider id agent k is paid through and the harness it
// runs in: a plan rider's plan, a key rider's key, else as key says.
func RouteOf(k Kind, key bool) (id string, h Kind) {
	p := ProviderOf(k)
	if Split(p) && (key || KeyOnly(k)) {
		return KeyOf(p), HarnessOf(k)
	}
	return p, HarnessOf(k)
}

// ProviderWord is provider id as one word to type: anthropic-sub,
// openai-api, ollama.
func ProviderWord(id string) string {
	p, key := Billed(id)
	w := word(ProviderLabel(p))
	switch {
	case key:
		return w + "-api"
	case Split(p):
		return w + "-sub"
	}
	return w
}

// HarnessWord is harness h as one word to type: claude-code, codex, pi.
func HarnessWord(h Kind) string { return word(HarnessLabel(h)) }

// ProviderByWord is the provider id a typed word names: its word, its id,
// its company or agent's name; "" when none does.
func ProviderByWord(w string) string {
	w = strings.ToLower(w)
	for _, id := range ProviderIDs() {
		p, key := Billed(id)
		if w == ProviderWord(id) || w == id || !key && !Split(p) && w == word(Label(Kind(p))) {
			return id
		}
	}
	return ""
}

// HarnessByWord is the harness a typed word names: its word, its kind, its
// program's name or one of its Words; "" when none does.
func HarnessByWord(w string) Kind {
	w = strings.ToLower(w)
	for _, h := range HarnessKinds() {
		words := []string{HarnessWord(h), string(h), ProgramOf(h)}
		if wd, ok := As[Worded](h); ok {
			words = append(words, wd.Words()...)
		}
		if slices.Contains(words, w) {
			return h
		}
	}
	return ""
}

// word is a name as one word to type: "Claude Code" is claude-code.
func word(s string) string {
	s = strings.NewReplacer("(", "", ")", "", ".", "").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), "-"))
}
