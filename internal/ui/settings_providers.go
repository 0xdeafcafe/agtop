package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Providers is a list and, beside it, everything about the line picked.
// The list is each installed provider in the default profile's order,
// with the account it's on and its tightest limit; then the profiles you
// made; then the folders that pick one. A provider shows its accounts,
// limits and spend, where it runs, what it does at a limit, what its new
// sessions start with and what its agent adds; a profile of yours its
// providers, their order and its policy; a folder the profile it gives.
// enter goes into what's shown and esc back to the list; on a narrow
// terminal the two take turns.

var providersPage = page{
	name: "Providers",
	pre: func(m *Model, s string) (tea.Cmd, bool) {
		d := m.dialog
		if d.inside && (s == "esc" || s == "q") {
			d.inside, d.cursor = false, d.pick
			return nil, true
		}
		return nil, false
	},
	body: (*Model).providersBody,
	key:  (*Model).providersKey,
	rows: func(m *Model) int {
		if m.dialog.inside {
			return len(flat(m.provForm(m.provPicked())))
		}
		return len(m.provItems())
	},
}

// provListW is the list's width, when there's room for what's picked
// beside it.
const provListW = 36

// provItem is a line of Providers' list: one of these is set.
type provItem struct {
	provider string // an installed provider, by name
	profile  string // a profile of yours
	folder   string // a folder rule's path
	add      string // "profile" or "folder": its group's + line
}

// provItems are Providers' lines.
func (m *Model) provItems() []provItem {
	var out []provItem
	for _, ad := range m.agentOrder() {
		if it := (provItem{provider: agent.ProviderOf(ad.Kind())}); !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	cfg := m.store.Config
	for _, p := range cfg.Profiles {
		if !ownProfile(p.Name) {
			out = append(out, provItem{profile: p.Name})
		}
	}
	out = append(out, provItem{add: "profile"})
	for _, r := range cfg.FolderRules {
		out = append(out, provItem{folder: r.Path})
	}
	return append(out, provItem{add: "folder"})
}

// provPicked is the list's line shown: the cursor's, or the one gone into.
func (m *Model) provPicked() provItem {
	d, items := m.dialog, m.provItems()
	i := d.cursor
	if d.inside {
		i = d.pick
	}
	return items[max(0, min(i, len(items)-1))]
}

// openItem goes into it on Providers, when it's there.
func (m *Model) openItem(it provItem) {
	if i := slices.Index(m.provItems(), it); i >= 0 {
		d := m.dialog
		d.pick, d.cursor, d.inside = i, 0, true
	}
}

// provKind is the agent provider pr runs as: in its own harness, or the
// one its profile chose.
func (m *Model) provKind(pr string) agent.Kind {
	p, _ := m.store.Config.ProfileNamed(pr)
	return agent.Kind(p.KindOf(pr))
}

// provUse is how an agent is being used: the account it's on, and what
// its sessions run and cost.
type provUse struct {
	inUse          acctRow // the account in use, or the agent itself
	accts, out     int     // accounts kept, and how many are nearly out
	running, today int     // sessions running, and active today
	spentToday     float64
	spentWeek      float64 // by sessions active in the last 7 days
}

func (m *Model) useOf(k agent.Kind) provUse {
	var u provUse
	for _, r := range m.accountRows() {
		switch {
		case r.kind != k:
		case r.head:
			if u.inUse.kind == "" {
				u.inUse = r
			}
		default:
			u.accts++
			if nearlyOut(r.q) {
				u.out++
			}
			if r.current {
				u.inUse = r
			}
		}
	}
	now := m.snap.At
	for _, a := range m.snap.Agents {
		if a.Kind != string(k) {
			continue
		}
		if a.Live() {
			u.running++
		}
		if a.Spend.Today > 0 || sameDay(a.Spend.Last, now) {
			u.today++
		}
		u.spentToday += a.Spend.Today
		if now.Sub(a.Spend.Last) < 7*24*time.Hour {
			u.spentWeek += a.Spend.Cost
		}
	}
	return u
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// provLeft is ← inside what's picked: a row's own use of it, else, on a
// row with no choices to go back through, back out to the list, as esc.
func (m *Model) provLeft(secs []section, s string) (tea.Cmd, bool) {
	d := m.dialog
	row := rowAt(secs, d.cursor)
	if row.key != nil {
		if cmd, used := row.key(s); used {
			return cmd, true
		}
	}
	if len(row.choices) > 0 {
		return nil, false
	}
	d.inside, d.cursor = false, d.pick
	return nil, true
}

func (m *Model) providersKey(s string) tea.Cmd {
	d := m.dialog
	if d.inside {
		secs := m.provForm(m.provPicked())
		if s == "left" || s == "h" {
			if cmd, used := m.provLeft(secs, s); used {
				return cmd
			}
		}
		return m.formKey(secs, s)
	}
	items, it := m.provItems(), m.provPicked()
	if n := int(s[0] - '0'); len(s) == 1 && n >= 1 && n <= 9 {
		if n <= len(items) && items[n-1].provider != "" {
			d.cursor = n - 1
		}
		return nil
	}
	cfg := &m.store.Config
	switch s {
	case "enter", "right", "l":
		switch it.add {
		case "profile":
			m.newProfile()
		case "folder":
			m.addFolder("")
		default:
			d.pick, d.cursor, d.inside = d.cursor, 0, true
		}
	case "n":
		m.newProfile()
	case "*", "space":
		if name := it.provider + it.profile; name != "" {
			m.makeDefaultProfile(name)
		}
	case "$":
		if it.provider != "" {
			return m.askAPIKey(agent.ProviderOf(m.provKind(it.provider)))
		}
	case "a":
		if it.provider != "" {
			return m.addAccount(m.provKind(it.provider))
		}
		m.addFolder(it.profile)
	case "r":
		if it.profile != "" {
			m.renameProfile(it.profile)
			return nil
		}
		m.flash("reading every provider's limits again…", false)
		return tea.Batch(m.fetchUsage(), m.fetchQuotas())
	case "x", "d":
		switch {
		case it.profile != "":
			m.confirmThen("Delete the profile "+it.profile+"? Its folders go back to the default.", func() tea.Cmd {
				cfg.DeleteProfile(it.profile)
				_ = m.store.SaveConfig()
				return nil
			})
		case it.folder != "":
			cfg.SetRule(it.folder, "")
			_ = m.store.SaveConfig()
		case it.provider != "":
			if p, _ := cfg.ProfileNamed(it.provider); !p.Builtin {
				cfg.DeleteProfile(it.provider)
				_ = m.store.SaveConfig()
				m.flash(agentName(it.provider)+"'s own profile is as it was", false)
			}
		}
	}
	return nil
}

// listKeys are the keys of the list's line it.
func (m *Model) listKeys(it provItem) []string {
	switch {
	case it.provider != "":
		keys := []string{"enter", "open", "1-9", "provider", "*", "make default", "a", "add account", "r", "read limits", "n", "new profile"}
		if agent.KeyEnv(agent.ProviderOf(m.provKind(it.provider))) != "" {
			keys = append(keys, "$", "API key")
		}
		if p, _ := m.store.Config.ProfileNamed(it.provider); !p.Builtin {
			keys = append(keys, "x", "put its profile back")
		}
		return keys
	case it.profile != "":
		return []string{"enter", "open", "*", "make default", "r", "rename", "d", "delete", "n", "new profile"}
	case it.folder != "":
		return []string{"enter", "open", "x", "remove"}
	}
	return []string{"enter", "add"}
}

func (m *Model) providersBody(w int) []string {
	d := m.dialog
	it := m.provPicked()
	wide := w >= 100
	dw := w
	if wide {
		dw = w - provListW - 3
	}
	secs := m.provForm(it)
	cur := -1
	if d.inside {
		cur = d.cursor
	}
	detail := append(m.provHead(it, dw), m.formRows(secs, cur, dw)...)
	keys := keysFit(w, append(m.listKeys(it), pagesKeys...)...)
	if d.inside {
		row := rowAt(secs, d.cursor)
		detail = append(detail, m.about(row, dw)...)
		back := []string{"esc", "back"}
		if len(row.choices) == 0 {
			back[0] = "← esc"
		}
		keys = m.formKeys(row, back, w)
	}
	for i, l := range detail {
		if cellw.String(ansi.Strip(l)) > dw {
			detail[i] = ansi.Truncate(l, dw-1, "…")
		}
	}
	out := []string{m.provTotals(), ""}
	switch {
	case wide:
		list := m.provList(provListW)
		for i := range max(len(list), len(detail)) {
			l, r := "", ""
			if i < len(list) {
				l = list[i]
			}
			if i < len(detail) {
				r = detail[i]
			}
			out = append(out, fit(l, provListW)+faint(" │ ")+r)
		}
	case d.inside:
		out = append(out, detail...)
	default:
		out = append(out, m.provList(w)...)
	}
	return append(out, "", keys)
}

// provTotals is every provider's spend and sessions summed, and which
// provider new sessions run for now, when it isn't the first.
func (m *Model) provTotals() string {
	var u provUse
	for _, ad := range m.agentOrder() {
		v := m.useOf(ad.Kind())
		u.running += v.running
		u.today += v.today
		u.spentToday += v.spentToday
		u.spentWeek += v.spentWeek
	}
	s := paint(cText, money(u.spentToday)) + dim(" today") + faint(" · ") +
		paint(cText, money(u.spentWeek)) + dim(" this week") + faint(" · ") +
		paint(cText, fmt.Sprint(u.running)) + dim(" running") + faint(" · ") +
		paint(cText, fmt.Sprint(u.today)) + dim(" sessions today")
	if sp := m.accts.spill; sp != "" {
		s += faint("   ·   ") + paint(cYellow, "→ ") + dim("new sessions run ") + paint(cText, agentName(sp)) + dim(" for now: the first's accounts are nearly out")
	}
	return s
}

// provList is the list, w wide: the cursor's line highlighted, or the
// one gone into marked.
func (m *Model) provList(w int) []string {
	d := m.dialog
	var out []string
	group := ""
	for i, it := range m.provItems() {
		g := ""
		switch {
		case it.profile != "" || it.add == "profile":
			g = "your profiles"
		case it.folder != "" || it.add == "folder":
			g = "folders"
		}
		if g != group {
			out = append(out, "", faint("── "+g+" "+strings.Repeat("─", max(0, w-len(g)-4))))
			group = g
		}
		for j, l := range m.provLine(it, w-2) {
			switch {
			case d.inside && i == d.pick && j == 0:
				out = append(out, paint(cOrange, "▸ ")+l)
			case !d.inside && i == d.cursor:
				out = append(out, highlight(paint(cOrange, "▍")+" "+l, w))
			default:
				out = append(out, "  "+l)
			}
		}
	}
	return out
}

// provLine is a line of the list, w wide: a provider's is two.
func (m *Model) provLine(it provItem, w int) []string {
	switch {
	case it.provider != "":
		k := m.provKind(it.provider)
		u := m.useOf(k)
		q := u.inUse.q
		meter := faint("—")
		switch win, ok := q.Tightest(""); {
		case ok:
			meter = bar(win.Percent) + " " + paint(cText, fmt.Sprintf("%3.0f%%", win.Percent))
		case q.Balance != "":
			meter = paint(cText, q.Balance)
		case q.Problem != "":
			meter = paint(cYellow, "! no reading")
		}
		name := paint(cText+bold, agentName(it.provider))
		if star := m.profileMark(it.provider); strings.TrimSpace(star) != "" {
			name += " " + strings.TrimSpace(star)
		}
		first := glyph(k) + " " + fit(name, w-18) + right(meter, 15)
		on := u.inUse.name()
		if u.inUse.head {
			on = firstNonEmpty(u.inUse.q.Email, "its own sign-in")
		}
		second := "  " + dim(on)
		if u.accts > 1 {
			second += faint(fmt.Sprintf(" · %d more", u.accts-1))
		}
		if u.out > 0 {
			second += paint(cYellow, fmt.Sprintf(" · %d nearly out", u.out))
		}
		return []string{fit(first, w), fit(second, w)}
	case it.profile != "":
		p, _ := m.store.Config.ProfileNamed(it.profile)
		return []string{fit(m.profileMark(it.profile)+paint(cText+bold, it.profile)+"  "+faint(m.profileAgents(p)), w)}
	case it.folder != "":
		to := ""
		if r, ok := m.store.Config.RuleFor(state.ExpandHome(it.folder)); ok {
			to = r.Profile
		}
		return []string{fit(paint(cText, tildify(state.ExpandHome(it.folder)))+faint(" → ")+dim(to), w)}
	case it.add == "profile":
		return []string{faint("+ new profile")}
	}
	return []string{faint("+ add a folder")}
}

// provHead is what's above the form of the line picked, w wide.
func (m *Model) provHead(it provItem, w int) []string {
	cfg := &m.store.Config
	para := func(s string) []string {
		var out []string
		for _, l := range wrap(s, w) {
			out = append(out, dim(l))
		}
		return out
	}
	switch {
	case it.provider != "":
		return m.providerHead(it.provider, w)
	case it.profile != "":
		if p, ok := cfg.ProfileNamed(it.profile); ok {
			return m.profileHead(p)
		}
	case it.folder != "":
		return para("Sessions started in " + tildify(state.ExpandHome(it.folder)) + ", or a folder inside it, get this profile unless you pick another for them. The longest folder that matches wins.")
	case it.add == "profile":
		return append(para("A profile groups providers for some of your work: Claude then Codex for one client, say, or Ollama in Pi for another, and chooses what happens when they run low. Every provider is a profile of its own already."),
			append([]string{""}, para("A new session gets the profile you pick for it (#profile name), else its folder's, else the default ★. enter names a new one; it starts with the default's providers, for you to change.")...)...)
	case it.add == "folder":
		return para("Gives a folder a profile, so every session started in it gets that one: a client's repositories on its own account's agent, say. enter takes the selected session's folder, or one you type, and gives it the default; open it to give it another.")
	}
	return nil
}

// provForm is the form of the line picked.
func (m *Model) provForm(it provItem) []section {
	cfg := &m.store.Config
	switch {
	case it.provider != "":
		return m.providerForm(it.provider)
	case it.profile != "":
		if p, ok := cfg.ProfileNamed(it.profile); ok {
			return m.profileForm(p)
		}
	case it.folder != "":
		if r, ok := cfg.RuleFor(state.ExpandHome(it.folder)); ok {
			return []section{{title: "Folder", rows: []setting{m.folderRow(r.Path, r.Profile)}}}
		}
	}
	return nil
}

// providerHead is provider pr's name, how far it's been tried, where it
// lives, its limits and spend, and what rush can do with it.
func (m *Model) providerHead(pr string, w int) []string {
	k := m.provKind(pr)
	u := m.useOf(k)
	label := func(s string) string { return dim(fit(s, 10)) }
	name := glyph(k) + " " + paint(cText+bold, agentName(pr)) + dim(runsInWords(k)) + "  " + levelChip(k) + faint(levelWords[agent.LevelOf(k)])
	if strings.EqualFold(pr, m.store.Config.Default().Name) {
		name += paint(cOrange, "  ★ the default")
	}
	out := []string{name}
	var where []string
	if ad, ok := agent.Get(k); ok {
		if p, ok := m.profileOf(ad); ok {
			where = append(where, tildify(p.Dir))
		}
	}
	if path := agent.Path(k); path != "" {
		where = append(where, "runs "+tildify(path))
	}
	if len(where) > 0 {
		out = append(out, label("home")+faint(strings.Join(where, " · ")))
	}
	if hint := agent.Hint(k); hint != "" && !agent.Runs(k) {
		out = append(out, label("can't run")+faint(hint))
	}
	switch why := m.accts.why[string(k)]; {
	case switches(k):
	case why != "":
		out = append(out, label("account")+paint(cRed, "✗ not signed in")+dim(" · "+why))
	default:
		out = append(out, label("account")+dim(firstNonEmpty(u.inUse.q.Email, "its own sign-in"))+faint(" · it signs in through its own program"))
	}
	q := u.inUse.q
	if len(q.Windows) == 0 && q.Balance == "" && q.Problem == "" {
		out = append(out, label("limits")+m.limits(u.inUse, w-10, 0))
	}
	out = append(out, m.windowLines(q, m.snap.At, label)...)
	spend := func(v float64) string {
		if !agent.Supports(k, agent.FeaturePricing) && v == 0 {
			return faint("—")
		}
		return paint(cText, money(v))
	}
	out = append(out, label("spend")+spend(u.spentToday)+dim(" today · ")+spend(u.spentWeek)+dim(" this week")+
		faint(fmt.Sprintf(" · %d running · %d sessions today", u.running, u.today)))
	return append(out, label("can do")+canDo(k, w-10))
}

// canDo is what rush can do with agent k, in a line w wide: the features
// it has, and how many of all there are.
func canDo(k agent.Kind, w int) string {
	var yes []string
	all := agent.AllFeatures()
	for _, f := range all {
		if agent.Supports(k, f.Feature) {
			yes = append(yes, strings.ToLower(f.Label))
		}
	}
	tail := fmt.Sprintf("%d of %d · Capabilities compares", len(yes), len(all))
	if len(yes) == 0 {
		return faint(tail)
	}
	tail = "  " + tail
	return ansi.Truncate(paint(cGreen, "✓ ")+dim(strings.Join(yes, " · ")), max(8, w-len(tail)), "…") + faint(tail)
}

// providerForm is provider pr's accounts, where it runs and what it does
// at a limit, what its new sessions start with, its own profile's
// default and folders, then what its agent adds.
func (m *Model) providerForm(pr string) []section {
	k := m.provKind(pr)
	p, _ := m.store.Config.ProfileNamed(pr)
	var secs []section
	if acct, ok := m.accountSection(k); ok {
		secs = append(secs, acct)
	}
	own := m.profileForm(p) // its harness and policy, then its default and folders
	n := len(own) - 2
	ag := m.agentSections(k)
	secs = append(append(secs, own[:n]...), ag[0])
	return append(append(secs, own[n:]...), ag[1:]...)
}

// accountSection is agent k's accounts, the one in use marked; none when
// rush can't switch it.
func (m *Model) accountSection(k agent.Kind) (section, bool) {
	if !switches(k) {
		return section{}, false
	}
	sec := section{title: "Account", note: "one at a time, shared by all its sessions"}
	rows := accountsOf(m.accountRows(), k)
	if why := m.accts.why[string(k)]; why != "" {
		// Signed in as no one: its sessions fail until it is again.
		sec.rows = append(sec.rows, setting{
			label: "not signed in",
			line: func(w int) string {
				return paint(cRed, "✗ "+agentName(string(k))+" is signed out") + dim(" · l signs it in")
			},
			key: func(s string) (tea.Cmd, bool) {
				if s == "l" || s == "enter" {
					return m.addAccount(k), true
				}
				return nil, false
			},
			keys: []string{"l", "sign in again"},
			about: func() (string, string, string) {
				return "Not signed in", agentName(string(k)) + " isn't signed in, so its sessions fail to start or stop at their first request.", why
			},
		})
	}
	for i := range rows {
		sec.rows = append(sec.rows, m.accountRow(rows[i]))
	}
	if all, n := together(rows); n > 1 {
		sec.rows = append(sec.rows, m.togetherRow(all, n))
	}
	sec.rows = append(sec.rows, setting{
		label: "+ add an account",
		line:  func(int) string { return faint("+ add an account") },
		key: func(s string) (tea.Cmd, bool) {
			if s == "enter" || s == "right" || s == "a" {
				return m.addAccount(k), true
			}
			return nil, false
		},
		keys: []string{"enter", "sign in"},
		about: func() (string, string, string) {
			return "Add an account", "Signs " + agentName(string(k)) + " in to another account, for rush to keep and switch to when one runs low.", ""
		},
	})
	return sec, true
}

// accountRow is one of an agent's accounts: enter switches to it.
func (m *Model) accountRow(r acctRow) setting {
	keys := []string{"enter", "switch to", "a", "add", "r", "rename", "l", "sign in again", "d", "forget"}
	if r.login != nil && expired(r.q) {
		keys[5] = "refresh the sign-in"
	}
	if rs := r.q.Resets; rs != nil && rs.Available > 0 {
		keys = append(keys, "u", "use a reset")
	}
	return setting{
		label: r.name(),
		line: func(w int) string {
			mark := faint("○ ")
			if r.current {
				mark = paint(cOrange, "● ")
			}
			use := m.limits(r, 26, 26) + resetsChip(r.q)
			if m.accts.why[string(r.kind)] != "" && !r.current {
				// Its usage reads stale only because the agent is signed out.
				use = paint(cYellow, "! ") + dim("signed out · l signs back in to it")
			}
			return mark + paint(cText, fit(r.name(), 16)) + dim(fit(r.email(), max(0, min(28, w-72)))) + use
		},
		key: func(s string) (tea.Cmd, bool) {
			switch {
			case s == "u":
				return m.useReset(&r), true
			case s == "a":
				return m.addAccount(r.kind), true
			case r.login != nil && slices.Contains([]string{"enter", "r", "l", "d", "x"}, s):
				return m.loginKey(*r.login, s), true
			case r.login == nil && slices.Contains([]string{"enter", "r", "l", "d", "x"}, s):
				return m.signInKey(r, s), true
			}
			return nil, false
		},
		keys: keys,
		about: func() (string, string, string) {
			now := "Kept by rush: enter switches to it."
			if r.current {
				now = "In use: new " + agentName(string(r.kind)) + " sessions run on it."
			}
			if resets := resetsText(r.q, m.snap.At); resets != "" {
				now += "\n" + resets
			}
			now += resetCredits(r.q, m.snap.At)
			return r.name(), acctWho(r), now
		},
	}
}

// resetsText is when each of a reading's windows resets, by the clock
// and how long from now: "5h resets 17:00, in 3h · 7d resets Thu 09:00,
// in 2d".
func resetsText(q usage.Quota, now time.Time) string {
	var out []string
	for i := range q.Windows {
		win := &q.Windows[i]
		if !win.ResetsAt.After(now) {
			continue
		}
		when := win.ResetsAt.Local().Format("15:04")
		if win.ResetsAt.Sub(now) > 20*time.Hour {
			when = win.ResetsAt.Local().Format("Mon 15:04")
		}
		out = append(out, win.Label+" resets "+when+", in "+roughly(win.ResetsAt.Sub(now)))
	}
	return strings.Join(out, " · ")
}

// together is an agent's accounts as one: each window's use averaged
// over the accounts with a reading of it, resetting when the first of
// them does; and how many accounts had a reading.
func together(rows []acctRow) (usage.Quota, int) {
	var all usage.Quota
	count := map[string]int{}
	n := 0
	for k := range rows {
		r := &rows[k]
		if len(r.q.Windows) == 0 {
			continue
		}
		n++
		if r.q.FetchedAt.After(all.FetchedAt) {
			all.FetchedAt = r.q.FetchedAt
		}
		for j := range r.q.Windows {
			win := &r.q.Windows[j]
			i := slices.IndexFunc(all.Windows, func(w usage.Window) bool { return w.Label == win.Label })
			if i < 0 {
				all.Windows = append(all.Windows, usage.Window{Label: win.Label, Name: win.Name, ResetsAt: win.ResetsAt})
				i = len(all.Windows) - 1
			}
			a := &all.Windows[i]
			a.Percent += win.Percent // summed here, averaged below
			if a.ResetsAt.IsZero() || !win.ResetsAt.IsZero() && win.ResetsAt.Before(a.ResetsAt) {
				a.ResetsAt = win.ResetsAt
			}
			count[win.Label]++
		}
	}
	for i := range all.Windows {
		all.Windows[i].Percent /= float64(count[all.Windows[i].Label])
	}
	for i := range rows {
		if rs := rows[i].q.Resets; rs != nil {
			if all.Resets == nil {
				all.Resets = &usage.Resets{}
			}
			all.Resets.Available += rs.Available
		}
	}
	return all, n
}

// togetherRow is an agent's accounts added up: how much room they have
// between them before rush has nowhere left to switch to.
func (m *Model) togetherRow(all usage.Quota, n int) setting {
	return setting{
		label: "together",
		line: func(w int) string {
			return faint("Σ ") + paint(cSub, fit("together", 16)) + dim(fit(fmt.Sprintf("%d accounts", n), max(0, min(28, w-72)))) + m.limits(acctRow{q: all}, 26, 26) + resetsChip(all)
		},
		key: func(string) (tea.Cmd, bool) { return nil, false },
		about: func() (string, string, string) {
			left := make([]string, 0, len(all.Windows))
			for i := range all.Windows {
				left = append(left, fmt.Sprintf("%s: %.1f accounts' worth left", all.Windows[i].Label, float64(n)*(100-all.Windows[i].Percent)/100))
			}
			now := strings.Join(left, " · ")
			if resets := resetsText(all, m.snap.At); resets != "" {
				now += "\nSoonest: " + resets
			}
			if rs := all.Resets; rs != nil && rs.Available > 0 {
				now += fmt.Sprintf("\n↺ %d limit reset%s earned between them: open an account to use one", rs.Available, plural(rs.Available))
			}
			return "All accounts together", fmt.Sprintf("Each limit's use averaged over the %d accounts with a reading, so 50%% means half their combined room is left. It resets when the first of them does.", n), now
		},
	}
}

// acctWho is who an account is: its email, organisation, role and plan,
// as far as they're known.
func acctWho(r acctRow) string {
	var who []string
	add := func(vs ...string) {
		for _, v := range vs {
			if v = strings.ReplaceAll(v, "_", " "); v != "" && !slices.Contains(who, v) {
				who = append(who, v)
			}
		}
	}
	if r.login != nil {
		u := r.login.Usage
		add(r.login.Email, u.Org, u.Role, u.Plan, u.Billing)
		if u.Extra {
			add("extra usage on")
		}
	} else {
		add(r.email(), firstNonEmpty(r.q.Plan, r.acct.Plan))
	}
	if len(who) == 0 {
		return "Who it is isn't known yet."
	}
	return strings.Join(who, " · ")
}

// askAPIKey asks for provider p's API key, which pays for its models per
// token in any harness that speaks its API, and picks "API key" over the
// subscription where a session can be either. "none" forgets it.
func (m *Model) askAPIKey(p string) tea.Cmd {
	name := agent.ProviderLabel(p)
	if agent.KeyEnv(p) == "" {
		m.flash(name+" takes no API key", true)
		return nil
	}
	what := name + " API key"
	if m.store.Config.HasAPIKey(p) {
		what += " (none forgets it)"
	}
	m.askSecret(what, func(v string) tea.Cmd {
		if v == "none" {
			v = ""
		}
		return later(func() error { return state.PutAPIKey(p, v) }, func(m *Model, err error) tea.Cmd {
			if err != nil {
				m.flash(err.Error(), true)
				return nil
			}
			m.store.Config.MarkAPIKey(p, v != "")
			_ = m.store.SaveConfig()
			if v == "" {
				m.flash(name+"'s API key is forgotten", false)
			} else {
				m.flash(name+"'s API key is kept in the keychain · alt+m picks it per session", false)
			}
			return nil
		})
	})
	return nil
}

// resetsChip is how many limit resets an account has earned, when any.
func resetsChip(q usage.Quota) string {
	if q.Resets == nil || q.Resets.Available == 0 {
		return ""
	}
	return "  " + paint(cGreen, fmt.Sprintf("↺ %d", q.Resets.Available))
}

// resetCredits says an account's earned resets, each with what it does
// and when it runs out, for its about: "" when it has none.
func resetCredits(q usage.Quota, now time.Time) string {
	rs := q.Resets
	if rs == nil || rs.Available == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n↺ %d limit reset%s earned: each resets its limits at once · u uses one", rs.Available, plural(rs.Available))
	for _, c := range rs.Credits {
		b.WriteString("\n  • " + firstNonEmpty(c.Title, "a limit reset"))
		if c.About != "" {
			b.WriteString(": " + c.About)
		}
		if !c.Expires.IsZero() {
			b.WriteString(" · runs out " + c.Expires.Local().Format("Mon 2 Jan") + ", in " + roughly(c.Expires.Sub(now)))
		}
	}
	return b.String()
}

// useReset spends one of an account's earned resets, once you say so. A
// reset goes to the account its agent is signed in to, so another one is
// switched to first.
func (m *Model) useReset(r *acctRow) tea.Cmd {
	rs := r.q.Resets
	ad, _ := agent.Get(r.kind)
	sp, ok := ad.(agent.ResetSpender)
	switch {
	case rs == nil || rs.Available == 0:
		m.flash(r.name()+" has no limit resets", false)
		return nil
	case !ok:
		m.flash(agentName(string(r.kind))+" can't spend resets through rush", true)
		return nil
	case !r.current:
		m.flash("a reset goes to the account in use: enter switches to "+r.name()+", then u", false)
		return nil
	}
	p, found := m.profileOf(ad)
	if !found {
		return nil
	}
	left := rs.Available - 1
	m.confirmThen(fmt.Sprintf("Use one of %s's %d limit resets now? Its limits go back to empty; %d left after.", r.name(), rs.Available, left), func() tea.Cmd {
		m.flash("using a reset on "+r.name()+"…", false)
		src, _ := ad.(agent.QuotaSource)
		return later(func() resetDone {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var d resetDone
			if d.words, d.err = sp.UseReset(ctx, p, ""); src == nil {
				return d
			}
			// Read again at once, so the limits shown are the reset ones.
			if q, err := src.Quota(ctx, p, agent.Account{Kind: p.Kind}); err == nil {
				d.q = quotaMsg{p.Dir: q}
				_ = usage.Record(host.QuotasPath(), host.QuotaKey(p), q)
				if q.Account != "" {
					d.q[q.Account] = q
					_ = usage.Record(host.QuotasPath(), q.Account, q)
				}
			}
			return d
		}, func(m *Model, d resetDone) tea.Cmd {
			if d.err != nil {
				m.flash("couldn't use a reset on "+r.name()+": "+d.err.Error(), true)
			} else {
				m.flash(r.name()+": "+d.words, false)
			}
			return d.q.applyTo(m)
		})
	})
	return nil
}

// resetDone is what came of spending a reset, and the limits read after.
type resetDone struct {
	words string
	err   error
	q     quotaMsg
}
