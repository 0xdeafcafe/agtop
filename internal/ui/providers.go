package ui

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
	"github.com/0xdeafcafe/rush/internal/theme"
)

// Every provider has a glyph and a colour of its own, taken from its
// brand where it has one, so which one a session, the header or the top
// bar is on can be told at a glance.

// look is a provider's glyph and colour, the colour as it is on rush's
// dark ground.
type look struct {
	glyph string
	rgb   theme.RGB
}

var (
	// builtinLook is Claude Code's: its starburst, in rush's orange.
	builtinLook = look{"✻", theme.RGB{R: 217, G: 119, B: 87}}
	// otherLook is a provider without one of its own.
	otherLook = look{"◇", theme.RGB{R: 143, G: 179, B: 217}}
	// looks are the rest, by kind.
	looks = map[agent.Kind]look{
		"codex":    {"◎", theme.RGB{R: 94, G: 199, B: 160}},  // OpenAI's green
		"copilot":  {"◈", theme.RGB{R: 178, G: 150, B: 230}}, // GitHub's purple
		"deepseek": {"◆", theme.RGB{R: 116, G: 146, B: 255}}, // DeepSeek's blue
		"glm":      {"▲", theme.RGB{R: 96, G: 196, B: 222}},  // Z.ai's cyan
		"gemini":   {"✦", theme.RGB{R: 138, G: 180, B: 248}}, // Gemini's sparkle
		"kimi":     {"◐", theme.RGB{R: 200, G: 200, B: 214}}, // Moonshot's moon
		"vibe":     {"■", theme.RGB{R: 245, G: 165, B: 60}},  // Mistral's amber
		"ollama":   {"◉", theme.RGB{R: 232, G: 232, B: 226}}, // Ollama's white llama
		"opencode": {"▣", theme.RGB{R: 186, G: 182, B: 176}},
		"pi":       {"π", theme.RGB{R: 230, G: 190, B: 120}},
	}
)

// unmarked is whether provider k's rows go without its mark, as they were
// before rush ran other agents: the one whose accounts are rush's logins.
func unmarked(k agent.Kind) bool { return k == loginsKind }

// lookOf is provider k's look.
func lookOf(k agent.Kind) look {
	if unmarked(k) {
		return builtinLook
	}
	if l, ok := looks[k]; ok {
		return l
	}
	// A provider in another's harness looks like the provider: Ollama in
	// Pi is Ollama's.
	if l, ok := looks[agent.Kind(agent.ProviderOf(k))]; ok {
		return l
	}
	return otherLook
}

// colour is the look's colour on the terminal's ground, as an accent.
func (l look) colour() string { return painted.Accent(l.rgb).FG() }

// glyph is provider k's glyph in its colour.
func glyph(k agent.Kind) string {
	l := lookOf(k)
	return paint(l.colour(), l.glyph)
}

// spinOf is frame n of what a run of provider k shows while it works: its
// harness's spinner (Claude Code's star, Codex's turning circle, another's
// glyph) in the provider's colour.
func spinOf(k agent.Kind, n int) string {
	h := agent.HarnessOf(k)
	frames := []string{lookOf(h).glyph}
	switch h {
	case loginsKind:
		frames = spinner
	case "codex":
		frames = codexSpin
	}
	return paint(lookOf(k).colour(), frames[n%len(frames)])
}

var codexSpin = []string{"◐", "◓", "◑", "◒"}

// kindOf is session c's provider: Claude Code's when it doesn't say.
func (c *hostConn) kindOf() agent.Kind {
	return agent.Kind(firstNonEmpty(c.sess.Info.Kind, string(loginsKind)))
}

// providerTag is provider k's glyph and short name, in its colour.
func providerTag(k agent.Kind) string {
	l := lookOf(k)
	return paint(l.colour(), l.glyph+" "+kindName(k))
}

// rowBadge is a session row's mark of its provider: none for Claude Code,
// so its rows stay as they were; the glyph and kind for any other.
func rowBadge(a *fleet.Agent) string {
	k := agent.Kind(a.Kind)
	if unmarked(k) {
		return ""
	}
	l := lookOf(k)
	return paint(l.colour(), l.glyph+" "+string(k))
}

// showProfile is whether a profile's name is worth showing: it isn't the
// default, or you've made profiles of your own to tell it from.
func (m *Model) showProfile(name string) bool {
	cfg := m.store.Config
	return name != "" && (len(cfg.Profiles) > 0 || !strings.EqualFold(name, cfg.Default().Name))
}

// providerName is provider p by the company behind its models, or its
// agent's short name when the adapter doesn't say.
func providerName(p string) string {
	if n := agent.ProviderName(p); n != "" {
		return n
	}
	return kindName(agent.Kind(p))
}

// accountOf is the account provider k is signed in as, when rush keeps
// more than the one: a Claude Code login, or another agent's sign-in.
func (m *Model) accountOf(k agent.Kind) string {
	if k != loginsKind {
		return m.inUseOf(string(k))
	}
	for _, l := range m.snap.Logins {
		if l.Current {
			return l.Name
		}
	}
	return ""
}

// startTag is what the top bar says new sessions start on: the provider's
// glyph, the account, the provider's name when it isn't Claude Code, and
// the profile when there's more than one or #profile picked one.
func (m *Model) startTag() string {
	k := agent.Kind(m.startKind())
	l := lookOf(k)
	parts := []string{paint(l.colour(), l.glyph) + " " + dim(m.startAccount())}
	if p := m.startProfile(m.startDir()).Name; m.showProfile(p) || m.accts.profile != "" {
		parts = append(parts, faint(p))
	}
	return strings.Join(parts, faint(" · "))
}

// usageTag is whose usage the header shows: the provider's glyph, the
// account new sessions start on, and their profile, always named.
func (m *Model) usageTag() string {
	l := lookOf(agent.Kind(m.startKind()))
	s := paint(l.colour(), l.glyph) + " " + paint(cText, m.startAccount())
	if p := m.startProfile(m.startDir()).Name; p != "" {
		s += faint(" · ") + dim(p)
	}
	return s
}

// runsOn is what an agent runs on, in startWith's words, each only when
// known: its provider unless it's Claude Code, its model, its effort.
func runsOn(k agent.Kind, model, effort string) string {
	var words []string
	if n := kindName(k); n != "" && !unmarked(k) {
		words = append(words, n)
	}
	if model != "" && !strings.HasPrefix(model, "<") { // not <synthetic>
		words = append(words, modelWord(string(k), model))
	}
	if effort != "" {
		words = append(words, effort+" effort")
	}
	return strings.Join(words, " · ")
}

// providerIn is agent k as its provider in its harness, "Ollama in
// Codex", or just its name when it's its own harness.
func providerIn(k agent.Kind) string {
	if agent.HarnessOf(k) == k {
		return kindName(k)
	}
	return agent.ProviderLabel(agent.ProviderOf(k)) + " in " + agent.HarnessLabel(k)
}

// startWith is what a new session in dir starts as: its agent, the model
// and effort that agent's Settings page gives it (the agent's own default
// when it gives none), and the profile when there's more than one or
// #profile picked one. Plain is without colour, for text that's matched.
func (m *Model) startWith(dir string, plain bool) string {
	st := m.nextStart(dir)
	k := st.kind
	model := "default model"
	if st.model != "" {
		model = modelWord(k, st.model)
	}
	words := []string{providerIn(agent.Kind(k)), model}
	if st.effort != "" {
		words = append(words, st.effort+" effort")
	}
	if st.billing == "key" {
		words = append(words, "API key")
	}
	if p := m.startProfile(dir).Name; m.showProfile(p) || m.accts.profile != "" {
		words = append(words, p)
	}
	if plain {
		return strings.Join(words, " · ")
	}
	words[0] = providerTag(agent.Kind(k))
	for i := 1; i < len(words); i++ {
		words[i] = dim(words[i])
	}
	return strings.Join(words, faint(" · "))
}

// levelWords say what each support level means.
var levelWords = map[agent.Level]string{
	agent.LevelFull:    "everything rush does, used every day",
	agent.LevelTested:  "tried against its real program",
	agent.LevelPreview: "built, not yet tried against its real program",
}

// levelChip is provider k's support level, coloured: full green, tested
// blue, preview yellow.
func levelChip(k agent.Kind) string {
	lv := agent.LevelOf(k)
	c := map[agent.Level]string{agent.LevelFull: cGreen, agent.LevelTested: cBlue, agent.LevelPreview: cYellow}[lv]
	return paint(c, fit(lv.String(), 8))
}

// chain is a profile's installed providers, glyph and name, in order: an
// arrow between them when new sessions move on, "only" when they stay.
func (m *Model) chain(p state.Profile) string {
	inst := p.Installed()
	if len(inst) == 0 {
		return paint(cYellow, "none of its providers is installed")
	}
	tag := func(k string) string {
		pr := agent.ProviderOf(agent.Kind(k))
		l := lookOf(agent.Kind(pr))
		return paint(l.colour(), l.glyph+" "+provLabel(p.ID(pr))) + dim(runsInWords(agent.Kind(k)))
	}
	if !p.Mixes() || len(inst) == 1 {
		return tag(inst[0]) + dim(" only")
	}
	var parts []string
	for _, k := range inst {
		parts = append(parts, tag(k))
	}
	return strings.Join(parts, faint(" → "))
}

// notInstalled are the providers rush has an adapter for that aren't
// installed here, each with its support level.
func (m *Model) notInstalled() string {
	var out []string
	for _, a := range agent.All() {
		if !agent.Installed(a.Kind()) {
			out = append(out, glyph(a.Kind())+" "+dim(a.Name())+faint(" "+agent.LevelOf(a.Kind()).String()))
		}
	}
	return strings.Join(out, faint(" · "))
}
