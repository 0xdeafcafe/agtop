package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// --- /model ---

// modelChoice is one model /model offers: the alias Claude Code takes,
// its name in the picker, and what it's for, as Claude Code's own
// /model picker says it.
type modelChoice struct{ value, label, about string }

var modelChoices = []modelChoice{
	{"default", "Default (recommended)", "Use the account's default model"},
	{"fable", "Fable", "Fable 5.1 · Most capable for your hardest and longest-running tasks"},
	{"opus", "Opus", "Opus 5.5 · Best for everyday, complex tasks"},
	{"opus[1m]", "Opus (1M context)", "Opus 5.5 with 1M context · Best for everyday, complex tasks"},
	{"sonnet", "Sonnet", "Sonnet 5 · Efficient for routine tasks"},
	{"sonnet[1m]", "Sonnet 5 (1M context)", "Sonnet 5 for long sessions"},
	{"haiku", "Haiku", "Haiku 4.5 · Fastest for quick answers"},
	{"opusplan", "Opus Plan Mode", "Use Opus in plan mode, Sonnet otherwise"},
}

// currentModel is the value of the choice the session runs on: the one
// last picked here, else the one its model id belongs to ("" when none
// does, or it isn't known yet).
func currentModel(c *hostConn) string {
	if c.modelPick != "" {
		return c.modelPick
	}
	id := strings.ToLower(firstNonEmpty(c.sess.Model, c.sess.Info.Model))
	for _, fam := range []string{"fable", "opus", "sonnet", "haiku"} {
		if !strings.Contains(id, fam) {
			continue
		}
		if strings.Contains(id, "[1m]") && (fam == "opus" || fam == "sonnet") {
			return fam + "[1m]"
		}
		return fam
	}
	return ""
}

// modelSheet is /model with no model named: the models to pick from, the
// one running marked, as Claude Code's /model shows them.
type modelSheet struct {
	conn    string
	choices []modelChoice
	now     int // the running one's row, -1 when none
	cur     int
}

func (m *Model) openModels(c *hostConn) {
	s := &modelSheet{conn: c.key, choices: modelChoices, now: -1}
	now := currentModel(c)
	if id := firstNonEmpty(c.sess.Model, c.sess.Info.Model); now == "" && id != "" {
		// A model none of the aliases stand for still shows, to stay on.
		s.choices = append(append([]modelChoice{}, modelChoices...), modelChoice{id, convo.PrettyModel(id), "Custom model · " + id})
		now = id
	}
	for i, ch := range s.choices {
		if ch.value == now {
			s.now, s.cur = i, i
		}
	}
	m.sheet = s
}

func (s *modelSheet) width(*Model) int { return 100 }

func (s *modelSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Model", "for this session, from the next turn", w), ""}
	labelW := 0
	for _, ch := range s.choices {
		labelW = max(labelW, ansi.StringWidth(ch.label))
	}
	from, to := window(len(s.choices), s.cur, max(1, h-6))
	for i := from; i < to; i++ {
		ch := s.choices[i]
		mark := "  "
		if i == s.now {
			mark = paint(cGreen, "✓ ")
		}
		label := ch.label + strings.Repeat(" ", labelW-ansi.StringWidth(ch.label))
		line := mark + paint(cText+bold, label) + "  " + dim(ansi.Truncate(ch.about, max(0, w-labelW-8), "…"))
		out = append(out, sheetRow(line, i == s.cur, w))
	}
	return append(out, "", dim("/model <name> takes any model id"), "",
		keysFit(w, "↑↓", "choose", "enter", "switch", "esc", "cancel"))
}

func (s *modelSheet) key(m *Model, _ tea.KeyPressMsg, k string) tea.Cmd {
	switch k {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "down", "tab", "shift+tab":
		dir := "down"
		if k == "up" || k == "shift+tab" {
			dir = "up"
		}
		s.cur = pickerMove(s.cur, len(s.choices), dir)
	case "enter":
		m.sheet = nil
		c := m.sheetConn(s.conn)
		if c == nil {
			return nil
		}
		return m.setModel(c, s.choices[s.cur].value)
	}
	return nil
}

// setModel switches the session's model from its next turn, as /model
// <name> does; "default" goes back to the account's default.
func (m *Model) setModel(c *hostConn, model string) tea.Cmd {
	if c.client == nil {
		m.flash("/model works in agtop-mode sessions · /agtop moves this one over", true)
		return nil
	}
	c.modelPick = model
	m.flash("model: "+model+" from the next turn", false)
	if model == "default" {
		model = ""
	}
	return hostCmd(func() error { return c.client.SetModel(model) })
}

// modelArgs are the models "/model " offers as you type one.
func modelArgs() []headless.Command {
	out := make([]headless.Command, 0, len(modelChoices))
	for _, ch := range modelChoices {
		out = append(out, headless.Command{Name: ch.value, Description: ch.about})
	}
	return out
}
