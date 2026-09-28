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
		"opencode": {"▣", theme.RGB{R: 186, G: 182, B: 176}},
	}
)

// lookOf is provider k's look; an empty kind is the built-in agent's.
func lookOf(k agent.Kind) look {
	if agent.IsBuiltin(k) {
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
	return paint(l.colour(), l.glyph+" "+kindName(agent.Kind(state.KindOf(string(k)))))
}

// rowBadge is a session row's mark of its provider: none for Claude Code,
// so its rows stay as they were; the glyph and kind for any other.
func rowBadge(a *fleet.Agent) string {
	k := agent.Kind(a.Kind)
	if agent.IsBuiltin(k) {
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
	k := agent.Kind(state.KindOf(a.Kind))
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
