package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/adapters/ollama"
	"github.com/0xdeafcafe/rush/internal/agent"
)

func (m *Model) ollamaModelSettings(model string) []setting {
	value := "automatic"
	if n := ollama.ContextSize(model); n > 0 {
		value = tokens(int64(n))
	}
	info := "Loaded on demand when a session starts. Context changes apply to new sessions."
	if known, ok := ollama.CachedModel(model); ok {
		if known.Context > 0 {
			info += fmt.Sprintf(" Currently loaded: %s tokens.", tokens(int64(known.Context)))
		}
		if known.MaxContext > 0 {
			info += fmt.Sprintf(" Model maximum: %s tokens.", tokens(int64(known.MaxContext)))
		}
	}
	contextRow := setting{label: "Context size", value: value, what: info, keys: []string{"enter", "change"}}
	contextRow.key = func(s string) (tea.Cmd, bool) {
		if s != "enter" {
			return nil, false
		}
		m.ask("context tokens (0 = automatic)", strconv.Itoa(ollama.ContextSize(model)), func(input string) tea.Cmd {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil {
				m.flash("Enter a whole number of context tokens", true)
				return nil
			}
			return sheetDo(func() (struct{}, error) { return struct{}{}, ollama.SetContextSize(model, n) }, func(m *Model, _ struct{}, err error) tea.Cmd {
				if err != nil {
					m.flash(err.Error(), true)
				} else {
					m.flash("Context saved for new Ollama sessions", false)
				}
				return nil
			})
		})
		return nil, true
	}
	refresh := setting{label: "Refresh local models", what: "Starts Ollama if needed and discovers installed models. Models load into memory only when used.", keys: []string{"enter", "refresh"}}
	refresh.key = func(s string) (tea.Cmd, bool) {
		if s != "enter" {
			return nil, false
		}
		return sheetDo(func() ([]ollama.Model, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return ollama.InstalledModels(ctx)
		}, func(m *Model, models []ollama.Model, err error) tea.Cmd {
			if err != nil {
				m.flash(err.Error(), true)
				return nil
			}
			choices := make([]agent.Choice, 0, len(models))
			for _, model := range models {
				if model.CanCode() {
					choices = append(choices, agent.Choice{ID: model.Name, Context: int64(model.MaxContext), Note: model.Params + " · loads on demand"})
				}
			}
			if m.listed == nil {
				m.listed = map[string][]agent.Choice{}
			}
			for _, k := range []agent.Kind{ollama.Kind, ollama.CodexKind, ollama.PiKind, ollama.VibeKind} {
				m.listed[string(k)] = choices
			}
			m.flash(fmt.Sprintf("Ollama ready · %d installed models · %d support coding tools", len(models), len(choices)), false)
			return nil
		}), true
	}
	rows := []setting{refresh}
	if model != "" {
		rows = []setting{contextRow, refresh}
	}
	for _, model := range ollama.CachedInstalledModels() {
		if !model.CanCode() {
			value, why := "no coding tools", "Installed in Ollama. This model does not advertise tool calling, which Rush coding harnesses require."
			if model.Can("decision") {
				value, why = "decision classifier", "Installed in Ollama. Selects one supplied option; cannot run a coding harness or summarize a conversation."
			}
			rows = append(rows, setting{label: model.Name, value: value, what: why})
		}
	}
	return rows
}
