package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// An installed agent's part of its provider's page: what its new
// sessions start with (the same rows for every agent, their values its
// adapter's Choices), then whatever sections the agent adds itself.

// agentExtras are the sections an agent adds to its page, by kind.
var agentExtras = map[agent.Kind]func(m *Model) []section{}

// agentSections are what agent k's new sessions start with, then its own
// sections, the advanced ones folded under a line of their own.
func (m *Model) agentSections(k agent.Kind) []section {
	secs := []section{m.startSection("", k)}
	var advanced, extras []section
	if extra := agentExtras[k]; extra != nil {
		extras = extra(m)
	}
	extras = append(extras, m.fileSections(k)...)
	for _, s := range extras {
		if s.advanced {
			advanced = append(advanced, s)
		} else {
			secs = append(secs, s)
		}
	}
	if len(advanced) == 0 {
		return secs
	}
	var names []string
	for _, s := range advanced {
		names = append(names, s.title)
	}
	open := m.dialog.advanced
	toggle := setting{
		label: "Advanced",
		line: func(int) string {
			mark := "▸ "
			if open {
				mark = "▾ "
			}
			return dim(mark+"Advanced") + faint(" · "+strings.Join(names, ", "))
		},
		key: func(s string) (tea.Cmd, bool) {
			if s == "enter" || s == "right" || s == "left" || s == "space" {
				m.dialog.advanced = !m.dialog.advanced
				return nil, true
			}
			return nil, false
		},
		keys: []string{"enter", "show or hide"},
		about: func() (string, string, string) {
			return "Advanced", "What " + harnessName(string(k)) + " itself reads, beyond what rush starts it with: " + strings.Join(names, ", ") + ". Most people never need these.", ""
		},
	}
	secs = append(secs, section{rows: []setting{toggle}})
	if open {
		secs = append(secs, advanced...)
	}
	return secs
}

// modelTakes is what model takes in agent k: images and PDFs as its
// MediaReader says, and its context window, each only when known. Nothing
// here waits: Ollama's Reads asks in the background.
func modelTakes(k agent.Kind, model string) string {
	var out []string
	if reader, ok := agent.As[agent.MediaReader](k); ok {
		if has, ok := reader.Reads(model); ok {
			for _, m := range []struct {
				bit  agent.Media
				name string
			}{{agent.MediaImage, "images"}, {agent.MediaPDF, "PDFs"}} {
				if has&m.bit != 0 {
					out = append(out, paint(cGreen, "✓ ")+dim(m.name))
				} else {
					out = append(out, faint("– "+m.name))
				}
			}
		}
	}
	if windower, ok := agent.As[agent.ContextWindower](k); ok {
		if n := windower.ContextWindow(model); n > 0 {
			out = append(out, dim(tokens(n)+" context"))
		}
	}
	if len(out) == 0 {
		return faint("what it takes is unknown to rush until it's used")
	}
	return strings.Join(out, faint(" · "))
}

// agentModels are the models agent k's adapter offers, then any your
// sessions have run that it didn't list.
func (m *Model) agentModels(k agent.Kind) []agent.Choice {
	models := m.models(string(k))
	seen := map[string]bool{}
	for _, c := range models {
		seen[c.ID] = true
	}
	for _, a := range m.snap.Agents {
		if model := a.Spend.Model; model != "" && !seen[model] && a.Kind == string(k) && !strings.HasPrefix(model, "<") {
			seen[model] = true
			models = append(models, agent.Choice{ID: model, Note: "one your " + agentName(string(k)) + " sessions have run."})
		}
	}
	return models
}

// startSection is what agent k's new sessions on provider id start with
// (empty is k's own way): a model, an effort and a permission mode, each
// the agent's own default until set.
func (m *Model) startSection(id string, k agent.Kind) section {
	name := agentName(string(k))
	sec := section{title: "New sessions start with", note: "sessions rush starts; running ones keep theirs"}
	if !agent.Supports(k, agent.FeatureRun) {
		sec.rows = append(sec.rows, setting{
			label: "rush can't start its sessions yet",
			line: func(int) string {
				return faint("rush can't start " + name + " sessions yet, so there's nothing to choose here.")
			},
			about: func() (string, string, string) {
				return name, "rush shows " + name + "'s sessions but can't start them itself yet.", ""
			},
		})
		return sec
	}
	d := &m.store.Config.Dispatch
	st := d.StartOn(id, k)
	ch, _ := agent.ChoicesOf(k)
	change := func(f func(*state.Start, string)) func(string) {
		return func(v string) {
			s := d.StartOn(id, k)
			f(&s, v)
			d.SetStartOn(id, k, s)
		}
	}
	row := func(label, value, what string, list []agent.Choice, set func(*state.Start, string)) setting {
		pairs := [][2]string{{"", name + " picks, from its own settings."}}
		for _, c := range list {
			pairs = append(pairs, [2]string{c.ID, c.Note})
		}
		r := choiceSetting(label, value, what, pairs, change(set))
		r.unset = name + "'s default"
		return r
	}

	// Models: the adapter's and your sessions', then one you type.
	models := m.agentModels(k)
	if st.Model != "" && !slices.ContainsFunc(models, func(c agent.Choice) bool { return c.ID == st.Model }) {
		models = append(models, agent.Choice{ID: st.Model, Note: "typed in."})
	}
	model := row("Model", st.Model, "The model a new "+name+" session starts with. You can change a running session's with /model.", models,
		func(s *state.Start, v string) { s.Model = v })
	model.key = func(s string) (tea.Cmd, bool) {
		if s != "t" {
			return nil, false
		}
		m.ask("model", st.Model, func(v string) tea.Cmd {
			change(func(s *state.Start, v string) { s.Model = v })(v)
			_ = m.store.SaveConfig()
			return nil
		})
		return nil, true
	}
	model.keys = []string{"t", "type one"}
	sec.rows = append(sec.rows, model)

	if agent.Supports(k, agent.FeatureEffort) {
		r := row("Effort", st.Effort, "How hard a new "+name+" session thinks before acting: more is slower and spends more.", ch.Efforts,
			func(s *state.Start, v string) { s.Effort = v })
		r.typed = len(ch.Efforts) == 0
		sec.rows = append(sec.rows, r)
	}
	if agent.Supports(k, agent.FeatureModes) {
		r := row("Permissions", st.Mode, "What a new "+name+" session may do without asking you.", ch.Modes,
			func(s *state.Start, v string) { s.Mode = v })
		r.typed = len(ch.Modes) == 0
		sec.rows = append(sec.rows, r)
	}
	return sec
}
