package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Agents is one installed agent at a time, picked with 1-9 from the row
// of them at the top: who it is and where it lives, what its new
// sessions start with (the same rows for every agent, their values its
// adapter's Choices), then whatever sections the agent adds itself.

// agentExtras are the sections an agent adds to its page, by kind.
var agentExtras = map[agent.Kind]func(m *Model) []section{}

var agentsPage = page{
	name: "Agents",
	keys: []string{"1-9", "agent"},
	head: func(m *Model, w int) []string {
		k := m.settingsAgent()
		return append([]string{"", m.agentStrip(k, w)}, m.agentHead(k, w)...)
	},
	pre: func(m *Model, s string) (tea.Cmd, bool) {
		d := m.dialog
		if n := int(s[0] - '0'); len(s) == 1 && n >= 1 && n <= 9 {
			if order := m.agentOrder(); n <= len(order) {
				d.agent, d.cursor = order[n-1].Kind(), 0
			}
			return nil, true
		}
		return nil, false
	},
	form: func(m *Model) []section {
		k := m.settingsAgent()
		if k == "" {
			return nil
		}
		secs := []section{m.startSection(k)}
		var advanced []section
		var extras []section
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
		// The advanced sections fold under one line, open or shut.
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
				return "Advanced", "What " + agentName(string(k)) + " itself reads, beyond what rush starts it with: " + strings.Join(names, ", ") + ". Most people never need these.", ""
			},
		}
		secs = append(secs, section{title: "", rows: []setting{toggle}})
		if open {
			secs = append(secs, advanced...)
		}
		return secs
	},
}

// settingsAgent is the agent Agents shows: the one picked, else the
// first installed.
func (m *Model) settingsAgent() agent.Kind {
	order := m.agentOrder()
	for _, a := range order {
		if m.dialog != nil && a.Kind() == m.dialog.agent {
			return a.Kind()
		}
	}
	if len(order) == 0 {
		return ""
	}
	return order[0].Kind()
}

// agentStrip is the installed agents, numbered, the one showing bright.
func (m *Model) agentStrip(cur agent.Kind, w int) string {
	var out []string
	for i, a := range m.agentOrder() {
		n := faint(fmt.Sprintf("%d ", i+1))
		name := dim(kindName(a.Kind()))
		if a.Kind() == cur {
			name = paint(cOrange, "▸") + paint(cText+bold, kindName(a.Kind()))
		}
		out = append(out, n+name)
	}
	if len(out) == 0 {
		return dim("No coding agent is installed where rush looks.")
	}
	return strings.Join(out, faint("   "))
}

// agentHead is the agent's name, how far it's been tried, where it lives,
// what it's doing now, what rush can do with it and what its models take.
func (m *Model) agentHead(k agent.Kind, w int) []string {
	if k == "" {
		return nil
	}
	label := func(s string) string { return dim(fit(s, 10)) }
	ad, _ := agent.Get(k)
	name := glyph(k) + " " + paint(cText+bold, agentName(string(k))) + " " + levelChip(k) + faint(levelWords[agent.LevelOf(k)])
	if string(k) == m.store.Config.DefaultAgent() {
		name += paint(cOrange, "  ★ the default")
	}
	out := []string{"", name}
	var where []string
	if p, ok := m.profileOf(ad); ok {
		where = append(where, tildify(p.Dir))
	}
	if path := agent.Path(k); path != "" {
		where = append(where, "runs "+tildify(path))
	}
	if len(where) > 0 {
		out = append(out, label("home")+faint(strings.Join(where, " · ")))
	}
	var live, total int
	for _, a := range m.snap.Agents {
		if a.Kind == string(k) {
			total++
			if a.Live() {
				live++
			}
		}
	}
	out = append(out, label("sessions")+faint(fmt.Sprintf("%d running · %d in all", live, total))+faint("   ·   Accounts has its sign-ins"))
	out = append(out, "", dim("what rush can do with it"))
	out = append(out, m.featureGrid(k, w)...)
	return append(out, modelTable(k, m.agentModels(k))...)
}

// modelTable is what each of models takes, a row each: images and PDFs
// as its agent's MediaReader says, its context window, and the efforts
// it starts with. What the agent doesn't know shows ?, and a footnote
// says so. Nothing here waits: Ollama's Reads asks in the background.
func modelTable(k agent.Kind, models []agent.Choice) []string {
	if len(models) == 0 {
		return nil
	}
	width := 17
	for _, c := range models {
		width = max(width, len(c.ID)+2)
	}
	unknown := false
	cell := func(s string, w int) string {
		switch s {
		case "✓":
			return paint(cGreen, fit(s, w))
		case "?":
			unknown = true
			return paint(cYellow, fit(s, w))
		case "–":
			return faint(fit(s, w))
		}
		return dim(fit(s, w))
	}
	effort := "–"
	if ch, _ := agent.ChoicesOf(k); agent.Supports(k, agent.FeatureEffort) && len(ch.Efforts) > 0 {
		effort = ch.Efforts[0].ID + "–" + ch.Efforts[len(ch.Efforts)-1].ID
	}
	reader, reads := agent.As[agent.MediaReader](k)
	windower, windows := agent.As[agent.ContextWindower](k)
	out := []string{"", dim(fit("models", 2+width) + "  " + fit("images", 8) + fit("pdf", 5) + fit("context", 9) + "effort")}
	for _, c := range models {
		img, pdf := "?", "?"
		if reads {
			if has, ok := reader.Reads(c.ID); ok {
				img, pdf = "–", "–"
				if has&agent.MediaImage != 0 {
					img = "✓"
				}
				if has&agent.MediaPDF != 0 {
					pdf = "✓"
				}
			}
		}
		window := "?"
		if windows {
			if n := windower.ContextWindow(c.ID); n > 0 {
				window = tokens(n)
			}
		}
		out = append(out, "  "+paint(cText, fit(c.ID, width))+"  "+cell(img, 8)+cell(pdf, 5)+cell(window, 9)+cell(effort, cellw.String(effort)))
	}
	if unknown {
		out = append(out, "  "+paint(cYellow, "? ")+faint("unknown to rush until it's used"))
	}
	return out
}

// agentModels are the models agent k's adapter offers, then any your
// sessions have run that it didn't list.
func (m *Model) agentModels(k agent.Kind) []agent.Choice {
	ch, _ := agent.ChoicesOf(k)
	models := append([]agent.Choice(nil), ch.Models...)
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

// startSection is what agent k's new sessions start with: a model, an
// effort and a permission mode, each the agent's own default until set.
func (m *Model) startSection(k agent.Kind) section {
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
	kind := string(k)
	st := d.StartFor(kind)
	ch, _ := agent.ChoicesOf(k)
	change := func(f func(*state.Start, string)) func(string) {
		return func(v string) {
			s := d.StartFor(kind)
			f(&s, v)
			d.SetStartFor(kind, s)
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
