package ui

import (
	"cmp"
	"os"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/fleet"
)

// @name at the start of a message sends the rest to that agent: from the
// Prompt instead of starting a session, from a Session instead of its own.
// Later in the message, the Prompt still sends it all to the agent tagged;
// a Session sends it to its own agent, told how to message the one tagged.

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

// atToken is the @tag the cursor is in: where its @ and its end are, and
// what's typed of it before the cursor. An @ inside a word (an email) is
// no tag.
func atToken(in []rune, back int) (at, end int, q string, ok bool) {
	cur := len(in) - back
	i := cur
	for i > 0 && !unicode.IsSpace(in[i-1]) && in[i-1] != '@' {
		i--
	}
	if i == 0 || in[i-1] != '@' || i > 1 && !unicode.IsSpace(in[i-2]) {
		return 0, 0, "", false
	}
	end = cur
	for end < len(in) && !unicode.IsSpace(in[end]) {
		end++
	}
	return i - 1, end, string(in[i:cur]), true
}

// completeMention is in with the tag the cursor is in made @name, and the
// cursor after it: back is how far from the end that is.
func completeMention(in []rune, back int, name string) ([]rune, int) {
	at, end, _, ok := atToken(in, back)
	if !ok {
		return in, back
	}
	tail := []rune(strings.TrimLeftFunc(string(in[end:]), unicode.IsSpace))
	return slices.Concat(in[:at], []rune("@"+name+" "), tail), len(tail)
}

// mentionsIn are the agents text tags past its start, each once.
func (m *Model) mentionsIn(text string) []*fleet.Agent {
	var out []*fleet.Agent
	for i, f := range strings.Fields(text) {
		tag, ok := strings.CutPrefix(f, "@")
		if !ok || i == 0 {
			continue
		}
		tag = strings.TrimRight(tag, ".,;:!?)'\"")
		for _, a := range m.order {
			if mentionable(a) && strings.EqualFold(mentionName(a), tag) && !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// rushExe is how a session's shell runs this rush.
var rushExe = func() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "rush"
}()

// withMentions is text for the Session self's agent, with a note on each
// agent it tags mid-message: who it is, and how its shell messages it.
func (m *Model) withMentions(text, self string) string {
	var notes []string
	for _, a := range m.mentionsIn(text) {
		if a.Key == self {
			continue
		}
		n := "@" + mentionName(a) + " is " + oneLine(a.DisplayName) + ", another agent rush runs"
		if a.Cwd != "" {
			n += ", in " + a.Cwd
		}
		if a.Rush {
			n += ". To message it, pipe the message to `" + rushExe + " session send " + a.ID + "` (add --now to reach it mid-turn)."
		} else {
			n += "; it isn't a rush session, so it can't be messaged from your shell."
		}
		if t := cmp.Or(a.TranscriptPath, a.History); t != "" {
			n += " Its conversation so far is in " + t + "."
		}
		notes = append(notes, n)
	}
	if len(notes) == 0 {
		return text
	}
	return text + "\n\n" + strings.Join(notes, "\n")
}

// mentionMatches is what the picker offers while a tag is typed, anywhere
// in the message: the agents whose name holds it, in the list's order so
// each section's stay together, with the section they're in.
func (m *Model) mentionMatches(in []rune, back int) []event.Command {
	_, _, q, ok := atToken(in, back)
	if !ok {
		return nil
	}
	q = strings.ToLower(q)
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

const mentionHow = "↑↓ · tab or enter tags · first, the message goes to them; later, the agent here is told how to reach them"
