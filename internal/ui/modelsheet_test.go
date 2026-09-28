package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// /model with no model named opens a picker of the models, the running
// one marked; enter switches to the one chosen, esc leaves it be.
func TestModelPicker(t *testing.T) {
	m, c := infoModel(t) // runs claude-opus-5-5
	c.input = []rune("/model")
	m.sendPane(c, false)
	s, ok := m.sheet.(*modelSheet)
	if !ok || len(c.input) != 0 {
		t.Fatalf("/model opened %T, input %q", m.sheet, string(c.input))
	}
	text := ansi.Strip(strings.Join(s.body(m, 84, 40), "\n"))
	for _, want := range []string{"Model", "Default (recommended)", "Fable", "Opus (1M context)", "Sonnet 5 (1M context)", "Haiku 4.5 · Fastest for quick answers", "Opus Plan Mode", "esc cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("picker lacks %q:\n%s", want, text)
		}
	}
	if s.choices[s.now].value != "opus" || s.cur != s.now || !strings.Contains(text, "✓ Opus  ") {
		t.Fatalf("running model not marked: now %d cur %d\n%s", s.now, s.cur, text)
	}

	// esc cancels: nothing switches.
	s.key(m, tea.KeyPressMsg{}, "esc")
	if m.sheet != nil || c.modelPick != "" {
		t.Fatalf("esc: sheet %T, pick %q", m.sheet, c.modelPick)
	}

	// Down twice from Opus is Sonnet; enter switches to it.
	m.openModels(c)
	s = m.sheet.(*modelSheet)
	s.key(m, tea.KeyPressMsg{}, "down")
	s.key(m, tea.KeyPressMsg{}, "down")
	if cmd := s.key(m, tea.KeyPressMsg{}, "enter"); cmd == nil || m.sheet != nil || c.modelPick != "sonnet" {
		t.Fatalf("enter: cmd %v sheet %T pick %q", cmd != nil, m.sheet, c.modelPick)
	}
	if !strings.Contains(m.status, "model: sonnet") {
		t.Fatalf("status %q", m.status)
	}
	// It's the one marked from then on, here and as you type /model <name>.
	m.openModels(c)
	if s = m.sheet.(*modelSheet); s.choices[s.now].value != "sonnet" {
		t.Fatalf("marked %q after switching", s.choices[s.now].value)
	}
	m.sheet = nil
	c.input = []rune("/model so")
	if got := argMatches(c); len(got) != 2 || got[0].Name != "model sonnet" || !strings.HasSuffix(got[0].Description, "now") {
		t.Fatalf("/model so: %v", got)
	}

	// /model <name> still switches right away.
	c.input = []rune("/model haiku")
	m.sendPane(c, false)
	if m.sheet != nil || c.modelPick != "haiku" {
		t.Fatalf("/model haiku: sheet %T pick %q", m.sheet, c.modelPick)
	}

	// A model no alias stands for gets its own row, marked.
	c.modelPick, c.sess.Model = "", "claude-mythos-5"
	m.openModels(c)
	s = m.sheet.(*modelSheet)
	if last := s.choices[len(s.choices)-1]; s.now != len(s.choices)-1 || last.value != "claude-mythos-5" {
		t.Fatalf("custom model: now %d last %+v", s.now, last)
	}
}

// Solo takes /model the same way, the picker over the one session.
func TestModelPickerSolo(t *testing.T) {
	draftHome(t)
	writeSession(t, "aaaa1111", "the solo session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(160, 45)
	c := &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}, client: &host.Client{}}
	c.sess.Model = "claude-sonnet-5[1m]"
	m.host = c
	for _, k := range "/model" {
		soloPress(m, string(k))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := m.sheet.(*modelSheet); !ok {
		t.Fatalf("enter on /model opened %T (box %q)", m.sheet, string(c.input))
	}
	out := ansi.Strip(m.Frame(160, 45))
	for _, want := range []string{"Model", "✓ Sonnet 5 (1M context)", "Haiku", "enter switch"} {
		if !strings.Contains(out, want) {
			t.Errorf("solo frame lacks %q:\n%s", want, out)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil || m.sheet != nil || c.modelPick != "haiku" {
		t.Fatalf("enter in solo: sheet %T pick %q", m.sheet, c.modelPick)
	}
	m.openModels(c)
	soloPress(m, "esc")
	if m.sheet != nil || c.modelPick != "haiku" {
		t.Fatalf("esc in solo: sheet %T pick %q", m.sheet, c.modelPick)
	}
}
