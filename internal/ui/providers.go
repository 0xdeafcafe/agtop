package ui

import (
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/theme"
)

// Every provider has a glyph and a colour of its own, taken from its
// brand where it has one, so which one a session, the header or the top
// bar is on can be told at a glance.

// look is a provider's glyph and colour, the colour as it is on agtop's
// dark ground.
type look struct {
	glyph string
	rgb   theme.RGB
}

var (
	// builtinLook is Claude Code's: its starburst, in agtop's orange.
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
	}
)

// unmarked is whether provider k's rows go without its mark, as they were
// before agtop ran other agents: the one whose accounts are agtop's logins.
func unmarked(k agent.Kind) bool { return k == loginsKind }

// lookOf is provider k's look.
func lookOf(k agent.Kind) look {
	if unmarked(k) {
		return builtinLook
	}
	if l, ok := looks[k]; ok {
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

// showProfile is whether a profile's name is worth showing: there's more
// than one, or it isn't the default.
func (m *Model) showProfile(name string) bool {
	cfg := m.store.Config
	return name != "" && (len(cfg.Profiles) > 1 || !strings.EqualFold(name, cfg.Default().Name))
}

// sessionTag is what a session's header says it runs on: the provider's
// glyph and name, the account it's signed in as, and its profile when
// there's more than one.
func (m *Model) sessionTag(a *fleet.Agent) string {
	k := agent.Kind(a.Kind)
	parts := []string{providerTag(k)}
	if acct := m.accountOf(k); acct != "" {
		parts = append(parts, dim(acct))
	}
	if p := m.sessionProfile(a).Name; m.showProfile(p) {
		parts = append(parts, faint(p))
	}
	return strings.Join(parts, faint(" · "))
}

// accountOf is the account provider k is signed in as, when agtop keeps
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

// startWith is what a new session in dir starts as: its agent, the model
// and effort that agent's Settings page gives it (the agent's own default
// when it gives none), and the profile when there's more than one or
// #profile picked one. Plain is without colour, for text that's matched.
func (m *Model) startWith(dir string, plain bool) string {
	k := m.startKindIn(dir)
	st := m.store.Config.Dispatch.StartFor(k)
	model := "default model"
	if st.Model != "" {
		model = modelWord(k, st.Model)
	}
	words := []string{kindName(agent.Kind(k)), model}
	if st.Effort != "" {
		words = append(words, st.Effort+" effort")
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
	agent.LevelFull:    "everything agtop does, used every day",
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
	if !p.Mixes() {
		return providerTag(agent.Kind(inst[0])) + dim(" only")
	}
	var parts []string
	for _, k := range inst {
		parts = append(parts, providerTag(agent.Kind(k)))
	}
	return strings.Join(parts, faint(" → "))
}

// notInstalled are the providers agtop has an adapter for that aren't
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

// featureGrid is what agtop can do with provider k, feature by feature, in
// as many columns as fit: ✓ it can, – it can't, ◌ planned. A feature's
// note follows the grid.
func (m *Model) featureGrid(k agent.Kind, w int, label func(string) string) []string {
	const cell = 24
	cols := max(1, (w-12)/cell)
	all := agent.AllFeatures()
	rows := (len(all) + cols - 1) / cols
	var out, notes []string
	for r := range rows {
		line := label("")
		if r == 0 {
			line = label("features")
		}
		for c := range cols {
			i := c*rows + r // down the columns, so related features stay together
			if i >= len(all) {
				break
			}
			f := all[i]
			s := agent.FeatureOf(k, f.Feature)
			var mark string
			switch s.Is {
			case agent.StateYes:
				mark = paint(cGreen, "✓ ") + paint(cText, fit(f.Label, cell-2))
			case agent.StatePlanned:
				mark = paint(cYellow, "◌ ") + dim(fit(f.Label, cell-2))
			default:
				mark = faint("– " + fit(f.Label, cell-2))
			}
			line += mark
			if s.Note != "" {
				notes = append(notes, f.Label+": "+s.Note)
			}
		}
		out = append(out, line)
	}
	out = append(out, label("")+faint("✓ yes   – no   ◌ planned"))
	for _, n := range notes {
		out = append(out, label("")+faint(n))
	}
	return out
}
