package agent

import (
	"fmt"
	"strings"
)

// Conversation is a session told for a hand-off: what another agent needs
// to carry it on.
type Conversation struct {
	From    Kind   // the agent it ran on
	Name    string // what it's called
	Cwd     string
	First   string   // the message that started it
	Done    []string // what was done, in words, oldest first: "ran go test", "editing view.go"
	Changed []string // the files it changed
	Recent  []Line   // its last turns: your messages (user) and its answers (assistant)
	Todos   []Todo   // its todo list as it stands
}

// How much of a conversation a hand-off carries: enough to go on with,
// short enough to leave the new session room.
const (
	handoffDone    = 30
	handoffChanged = 40
	handoffTurns   = 6
	handoffText    = 2000
	handoffFirst   = 4000
)

// Handoff is the first message of a new session, on another agent, that
// carries c on: where it ran, how it started, what it did, where it left
// off and what's still to do.
func Handoff(c Conversation) Input {
	from := string(c.From)
	if a, ok := Get(c.From); ok {
		from = a.Name()
	}
	var b strings.Builder
	title := ""
	if c.Name != "" {
		title = " (" + c.Name + ")"
	}
	fmt.Fprintf(&b, "You're taking over a conversation%s that ran in %s and can't go on there. Here is where it stands; carry it on from here.\n", title, from)
	if c.Cwd != "" {
		fmt.Fprintf(&b, "\nIt worked in %s.\n", c.Cwd)
	}
	if first := clip(c.First, handoffFirst); first != "" {
		b.WriteString("\nIt started with this message:\n")
		quote(&b, first)
	}
	if len(c.Done) > 0 {
		b.WriteString("\nWhat it did:\n")
		done := c.Done
		if n := len(done) - handoffDone; n > 0 {
			fmt.Fprintf(&b, "- (%d earlier steps)\n", n)
			done = done[n:]
		}
		for _, d := range done {
			b.WriteString("- " + oneLine(d) + "\n")
		}
	}
	if len(c.Changed) > 0 {
		changed := c.Changed
		more := ""
		if n := len(changed) - handoffChanged; n > 0 {
			changed, more = changed[:handoffChanged], fmt.Sprintf(" and %d more", n)
		}
		fmt.Fprintf(&b, "\nFiles it changed: %s%s.\n", strings.Join(changed, ", "), more)
	}
	if len(c.Recent) > 0 {
		b.WriteString("\nWhere it left off:\n")
		recent := c.Recent
		if n := len(recent) - handoffTurns; n > 0 {
			recent = recent[n:]
		}
		for _, l := range recent {
			who := from
			if l.Role == "user" {
				who = "The user"
			}
			b.WriteString("\n" + who + ":\n")
			quote(&b, clip(l.Text, handoffText))
		}
	}
	var open []Todo
	for _, t := range c.Todos {
		if !t.Done {
			open = append(open, t)
		}
	}
	if len(open) > 0 {
		b.WriteString("\nStill to do:\n")
		for _, t := range open {
			mark := "[ ]"
			if t.Started {
				mark = "[in progress]"
			}
			b.WriteString("- " + mark + " " + oneLine(t.Label) + "\n")
		}
	}
	b.WriteString("\nThe files are as it left them: look at them before changing anything again. Carry on with what the user asked.")
	return Input{Text: b.String()}
}

// quote writes s as a quoted block.
func quote(b *strings.Builder, s string) {
	for _, l := range strings.Split(s, "\n") {
		b.WriteString("> " + l + "\n")
	}
}

// clip is s, trimmed, cut to n bytes at a line or word where it can be.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, "\n "); i > n/2 {
		cut = cut[:i]
	}
	return strings.ToValidUTF8(cut, "") + " …"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
