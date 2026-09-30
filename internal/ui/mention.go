package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// @name at the start of a message sends the rest to that agent: from the
// Prompt instead of starting a session, from a Session instead of its own.

// mentionName is how an agent is tagged: its name, spaces as dashes.
func mentionName(a *fleet.Agent) string {
	return strings.Join(strings.Fields(oneLine(a.DisplayName)), "-")
}

// mentionable is whether a message can reach it from here.
func mentionable(a *fleet.Agent) bool { return !a.Interactive && !a.Headless }

// mentioned is the agent text starts by tagging, and the message after it.
func (m *Model) mentioned(text string) (*fleet.Agent, string) {
	if !strings.HasPrefix(text, "@") {
		return nil, ""
	}
	tag, rest := text[1:], ""
	if i := strings.IndexFunc(tag, unicode.IsSpace); i >= 0 {
		tag, rest = tag[:i], strings.TrimSpace(tag[i:])
	}
	for _, a := range m.order {
		if mentionable(a) && strings.EqualFold(mentionName(a), tag) {
			return a, rest
		}
	}
	return nil, ""
}

// sendMentioned sends text to the agent it tags, if it tags one; tagged
// is the same text with its pastes marked, for rush sessions.
func (m *Model) sendMentioned(text, tagged string) (tea.Cmd, bool) {
	a, rest := m.mentioned(text)
	if a == nil {
		return nil, false
	}
	if rest == "" {
		m.flash("type the message for "+a.DisplayName+" after its tag", true)
		return nil, true
	}
	if _, t := m.mentioned(tagged); t != "" {
		tagged = t
	}
	return m.replyTo(a, rest, tagged), true
}

// mentionMatches is what the picker offers while a tag is typed: the
// agents whose name holds it, in the list's order so each section's stay
// together, with the section they're in.
func (m *Model) mentionMatches(in []rune, back int) []event.Command {
	text := string(in)
	if back != 0 || !strings.HasPrefix(text, "@") || strings.ContainsFunc(text, unicode.IsSpace) {
		return nil
	}
	q := strings.ToLower(text[1:])
	var out []event.Command
	for _, a := range m.order {
		name := mentionName(a)
		if !mentionable(a) || !strings.Contains(strings.ToLower(name), q) {
			continue
		}
		d := m.groupOf[a.Key]
		if a.Branch != "" {
			d += " · " + a.Branch
		}
		out = append(out, event.Command{Name: name, Description: d, ArgumentHint: "<message>"})
	}
	return out
}

const mentionHow = "↑↓ · tab or enter tags · the message goes to them"
