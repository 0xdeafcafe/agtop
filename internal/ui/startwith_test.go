package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// The Prompt says what a new session starts as: its agent, model, effort
// and profile, before enter starts it.
func TestPromptSaysWhatStarts(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.sel, m.preview, m.paneFocus = "", false, false
	m.store.Config.Dispatch.Kind = state.LoginsKind
	m.store.Config.Dispatch.SetStartFor(state.LoginsKind, state.Start{Model: "claude-opus-5-5[1m]", Effort: "high"})
	top := ansi.Strip(m.promptLines(200)[0])
	for _, want := range []string{"Opus 5.5", "high effort"} {
		if !strings.Contains(top, want) {
			t.Errorf("the Prompt's top edge %q doesn't say %q", top, want)
		}
	}
	m.store.Config.Dispatch.SetStartFor(state.LoginsKind, state.Start{})
	if top := ansi.Strip(m.promptLines(200)[0]); !strings.Contains(top, "default model") || strings.Contains(top, "effort") {
		t.Errorf("with nothing set it starts on the agent's own model: %q", top)
	}
	if w := m.startWith(m.startDir(), true); strings.Contains(w, "\x1b") {
		t.Errorf("plain is without colour: %q", w)
	}
}
