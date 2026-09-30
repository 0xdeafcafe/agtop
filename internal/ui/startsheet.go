package ui

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// The start sheet picks what the next session starts as, for that one
// session: a profile of yours, or the provider, harness, account, model
// and effort. Settings' defaults are untouched. Opened on a session, the
// same sheet switches it (switchSession).

// startOver is what a session starts as, over what its folder's profile
// and Settings say.
type startOver struct {
	kind, model, effort string
	billing             string // state.BillingKey pays with the provider's API key; "" as it's signed in
	account             string // the account's name; "" is the one in use
	profile             string // the profile of yours it is; "" none
}

// route is a provider id (claude, claude-key, ollama) and the agents here
// that run it.
type route struct {
	id    string
	kinds []string
}

// routes are the valid ways to start here, of kinds (the agents that run
// here): a split provider's subscription in its own harness only, its API
// key in any that speaks its API, and anything paid by key only once the
// key is kept (hasKey).
func routes(kinds []string, hasKey func(p string) bool) []route {
	var out []route
	var seen []string
	for _, k := range kinds {
		p := agent.ProviderOf(agent.Kind(k))
		if slices.Contains(seen, p) {
			continue
		}
		seen = append(seen, p)
		ids := []string{p}
		if agent.Split(p) {
			ids = append(ids, agent.KeyOf(p))
		}
		for _, id := range ids {
			_, key := agent.Billed(id)
			var ks []string
			for _, r := range agent.RunsFor(id) {
				if slices.Contains(kinds, string(r)) && (!key && !agent.KeyOnly(r) || hasKey(p)) {
					ks = append(ks, string(r))
				}
			}
			if len(ks) > 0 {
				out = append(out, route{id, ks})
			}
		}
	}
	return out
}

// billingOf is how agent k is paid for as provider id: by key when id is
// a key, or k runs only on one.
func billingOf(id string, k agent.Kind) string {
	if _, key := agent.Billed(id); key || agent.KeyOnly(k) {
		return state.BillingKey
	}
	return ""
}

// startID is the provider o is paid through: a split provider's key one
// when it goes by the key.
func startID(o startOver) string {
	p := agent.ProviderOf(agent.Kind(o.kind))
	if o.billing == state.BillingKey && agent.Split(p) {
		return agent.KeyOf(p)
	}
	return p
}

// startRoutes are the ways a session can start here now.
func (m *Model) startRoutes() []route {
	var kinds []string
	for _, a := range agent.All() {
		if _, ok := a.(agent.Driver); ok && agent.Installed(a.Kind()) && agent.Runs(a.Kind()) {
			kinds = append(kinds, string(a.Kind()))
		}
	}
	return routes(kinds, m.store.Config.HasAPIKey)
}

// nextStart is what the next session in dir starts as: one picked on the
// sheet, else its folder's agent with what Settings says it starts with,
// and what its profile says over that.
func (m *Model) nextStart(dir string) startOver {
	if m.startOver != nil {
		return *m.startOver
	}
	k := m.startKindIn(dir)
	if p := m.startProfile(dir); len(p.Providers) > 0 && agent.ProviderOf(agent.Kind(k)) == p.Providers[0] {
		o := m.profileSetup(p, k)
		o.account = "" // the one in use: a profile's account is where it starts while that has room
		return o
	}
	return m.startDefaults(k)
}

// startDefaults is agent k as Settings says it starts.
func (m *Model) startDefaults(k string) startOver {
	st := m.store.Config.Dispatch.StartFor(k)
	return startOver{kind: k, model: st.Model, effort: st.Effort, billing: billingOf("", agent.Kind(k))}
}

// profileSetup is profile p as a start on agent k (its first provider
// here): its model, effort, billing and account over what Settings
// starts k with.
func (m *Model) profileSetup(p state.Profile, k string) startOver {
	o := m.startDefaults(k)
	o.model, o.effort = cmp.Or(p.Model, o.model), cmp.Or(p.Effort, o.effort)
	if p.Billing != "" {
		o.billing = p.Billing // with no key kept yet, starting says where to add one
	}
	o.account = m.accountName(agent.Kind(k), p.Account)
	if !p.Builtin {
		o.profile = p.Name
	}
	return o
}

// startSheet picks a startOver, row by row, ←→ through each row's values.
type startSheet struct {
	routes []route
	o      startOver
	row    int
	conn   string // the session it switches; "" for the next one
}

// startRows are the sheet's rows, top to bottom.
var startRows = []string{"Profile", "Provider", "Harness", "Account", "Model", "Effort"}

// openStartSheet opens the sheet on what the next session would start as.
func (m *Model) openStartSheet() tea.Cmd {
	return m.openSetupSheet(nil, m.nextStart(m.startDir()))
}

// openSwitchSheet opens the sheet on what session c runs as, to switch it.
func (m *Model) openSwitchSheet(c *hostConn) tea.Cmd {
	return m.openSetupSheet(c, m.sessionStart(c))
}

func (m *Model) openSetupSheet(c *hostConn, o startOver) tea.Cmd {
	rs := m.startRoutes()
	if len(rs) == 0 {
		m.flash("rush can't run any agent here yet", true)
		return nil
	}
	s := &startSheet{routes: rs, o: o, row: 1}
	if c != nil {
		s.conn = c.key
	}
	if o.profile != "" {
		s.row = 0
	}
	if r := s.at(startID(o)); r == nil || !slices.Contains(r.kinds, o.kind) {
		s.o = m.startDefaults(rs[0].kinds[0])
		s.o.billing = billingOf(rs[0].id, agent.Kind(s.o.kind))
	}
	m.sheet = s
	return m.loadModels(s.o.kind)
}

// at is the route of provider id, or nil.
func (s *startSheet) at(id string) *route {
	for i := range s.routes {
		if s.routes[i].id == id {
			return &s.routes[i]
		}
	}
	return nil
}

// loadModels reads the models agent k offers from its home (Codex's
// cache), off the UI, for the sheet; nil when its adapter lists them
// itself or reads none.
func (m *Model) loadModels(k string) tea.Cmd {
	ad, ok := agent.Get(agent.Kind(k))
	ml, lists := ad.(agent.ModelLister)
	if ch, _ := agent.ChoicesOf(agent.Kind(k)); !ok || !lists || len(ch.Models) > 0 {
		return nil
	}
	return sheetDo(func() ([]agent.Choice, error) {
		if ps := ad.Profiles(); len(ps) > 0 { // off the UI, so it may read
			return ml.ListModels(ps[0]), nil
		}
		return nil, nil
	}, func(m *Model, v []agent.Choice, _ error) tea.Cmd {
		if m.listed == nil {
			m.listed = map[string][]agent.Choice{}
		}
		m.listed[k] = v
		return nil
	})
}

// models are what agent k can start on: its adapter's list, else the one
// read from its home.
func (m *Model) models(k string) []agent.Choice {
	if ch, _ := agent.ChoicesOf(agent.Kind(k)); len(ch.Models) > 0 {
		return ch.Models
	}
	return m.listed[k]
}

// choices are row's values, as the startOver field it sets holds them.
func (s *startSheet) choices(m *Model, row int) []string {
	k := agent.Kind(s.o.kind)
	switch row {
	case 0:
		out := []string{""}
		for _, p := range m.store.Config.Profiles {
			out = append(out, p.Name)
		}
		return out
	case 1:
		var out []string
		for _, r := range s.routes {
			out = append(out, r.id)
		}
		return out
	case 2:
		if r := s.at(startID(s.o)); r != nil {
			return r.kinds
		}
		return []string{s.o.kind}
	case 3:
		if s.o.billing == state.BillingKey {
			return []string{""}
		}
		return append([]string{""}, m.accountNames(k)...)
	}
	list, now := m.models(s.o.kind), s.o.model
	if row == 5 {
		ch, _ := agent.ChoicesOf(k)
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

// now is row's value in o.
func (s *startSheet) now(row int) string {
	return []string{s.o.profile, startID(s.o), s.o.kind, s.o.account, s.o.model, s.o.effort}[row]
}

// word is row's value v in words.
func (s *startSheet) word(m *Model, row int, v string) string {
	k := agent.Kind(s.o.kind)
	switch row {
	case 0:
		return cmp.Or(v, "none: as below")
	case 1:
		return provLabel(v)
	case 2:
		return agent.HarnessLabel(agent.Kind(v))
	case 3:
		switch in := m.accountOf(k); {
		case s.o.billing == state.BillingKey:
			return "its API key"
		case v == "" && in != "":
			return in + ", in use"
		case v == "":
			return "its own sign-in"
		case v == in:
			return v + ", in use"
		}
		return v + ", switched to for every " + agent.HarnessLabel(k) + " session"
	case 4:
		if v == "" {
			return "its default model"
		}
		return modelWord(s.o.kind, v)
	}
	return cmp.Or(v, "its default effort")
}

func (s *startSheet) width(*Model) int { return 76 }

func (s *startSheet) body(m *Model, w, _ int) []string {
	title, about := "Start as", "the next session only; Settings stay as they are"
	if s.conn != "" {
		title, about = "Switch to", "in place in the same harness, else the conversation is handed over"
	}
	out := []string{sheetTitle(title, about, w), ""}
	for i, label := range startRows {
		v := s.word(m, i, s.now(i))
		line := dim(fit(label, 10)) + faint("‹ ") + paint(cText+bold, v) + faint(" ›")
		if len(s.choices(m, i)) < 2 {
			line = dim(fit(label, 10)) + "  " + paint(cText, v)
		}
		out = append(out, sheetRow(line, i == s.row, w))
	}
	out = append(out, "", "  "+m.setupChip(s.o))
	do := "use it"
	switch {
	case s.conn != "":
		do = "switch"
	case len(m.input) > 0:
		do = "start it"
	}
	return append(out, "", keysFit(w, "↑↓", "choose", "←→", "change", "enter", do, "esc", "cancel"))
}

func (s *startSheet) key(m *Model, _ tea.KeyPressMsg, k string) tea.Cmd {
	switch k {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "shift+tab":
		s.row = roundMove(s.row, -1, len(startRows))
	case "down", "tab":
		s.row = roundMove(s.row, 1, len(startRows))
	case "left", "right":
		vals := s.choices(m, s.row)
		d := 1
		if k == "left" {
			d = -1
		}
		i := max(0, slices.Index(vals, s.now(s.row)))
		return s.set(m, vals[roundMove(i, d, len(vals))])
	case "enter":
		m.sheet = nil
		o := s.o
		if s.conn != "" {
			if c := m.sheetConn(s.conn); c != nil {
				return m.switchSession(c, o)
			}
			return nil
		}
		m.startOver = &o
		if len(m.input) > 0 {
			return m.submit()
		}
		m.flash("the next session starts as "+m.startWith(m.startDir(), true), false)
	}
	return nil
}

// set puts v in the row the cursor is on. Anything but a profile makes
// it a setup of its own; another provider or harness starts as Settings
// start it.
func (s *startSheet) set(m *Model, v string) tea.Cmd {
	switch s.row {
	case 0:
		p, ok := m.store.Config.ProfileNamed(v)
		if !ok || v == "" {
			s.o.profile = ""
			return nil
		}
		inst := p.Installed()
		if len(inst) == 0 {
			m.flash("none of "+v+"'s providers runs here", true)
			return nil
		}
		s.o = m.profileSetup(p, inst[0])
		return m.loadModels(s.o.kind)
	case 1:
		r := s.at(v)
		k := r.kinds[0]
		if pk := string(m.provKind(v)); slices.Contains(r.kinds, pk) {
			k = pk // the harness it runs in by default
		}
		s.o = m.startDefaults(k)
		s.o.billing = billingOf(v, agent.Kind(k))
		return m.loadModels(k)
	case 2:
		id := startID(s.o)
		s.o = m.startDefaults(v)
		s.o.billing = billingOf(id, agent.Kind(v))
		return m.loadModels(v)
	case 3:
		s.o.account = v
	case 4:
		s.o.model = v
	case 5:
		s.o.effort = v
	}
	s.o.profile = ""
	return nil
}
