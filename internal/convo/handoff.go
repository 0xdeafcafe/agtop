package convo

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Conversation is the session told for a hand-off to another agent
// (agent.Handoff): how it began, its calls, the files it changed,
// its last turns and its todo list.
func (s *Session) Conversation(from agent.Kind) agent.Conversation {
	c := agent.Conversation{From: from, Name: s.Info.Name, Cwd: s.Cwd}
	if c.Cwd == "" {
		c.Cwd = s.Info.Cwd
	}
	const recentSteps = 64
	count := 0
	for _, t := range s.Turns {
		if c.First == "" && t.From == "" {
			c.First = t.Prompt
		}
		for _, it := range t.Items {
			if it.Kind == KStep && it.Step != nil {
				count++
			}
		}
	}
	// A summary only displays the latest calls. Count older calls without
	// materializing their large tool.Call structs or growing a full copy.
	keep := min(count, recentSteps)
	c.EarlierSteps = count - keep
	c.Steps = make([]agent.Step, keep)
	for ti := len(s.Turns) - 1; ti >= 0 && keep > 0; ti-- {
		items := s.Turns[ti].Items
		for i := len(items) - 1; i >= 0 && keep > 0; i-- {
			it := items[i]
			if it.Kind != KStep || it.Step == nil {
				continue
			}
			keep--
			c.Steps[keep] = agent.Step{Call: it.Step.Call(), Failed: it.Step.Status == Failed}
		}
	}
	c.History = history(s.Turns)
	for _, f := range s.Changes() {
		c.Changed = append(c.Changed, f.Path)
	}
	turns := s.Turns
	if len(turns) > 3 {
		turns = turns[len(turns)-3:]
	}
	for _, t := range turns {
		for _, l := range turnHistory(t) {
			c.Recent = append(c.Recent, l)
		}
	}
	for _, t := range s.Tasks {
		c.Todos = append(c.Todos, agent.Todo{Label: t.Subject, Done: t.Status == "completed", Started: t.Status == "in_progress"})
	}
	return c
}

// historyMax is how much of a conversation a whole hand-off carries, the
// latest kept: about 50k tokens.
// ponytail: one cap for every model; fit it to the one taking it if a small one chokes.
const historyMax = 200_000

// history is every turn: its prompt, and its words and steps in order,
// each step a → line. The oldest go first past historyMax.
func history(turns []*Turn) []agent.Line {
	// Walk newest first so exporting a long session doesn't format thousands
	// of old tool calls that the cap would immediately discard.
	var out []agent.Line
	n := 0
	full := false
	for ti := len(turns) - 1; ti >= 0 && !full; ti-- {
		lines := turnHistory(turns[ti])
		for i := len(lines) - 1; i >= 0; i-- {
			l := lines[i]
			// Reserve room for both a user message and its answer, even when
			// one is larger than the entire handoff budget.
			l.Text = handoffClip(l.Text, historyMax/2)
			if n+len(l.Text) > historyMax {
				full = true
				break
			}
			n += len(l.Text)
			out = append(out, l)
		}
	}
	slices.Reverse(out)
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out
}

func turnHistory(t *Turn) []agent.Line {
	var out []agent.Line
	if t.Prompt != "" {
		out = append(out, agent.Line{Role: "user", Text: t.Prompt, At: t.Start})
	}
	var said []string
	flush := func() {
		if len(said) > 0 {
			out = append(out, agent.Line{Role: "assistant", Text: strings.Join(said, "\n\n"), At: t.End})
			said = nil
		}
	}
	for _, it := range t.Items {
		switch {
		case it.Kind == KExchange && it.Exchange != nil:
			said = append(said, "[Agent exchange: "+peerLabel(it.Exchange.Sender)+" → "+peerLabel(it.Exchange.Receiver)+"]\n"+it.Text)
		case it.Kind == KInterject && it.Text != "":
			flush()
			out = append(out, agent.Line{Role: "user", Text: it.Text, At: t.Start})
		case it.Kind == KText && strings.TrimSpace(it.Text) != "":
			said = append(said, strings.TrimSpace(it.Text))
		case it.Kind == KStep && it.Step != nil:
			if d := tool.Doing(it.Step.Call()); d != "" {
				if it.Step.Status == Failed {
					d += " (failed)"
				}
				said = append(said, "→ "+d)
			}
		}
	}
	flush()
	return out
}

// Keep the start and end of an oversized message; do not split UTF-8.
func handoffClip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const gap = "\n[… middle omitted for handoff …]\n"
	head := (max - len(gap)) / 2
	tail := len(s) - (max - len(gap) - head)
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	return s[:head] + gap + s[tail:]
}
