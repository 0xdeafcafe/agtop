package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// alt+m in the Prompt picks what the next session starts as: whose models
// (the provider), in which program (the harness), and the model and
// effort, for that one session, as #profile picks a profile. What
// Settings says each agent starts with is untouched.

// startOver is what the next session from the Prompt starts as, over
// what its folder's profile and Settings say.
type startOver struct {
	kind, model, effort string
	billing             string // "key" pays with the provider's API key; "" as it's signed in
}

// startSheet picks a startOver: a row each for the provider, harness,
// model and effort, ←→ through each one's values.
type startSheet struct {
	kinds []string // the agents rush can run here: each a provider in a harness
	o     startOver
	row   int
	// billings are the ways the agent picked can be paid for, as last
	// worked out: read by choices, which has no Model to ask.
	billings []string
}

// startRows are the sheet's rows, top to bottom.
var startRows = []string{"Provider", "Harness", "Model", "Effort", "Billing"}

// Billing is how a session is paid for: out of the subscription its
// harness is signed in to, or per token with its provider's API key. A
// provider in another's harness has only the key.

// billings are the ways agent k can be paid for here, "" (its sign-in)
// and "key" (the API key, once there is one).
func (m *Model) billings(k agent.Kind) []string {
	switch {
	case agent.KeyOnly(k):
		return []string{"key"}
	case agent.KeyEnv(string(k)) != "" && m.store.Config.HasAPIKey(string(k)):
		return []string{"", "key"}
	}
	return []string{""}
}

// billingWord says billing b.
func billingWord(b string) string {
	if b == "key" {
		return "API key, per token"
	}
	return "subscription"
}

// openStartSheet opens the sheet on what the next session would start as.
func (m *Model) openStartSheet() {
	var kinds []string
	for _, a := range agent.All() {
		// A provider in another's harness shows once its key is here.
		if agent.KeyOnly(a.Kind()) && !m.store.Config.HasAPIKey(agent.ProviderOf(a.Kind())) {
			continue
		}
		if _, ok := a.(agent.Driver); ok && agent.Installed(a.Kind()) && agent.Runs(a.Kind()) {
			kinds = append(kinds, string(a.Kind()))
		}
	}
	if len(kinds) == 0 {
		m.flash("rush can't run any agent here yet", true)
		return
	}
	o := m.nextStart(m.startDir())
	if !slices.Contains(kinds, o.kind) {
		o = m.startDefaults(kinds[0])
	}
	m.sheet = &startSheet{kinds: kinds, o: m.fitBilling(o)}
}

// nextStart is what the next session in dir starts as: one picked with
// alt+m, else its folder's agent with what Settings says it starts with.
func (m *Model) nextStart(dir string) startOver {
	if m.startOver != nil {
		return *m.startOver
	}
	return m.startDefaults(m.startKindIn(dir))
}

// startDefaults is agent k as Settings says it starts.
func (m *Model) startDefaults(k string) startOver {
	st := m.store.Config.Dispatch.StartFor(k)
	return m.fitBilling(startOver{kind: k, model: st.Model, effort: st.Effort})
}

// fitBilling is o paid for a way its agent can be.
func (m *Model) fitBilling(o startOver) startOver {
	if b := m.billings(agent.Kind(o.kind)); !slices.Contains(b, o.billing) {
		o.billing = b[0]
	}
	return o
}

// choices are row's values: the providers here, the harnesses the one
// picked runs in (as the agents that run it), then the model and effort,
// the agent's own default first, as "".
func (s *startSheet) choices(row int) []string {
	switch row {
	case 0:
		var ps []string
		for _, k := range s.kinds {
			if p := agent.ProviderOf(agent.Kind(k)); !slices.Contains(ps, p) {
				ps = append(ps, p)
			}
		}
		return ps
	case 4:
		return s.billings
	case 1:
		p := agent.ProviderOf(agent.Kind(s.o.kind))
		var hs []string
		for _, k := range s.kinds {
			if agent.ProviderOf(agent.Kind(k)) == p {
				hs = append(hs, k)
			}
		}
		return hs
	}
	ch, _ := agent.ChoicesOf(agent.Kind(s.o.kind))
	list, now := ch.Models, s.o.model
	if row == 3 {
		list, now = ch.Efforts, s.o.effort
	}
	out := []string{""}
	for _, c := range list {
		out = append(out, c.ID)
	}
	if !slices.Contains(out, now) {
		out = append(out, now) // one Settings names that the list doesn't
	}
	return out
}

func (s *startSheet) width(*Model) int { return 72 }

func (s *startSheet) body(m *Model, w, _ int) []string {
	out := []string{sheetTitle("Start as", "for the next session only", w), ""}
	model := "its default model"
	if s.o.model != "" {
		model = modelWord(s.o.kind, s.o.model)
	}
	k := agent.Kind(s.o.kind)
	vals := []string{agent.ProviderLabel(agent.ProviderOf(k)), agent.HarnessLabel(k), model, firstNonEmpty(s.o.effort, "its default effort"), billingWord(s.o.billing)}
	s.billings = m.billings(k)
	for i, label := range startRows {
		line := dim(fit(label, 10)) + faint("‹ ") + paint(cText+bold, vals[i]) + faint(" ›")
		if n := len(s.choices(i)); n < 2 {
			line = dim(fit(label, 10)) + "  " + paint(cText, vals[i])
		}
		out = append(out, sheetRow(line, i == s.row, w))
	}
	do := "keep it for the next session"
	if len(m.input) > 0 {
		do = "start it"
	}
	return append(out, "", keysFit(w, "↑↓", "choose", "←→", "change", "enter", do, "esc", "cancel"))
}

func (s *startSheet) key(m *Model, _ tea.KeyPressMsg, k string) tea.Cmd {
	switch k {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "shift+tab":
		s.row = (s.row + len(startRows) - 1) % len(startRows)
	case "down", "tab":
		s.row = (s.row + 1) % len(startRows)
	case "left", "right":
		vals := s.choices(s.row)
		s.billings = m.billings(agent.Kind(s.o.kind))
		now := []string{agent.ProviderOf(agent.Kind(s.o.kind)), s.o.kind, s.o.model, s.o.effort, s.o.billing}[s.row]
		d := 1
		if k == "left" {
			d = -1
		}
		i := max(0, slices.Index(vals, now))
		v := vals[(i+d+len(vals))%len(vals)]
		switch s.row {
		case 0:
			// Another provider, in its own harness when that runs here.
			k, _ := agent.KindFor(v, "")
			if !slices.Contains(s.kinds, string(k)) {
				k = agent.Kind(s.firstOf(v))
			}
			s.o = m.startDefaults(string(k)) // and as Settings start it
		case 1:
			s.o = m.startDefaults(v)
		case 2:
			s.o.model = v
		case 3:
			s.o.effort = v
		case 4:
			s.o.billing = v
		}
	case "enter":
		m.sheet = nil
		o := s.o
		m.startOver = &o
		if len(m.input) > 0 {
			return m.submit()
		}
		m.flash("the next session starts as "+m.startWith(m.startDir(), true), false)
	}
	return nil
}

// firstOf is the first agent here that runs provider p.
func (s *startSheet) firstOf(p string) string {
	for _, k := range s.kinds {
		if agent.ProviderOf(agent.Kind(k)) == p {
			return k
		}
	}
	return s.o.kind
}
