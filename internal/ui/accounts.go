package ui

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// Providers lists every installed agent (a provider: Claude Code, Codex,
// Copilot…), in the default profile's order, each with the accounts it
// can be signed in as, how far agtop's support for it has been tried, and
// what agtop can do with it. Every agent runs from its own home
// (~/.claude, ~/.codex…); an account is a sign-in agtop puts in that home
// when you switch to it.

// accountsState is what Accounts knows beyond the snapshot.
type accountsState struct {
	// now is the account each agent other than Claude Code is signed in
	// as, by kind: its id.
	now map[string]string
	// why is why who an agent is signed in as isn't known, by kind.
	why map[string]string
	// spill is the agent new sessions run instead of the default
	// profile's first, while every account of that is nearly out.
	spill string
	// switchedAt is when agtop last switched an agent's account itself.
	switchedAt map[string]time.Time
	// profile is the profile picked for the next session started from the
	// Prompt (#profile): it goes once used.
	profile string
	// handedOff are the sessions a usage limit stopped that agtop handed
	// to another provider, or tried to, by key: each is handed on once.
	handedOff map[string]bool
}

// loginsKind is the provider whose accounts are Config.Logins, switched in
// its one home by the vault rather than through its adapter.
var loginsKind = agent.Kind(state.KindOf(""))

// ready makes the maps, for a Model made without New.
func (s *accountsState) ready() {
	if s.now == nil {
		s.now, s.why, s.switchedAt = map[string]string{}, map[string]string{}, map[string]time.Time{}
	}
	if s.handedOff == nil {
		s.handedOff = map[string]bool{}
	}
}

// acctMsg is a message Accounts handles.
type acctMsg interface{ applyTo(m *Model) tea.Cmd }

// acctRow is one line of Accounts: an agent, or one of its accounts.
type acctRow struct {
	kind    agent.Kind
	head    bool             // the agent's own line
	login   *fleet.LoginView // one of Claude Code's accounts
	acct    agent.Account    // another agent's
	current bool
	q       usage.Quota
}

func (r acctRow) name() string {
	switch {
	case r.head:
		return agentName(string(r.kind))
	case r.login != nil:
		return r.login.Name
	}
	return r.acct.Name
}

func (r acctRow) email() string {
	if r.login != nil {
		return r.login.Email
	}
	return firstNonEmpty(r.acct.Email, r.q.Email)
}

// agentOrder is every installed agent, in the default profile's order,
// which is the order new sessions move on through when an agent's
// accounts are all nearly out. Those it doesn't list follow: Claude Code,
// then by name.
func (m *Model) agentOrder() []agent.Adapter {
	inst := agent.InstalledAll()
	by := map[string]agent.Adapter{}
	for _, a := range inst {
		by[string(a.Kind())] = a
	}
	var out []agent.Adapter
	for _, k := range m.store.Config.Default().Providers {
		if a, ok := by[k]; ok {
			out = append(out, a)
			delete(by, k)
		}
	}
	var rest []agent.Adapter
	for _, a := range inst {
		if _, ok := by[string(a.Kind())]; ok {
			rest = append(rest, a)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Kind() == loginsKind && rest[j].Kind() != loginsKind })
	return append(out, rest...)
}

// profileOf is the home an agent runs from.
func (m *Model) profileOf(a agent.Adapter) (agent.Profile, bool) {
	if a.Kind() == loginsKind {
		acct := m.store.Config.ActiveAccount()
		return agent.Profile{Kind: loginsKind, Name: acct.Name, Dir: acct.ConfigDir}, true
	}
	ps := a.Profiles()
	if len(ps) == 0 {
		return agent.Profile{}, false
	}
	return ps[0], true
}

// acctKey is where an account's limits are kept.
func acctKey(kind, id string) string {
	if kind == "copilot" {
		return "copilot:gh:" + id
	}
	return kind + ":" + id
}

// signInAccount is a kept account as the agent's.
func signInAccount(s state.SignIn) agent.Account {
	return agent.Account{Kind: agent.Kind(s.Kind), ID: s.ID, Key: acctKey(s.Kind, s.ID), Name: s.Name, Email: s.Email, Plan: s.Plan}
}

// accountRows are Accounts' lines: each installed agent, then its
// accounts.
func (m *Model) accountRows() []acctRow {
	var out []acctRow
	for _, ad := range m.agentOrder() {
		k := ad.Kind()
		head := acctRow{kind: k, head: true}
		switch _, switches := ad.(agent.Accounts); {
		case k == loginsKind:
			if len(m.snap.Accounts) > 0 {
				head.q = m.snap.Accounts[0].Quota
			}
			out = append(out, head)
			for i := range m.snap.Logins {
				lv := &m.snap.Logins[i]
				out = append(out, acctRow{kind: k, login: lv, current: lv.Current, q: lv.Quota})
			}
		case switches:
			p, _ := m.profileOf(ad)
			out = append(out, head)
			for _, s := range m.store.Config.SignInsOf(string(k)) {
				a := signInAccount(s)
				r := acctRow{kind: k, acct: a, current: m.accts.now[string(k)] == s.ID, q: m.quotas[a.Key]}
				if pq, ok := m.quotas[p.Dir]; r.current && ok && (pq.Account == "" || pq.Account == a.Key) && !pq.FetchedAt.Before(r.q.FetchedAt) {
					r.q = pq
				}
				out = append(out, r)
			}
		default:
			// An agent agtop can't switch: its one sign-in is its own.
			if p, ok := m.profileOf(ad); ok {
				head.q = m.quotas[p.Dir]
			}
			out = append(out, head)
		}
	}
	return out
}

// switches is whether agtop can switch agent k between accounts.
func switches(k agent.Kind) bool {
	if k == loginsKind {
		return true
	}
	ad, _ := agent.Get(k)
	_, ok := ad.(agent.Accounts)
	return ok
}

// accountsOf are the account rows of agent k.
func accountsOf(rows []acctRow, k agent.Kind) []acctRow {
	var out []acctRow
	for _, r := range rows {
		if r.kind == k && !r.head {
			out = append(out, r)
		}
	}
	return out
}

// startKind is the agent new sessions from the Prompt run: their
// profile's first, unless every account of it is nearly out and the
// profile moves on to the next.
func (m *Model) startKind() string { return m.startKindIn(m.startDir()) }

// startKindIn is the agent a new session in dir runs.
func (m *Model) startKindIn(dir string) string {
	if p, ok := m.startPick(dir); ok {
		return p.Kind
	}
	return m.store.Config.DefaultAgent()
}

// startAccount is the agent and account new sessions start on, for the
// top bar.
func (m *Model) startAccount() string {
	k := m.startKind()
	if agent.Kind(k) == loginsKind {
		return m.inUse()
	}
	name := agentName(k)
	for _, s := range m.store.Config.SignInsOf(k) {
		if s.ID == m.accts.now[k] {
			return name + " · " + s.Name
		}
	}
	return name
}

// startQuota is the limits of the account new sessions start on, when
// they run an agent other than Claude Code.
func (m *Model) startQuota() (usage.Quota, bool) {
	k := m.startKind()
	if agent.Kind(k) == loginsKind {
		return usage.Quota{}, false
	}
	for _, r := range m.accountRows() {
		if string(r.kind) == k && (r.current || r.head && !switches(r.kind)) {
			return r.q, true
		}
	}
	return usage.Quota{}, true
}

// findSignIns asks each agent agtop can switch who it's signed in as, and
// which accounts it knows of itself.
func (m *Model) findSignIns() tea.Cmd {
	if m.offline {
		return nil
	}
	var cmds []tea.Cmd
	for _, ad := range agent.InstalledAll() {
		acc, ok := ad.(agent.Accounts)
		if !ok || ad.Kind() == loginsKind {
			continue
		}
		p, ok := m.profileOf(ad)
		if !ok {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			msg := signInsMsg{kind: string(ad.Kind())}
			msg.cur, msg.err = acc.Current(p)
			if kn, ok := ad.(agent.Known); ok {
				msg.known = kn.Known()
			}
			return msg
		})
	}
	return tea.Batch(cmds...)
}

// signInsMsg is who an agent is signed in as, and the accounts it knows.
type signInsMsg struct {
	kind  string
	cur   agent.Account
	err   error
	known []agent.Account
}

func (msg signInsMsg) applyTo(m *Model) tea.Cmd {
	m.accts.ready()
	cfg := &m.store.Config
	before := append([]state.SignIn(nil), cfg.SignIns...)
	note := func(a agent.Account) {
		if a.ID != "" {
			cfg.NoteSignIn(state.SignIn{Kind: msg.kind, ID: a.ID, Email: a.Email, Plan: a.Plan}, a.Name)
		}
	}
	for _, a := range msg.known {
		note(a)
	}
	if msg.err != nil {
		m.accts.why[msg.kind] = msg.err.Error()
		delete(m.accts.now, msg.kind)
	} else {
		note(msg.cur)
		m.accts.now[msg.kind] = msg.cur.ID
		delete(m.accts.why, msg.kind)
	}
	if !reflect.DeepEqual(before, cfg.SignIns) {
		_ = m.store.SaveConfig()
	}
	return nil
}

// setDefaultAgent makes k the agent new sessions run.
func (m *Model) setDefaultAgent(k agent.Kind) {
	if string(k) == m.store.Config.DefaultAgent() {
		m.flash("new sessions already run "+agentName(string(k)), false)
		return
	}
	m.withAgent(string(k))
	m.accts.spill = ""
}

// switchAccount signs another agent's home in as a.
func (m *Model) switchAccount(a agent.Account, why string) tea.Cmd {
	ad, ok := agent.Get(a.Kind)
	if !ok {
		return nil
	}
	acc, ok := ad.(agent.Accounts)
	p, found := m.profileOf(ad)
	if !ok || !found {
		m.flash("agtop can't switch "+ad.Name()+"'s account", true)
		return nil
	}
	return func() tea.Msg {
		return acctSwitchedMsg{to: a, why: why, err: acc.Switch(p, a)}
	}
}

// acctSwitchedMsg is another agent's home signed in as to, or why not.
type acctSwitchedMsg struct {
	to  agent.Account
	why string
	err error
}

func (msg acctSwitchedMsg) applyTo(m *Model) tea.Cmd {
	m.accts.ready()
	k := string(msg.to.Kind)
	if msg.err != nil {
		m.flash("couldn't switch to "+msg.to.Name+": "+msg.err.Error(), true)
		return nil
	}
	m.accts.now[k] = msg.to.ID
	m.accts.switchedAt[k] = time.Now()
	if ad, _ := agent.Get(msg.to.Kind); ad != nil {
		if _, own := ad.(agent.Known); own {
			// Which of its accounts the agent runs on is agtop's to keep.
			cfg := &m.store.Config
			if cfg.Using == nil {
				cfg.Using = map[string]string{}
			}
			cfg.Using[k] = msg.to.ID
			_ = m.store.SaveConfig()
		}
	}
	text := "new " + agentName(k) + " sessions run on " + msg.to.Name
	if msg.why != "" {
		text = msg.why + " · " + text
	}
	m.flash(text, false)
	return m.fetchQuotas()
}

// addAccount signs in to another account of agent k.
func (m *Model) addAccount(k agent.Kind) tea.Cmd {
	if k == loginsKind {
		m.ask("login name", "")
		return nil
	}
	ad, _ := agent.Get(k)
	acc, ok := ad.(agent.Accounts)
	p, found := m.profileOf(ad)
	if !ok || !found {
		m.flash(agentName(string(k))+" signs in through its own program; agtop can't keep more than one account of it", true)
		return nil
	}
	cmd, done, err := acc.SignIn(p)
	if err != nil {
		m.flash("couldn't sign in: "+err.Error(), true)
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return acctAddedMsg{kind: k, err: err}
		}
		a, err := done()
		return acctAddedMsg{kind: k, a: a, err: err}
	})
}

// acctAddedMsg is an account just signed in to from Accounts.
type acctAddedMsg struct {
	kind agent.Kind
	a    agent.Account
	err  error
}

func (msg acctAddedMsg) applyTo(m *Model) tea.Cmd {
	if msg.err != nil {
		m.flash("didn't add a "+agentName(string(msg.kind))+" account: "+msg.err.Error(), true)
		return nil
	}
	cfg := &m.store.Config
	known := false
	for _, s := range cfg.SignInsOf(string(msg.kind)) {
		known = known || s.ID == msg.a.ID
	}
	s := cfg.NoteSignIn(state.SignIn{Kind: string(msg.kind), ID: msg.a.ID, Email: msg.a.Email, Plan: msg.a.Plan}, msg.a.Name)
	_ = m.store.SaveConfig()
	if known {
		m.flash("signed in to "+s.Name+" again", false)
	} else {
		m.flash("added "+s.Name+" · enter switches to it", false)
	}
	return m.fetchQuotas()
}

// forgetAccount drops another agent's account, after asking.
func (m *Model) forgetAccount(r acctRow) {
	if r.current {
		m.flash("switch to another account before forgetting "+r.name(), true)
		return
	}
	d := m.dialog
	d.confirm = fmt.Sprintf("Forget %s (%s)? agtop drops its saved sign-in; sessions already on it keep going.", r.name(), firstNonEmpty(r.email(), agentName(string(r.kind))))
	d.onYes = func() tea.Cmd {
		if ad, ok := agent.Get(r.kind); ok {
			if acc, ok := ad.(agent.Accounts); ok {
				_ = acc.Forget(r.acct)
			}
		}
		m.store.Config.ForgetSignIn(string(r.kind), r.acct.ID)
		_ = m.store.SaveConfig()
		d.cursor = max(0, d.cursor-1)
		return nil
	}
}

// moveAgent moves agent k one place earlier (d<0) or later in the order.
func (m *Model) moveAgent(k agent.Kind, d int) {
	order := m.agentOrder()
	var kinds []string
	at := -1
	for i, a := range order {
		kinds = append(kinds, string(a.Kind()))
		if a.Kind() == k {
			at = i
		}
	}
	to := at + d
	if at < 0 || to < 0 || to >= len(kinds) {
		return
	}
	kinds[at], kinds[to] = kinds[to], kinds[at]
	cfg := &m.store.Config
	p := cfg.Default()
	p.Providers = kinds
	cfg.SetProfile(p.Name, p)
	_ = m.store.SaveConfig()
	// The cursor follows the agent.
	for i, r := range m.accountRows() {
		if r.head && r.kind == k {
			m.dialog.cursor = i
		}
	}
}

// onLimitChoices are what agtop can do when an account is nearly out.
var onLimitChoices = []string{state.OnLimitAccount, state.OnLimitAgent, state.OnLimitOff}

// setSwitchOnLimit sets the default profile's policy from one of
// onLimitChoices: switch account and stay on the provider, move on to the
// next provider too, or wait.
func (m *Model) setSwitchOnLimit(v string) {
	cfg := &m.store.Config
	p := cfg.Default()
	p.Mix, p.OnLimit = state.MixStay, state.LimitAccount
	switch v {
	case state.OnLimitAgent:
		p.Mix = state.MixMix
	case state.OnLimitOff:
		p.OnLimit = state.LimitWait
	}
	cfg.SetProfile(p.Name, p)
}

func onLimitWords(v string) string {
	switch v {
	case state.OnLimitAgent:
		return "switch account, then agent"
	case state.OnLimitOff:
		return "stay put"
	}
	return "switch account"
}

// accountsKey handles a key in Accounts.
func (m *Model) accountsKey(s string) tea.Cmd {
	d := m.dialog
	cfg := &m.store.Config
	rows := m.accountRows()
	if s == "s" {
		i := 0
		for j, c := range onLimitChoices {
			if c == cfg.SwitchOnLimit {
				i = j
			}
		}
		m.setSwitchOnLimit(onLimitChoices[(i+1)%len(onLimitChoices)])
		_ = m.store.SaveConfig()
		switch cfg.SwitchOnLimit {
		case state.OnLimitAgent:
			m.flash(fmt.Sprintf("at %.0f%% agtop switches account; once an agent's are all out, new sessions run the next agent", state.SwitchAt), false)
		case state.OnLimitOff:
			m.flash("agtop stays on the accounts in use, even when they're nearly out", false)
		default:
			m.flash(fmt.Sprintf("at %.0f%% agtop switches to another account of the same agent", state.SwitchAt), false)
		}
		return tea.Batch(m.autoSwitch(), m.checkLimits())
	}
	if n := int(s[0] - '0'); len(s) == 1 && n >= 1 && n <= 9 {
		// Straight to the nth agent.
		for i, r := range rows {
			if r.head {
				if n--; n == 0 {
					d.cursor = i
				}
			}
		}
		return nil
	}
	if d.cursor >= len(rows) {
		return nil
	}
	r := rows[d.cursor]
	switch s {
	case "p", "*":
		m.setDefaultAgent(r.kind)
		return nil
	case "a":
		return m.addAccount(r.kind)
	case "K", "shift+up":
		m.moveAgent(r.kind, -1)
		return nil
	case "J", "shift+down":
		m.moveAgent(r.kind, 1)
		return nil
	}
	switch {
	case r.head:
		if s == "enter" {
			m.setDefaultAgent(r.kind)
		}
	case r.login != nil:
		return m.loginKey(*r.login, s)
	default:
		switch s {
		case "enter":
			if r.current {
				m.flash("already on "+r.name(), false)
				return nil
			}
			return m.switchAccount(r.acct, "")
		case "r":
			m.ask("rename account "+string(r.kind)+" "+r.acct.ID, r.name())
		case "l":
			return m.addAccount(r.kind)
		case "d", "x":
			m.forgetAccount(r)
		}
	}
	return nil
}

// renameSignIn renames another agent's account, from the rename prompt.
func (m *Model) renameSignIn(what, v string) {
	kind, id, _ := strings.Cut(strings.TrimPrefix(what, "rename account "), " ")
	for i, s := range m.store.Config.SignIns {
		if s.Kind == kind && s.ID == id {
			m.store.Config.SignIns[i].Name = v
		}
	}
	_ = m.store.SaveConfig()
}

// accountsBody draws Accounts at width w.
func (m *Model) accountsBody(w int) []string {
	d := m.dialog
	cfg := m.store.Config
	var out []string
	row := func(i int, s string) string {
		if i == d.cursor {
			return highlight(paint(cOrange, "▍")+" "+s, w)
		}
		return "  " + s
	}
	def := cfg.Default()
	line := dim("New sessions: ") + paint(cText, def.Name) + dim(" · ") + m.chain(def) + dim("   ·   at a limit: ") + paint(cText, limitWords(def.Limit())) + faint("   (Profiles changes it)")
	if m.accts.spill != "" {
		line += dim(" · for now ") + glyph(agent.Kind(m.accts.spill)) + " " + paint(cYellow, agentName(m.accts.spill)) + dim(": the first's accounts are all nearly out")
	}
	out = append(out, line, "")
	// Name, email, plan, two limit windows, running: the email gives way
	// first, then the second window.
	cols := []int{28, 28, 12, 19, 19, 8}
	if w < 116 {
		cols[4] = 0
	}
	room := func() int { return w - 4 - cols[0] - cols[2] - cols[3] - cols[4] - cols[5] }
	if room() < 14 {
		cols[2] = 0
	}
	cols[1] = max(8, min(32, room()))
	head := faint(fit("PROVIDER / ACCOUNT", cols[0]+2) + fit("EMAIL", cols[1]) + fit("PLAN", cols[2]) + fit("LIMITS", cols[3]+cols[4]) + right("RUNNING", cols[5]))
	out = append(out, "  "+head)
	rows := m.accountRows()
	if len(rows) == 0 {
		out = append(out, "", dim("  No coding agent is installed where agtop looks: install Claude Code, Codex or another, and it shows here."))
	}
	defer func() {
		if missing := m.notInstalled(); missing != "" {
			out = append(out, "", faint("  Not installed here: ")+missing)
		}
	}()
	n := 0
	for i, r := range rows {
		if r.head {
			n++
			if i > 0 {
				out = append(out, "")
			}
		}
		out = append(out, row(i, m.accountLine(r, n, cols)))
	}
	if d.cursor < len(rows) {
		r := rows[d.cursor]
		title := r.name()
		if !r.head {
			title = agentName(string(r.kind)) + " · " + r.name()
		}
		out = append(out, "", rule(title, "", w))
		out = append(out, m.accountDetail(r, w)...)
		keys := []string{}
		switch {
		case r.head:
			keys = append(keys, "enter", "make default")
			if switches(r.kind) {
				keys = append(keys, "a", "add account")
			}
		case r.login != nil:
			keys = append(keys, "enter", "switch to", "a", "add account", "r", "rename", "l", "sign in again", "d", "forget", "p", "make default")
		default:
			keys = append(keys, "enter", "switch to", "a", "add account", "r", "rename", "d", "forget", "p", "make default")
		}
		keys = append(keys, "J/K", "move agent", "1-9", "agent", "s", "when nearly out")
		out = append(out, "", keysFit(w, keys...))
	}
	for i, l := range out {
		if cellw.String(ansi.Strip(l)) > w {
			out[i] = ansi.Truncate(l, w-1, "…")
		}
	}
	return out
}

// accountLine is one row of Accounts.
func (m *Model) accountLine(r acctRow, n int, cols []int) string {
	cfg := m.store.Config
	if r.head {
		mark := faint(fmt.Sprintf("%d", n))
		// Its glyph, its name, and how far its support has been tried.
		name := glyph(r.kind) + " " + paint(cText+bold, fit(r.name(), cols[0]-11)) + " " + levelChip(r.kind)
		if string(r.kind) == cfg.DefaultAgent() {
			mark = paint(cOrange, "★")
		}
		live := 0
		for _, a := range m.snap.Agents {
			if a.Live() && state.KindOf(a.Kind) == string(r.kind) {
				live++
			}
		}
		running := faint(right("·", cols[5]))
		if live > 0 {
			running = paint(cSub, right(fmt.Sprint(live), cols[5]))
		}
		var note string
		switch k := string(r.kind); {
		case !agent.Runs(r.kind) && agent.Hint(r.kind) != "":
			note = faint(fmt.Sprintf("%d accounts · without its CLI: sessions on GitHub only", len(cfg.SignInsOf(k))))
		case k == m.startKind() && k != cfg.DefaultAgent():
			note = paint(cYellow, "new sessions run it for now")
		case r.kind == loginsKind && len(m.snap.Logins) == 0:
			note = faint("no account kept yet · a adds one")
		case r.kind != loginsKind && switches(r.kind) && len(cfg.SignInsOf(k)) == 0:
			note = faint(firstNonEmpty(m.accts.why[k], "who it's signed in as isn't known yet"))
		case !switches(r.kind):
			// One sign-in, the agent's own: its limits are the agent's.
			note = dim(fit(r.q.Email, cols[1])) + faint(fit(r.q.Plan, cols[2])) + m.limits(r, cols[3], cols[4])
		default:
			c := len(cfg.SignInsOf(k))
			if r.kind == loginsKind {
				c = len(m.snap.Logins)
			}
			note = faint(fmt.Sprintf("%d accounts", c))
			if c == 1 {
				note = faint("1 account · a adds another")
			}
		}
		return mark + " " + name + fit(note, cols[1]+cols[2]+cols[3]+cols[4]) + running
	}
	mark := faint("○")
	if r.current {
		mark = paint(cOrange, "●")
	}
	plan := r.q.Plan
	if r.login != nil {
		plan = firstNonEmpty(r.login.Usage.Plan, plan)
	}
	plan = firstNonEmpty(plan, r.acct.Plan)
	return "  " + mark + " " + paint(cText, fit(r.name(), cols[0]-2)) + dim(fit(r.email(), cols[1])) +
		faint(fit(strings.ReplaceAll(plan, "_", " "), cols[2])) + m.limits(r, cols[3], cols[4]) + fit("", cols[5])
}

// kindName is an agent's name, short enough for a table.
func kindName(k agent.Kind) string {
	if ad, ok := agent.Get(k); ok && len(ad.Name()) <= 9 {
		return ad.Name()
	}
	s := string(k)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// limits is an account's first two windows, each a labelled meter, or
// its balance, or why there are none.
func (m *Model) limits(r acctRow, w1, w2 int) string {
	q := r.q
	if len(q.Windows) == 0 {
		msg := faint("no reading")
		switch {
		case q.Balance != "":
			msg = paint(cText, q.Balance)
			if q.Problem != "" {
				msg = paint(cYellow, q.Balance+" · "+q.Problem)
			}
		case q.Problem != "":
			msg = paint(cYellow, q.Problem)
		case r.kind != loginsKind:
			ad, _ := agent.Get(r.kind)
			if _, ok := ad.(agent.QuotaSource); !ok {
				msg = faint("doesn't report its limits")
			} else if q.FetchedAt.IsZero() && r.current {
				msg = faint("fetching…")
			} else if q.FetchedAt.IsZero() {
				msg = faint("read while it's in use")
			}
		case q.FetchedAt.IsZero():
			msg = faint("fetching…")
		}
		return fit(msg, w1+w2)
	}
	stale := time.Since(q.FetchedAt) > 3*claude.UsageEvery
	var out string
	for i, cw := range []int{w1, w2} {
		if i >= len(q.Windows) {
			out += fit("", cw)
			continue
		}
		win := q.Windows[i]
		pct := fmt.Sprintf("%3.0f%%", win.Percent)
		cell := faint(fit(win.Label, 3)) + bar(win.Percent) + " " + paint(cText, pct)
		if stale {
			cell = faint(fit(win.Label, 3) + strings.Repeat("▱", 10) + " " + pct)
		}
		out += fit(cell, cw)
	}
	return out
}

// accountDetail is everything known about the selected line.
func (m *Model) accountDetail(r acctRow, w int) []string {
	now := m.snap.At
	label := func(k string) string { return dim(fit(k, 10)) }
	var out []string
	if r.head {
		ad, _ := agent.Get(r.kind)
		var where []string
		if p, ok := m.profileOf(ad); ok {
			where = append(where, tildify(p.Dir))
		}
		if path := agent.Path(r.kind); path != "" {
			where = append(where, "runs "+tildify(path))
		}
		out = append(out, label("home")+faint(strings.Join(where, " · ")))
		place := 0
		for i, a := range m.agentOrder() {
			if a.Kind() == r.kind {
				place = i + 1
			}
		}
		order := fmt.Sprintf("%d of %d", place, len(m.agentOrder()))
		if string(r.kind) == m.store.Config.DefaultAgent() {
			order += " · the default: new sessions run it"
		}
		out = append(out, label("order")+faint(order))
		lv := agent.LevelOf(r.kind)
		out = append(out, label("support")+levelChip(r.kind)+faint(levelWords[lv]))
		out = append(out, m.featureGrid(r.kind, w, label)...)
		if hint := agent.Hint(r.kind); hint != "" && !agent.Runs(r.kind) {
			out = append(out, label("can't run")+faint(hint))
		}
		if !switches(r.kind) {
			out = append(out, label("accounts")+faint("it signs in through its own program; agtop uses whichever account that is"))
		}
		var live, total int
		var today float64
		for _, ag := range m.snap.Agents {
			if state.KindOf(ag.Kind) != string(r.kind) {
				continue
			}
			total++
			if ag.Live() {
				live++
			}
			today += ag.Spend.Today
		}
		out = append(out, label("agents")+paint(cText, fmt.Sprintf("%d running · %d in total", live, total))+dim("  ·  today ")+paint(cText, money(today)))
		return append(out, m.windowLines(r.q, now, label)...)
	}
	var who []string
	seen := map[string]bool{}
	add := func(vs ...string) {
		for _, v := range vs {
			if v = strings.ReplaceAll(v, "_", " "); v != "" && !seen[v] {
				seen[v] = true
				who = append(who, v)
			}
		}
	}
	if r.login != nil {
		u := r.login.Usage
		add(r.login.Email, u.Org, u.Role, u.Plan, u.Billing)
		if u.Extra {
			who = append(who, "extra usage on")
		}
	} else {
		add(r.email(), firstNonEmpty(r.q.Plan, r.acct.Plan))
	}
	if len(who) == 0 {
		who = append(who, "who it is isn't known yet")
	}
	out = append(out, label("who")+paint(cText, strings.Join(who, " · ")))
	state := "kept by agtop · enter switches to it"
	if r.current {
		state = "in use: new " + agentName(string(r.kind)) + " sessions run on it"
	}
	out = append(out, label("sign-in")+faint(state))
	return append(out, m.windowLines(r.q, now, label)...)
}

// windowLines are a reading's windows, each with when it resets.
func (m *Model) windowLines(q usage.Quota, now time.Time, label func(string) string) []string {
	var out []string
	for _, win := range q.Windows {
		s := label(win.Label) + bar(win.Percent) + " " + paint(cText, fmt.Sprintf("%.0f%%", win.Percent))
		if !win.ResetsAt.IsZero() {
			when := win.ResetsAt.Local().Format("15:04")
			if win.ResetsAt.Sub(now) > 20*time.Hour {
				when = win.ResetsAt.Local().Format("Mon 15:04")
			}
			if win.ResetsAt.After(now) {
				s += dim("  resets " + when + " · in " + dur(win.ResetsAt.Sub(now)))
			} else {
				s += faint("  reset at " + when + ", since this reading")
			}
		}
		out = append(out, s)
	}
	if q.Balance != "" {
		out = append(out, label("balance")+paint(cText, q.Balance))
	}
	if len(q.Windows) == 0 && q.Problem != "" {
		out = append(out, label("limits")+paint(cYellow, q.Problem))
	}
	if !q.FetchedAt.IsZero() {
		source := "read"
		switch q.Source {
		case usage.Fetched:
			source = "fetched"
		case usage.Live:
			source = "reported by a session"
		}
		out = append(out, label("usage")+faint(source+" at "+q.FetchedAt.Local().Format("15:04")))
	}
	return out
}
