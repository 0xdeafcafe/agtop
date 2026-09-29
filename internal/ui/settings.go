package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/keymap"
	"github.com/0xdeafcafe/rush/internal/settingsfile"
)

// Settings is a place of pages, and [ and ] go between them, as in every
// place with pages. Providers is each installed provider, your profiles
// and the folders that pick them, a list with everything about the one
// picked beside it: its account, limits, spend, where it runs, what it
// does at a limit and what its new sessions start with. Capabilities
// sets the providers side by side, feature by feature and model by
// model. General is the rest.
//
// Most pages are forms: sections of settings, each of which says what it
// does and what its values mean, drawn and driven here. Providers and
// Capabilities draw themselves, Providers with a form beside its list.

// page is one page of Settings: a form, or one that draws itself.
type page struct {
	name string
	keys []string // its own keys, for a form's key line
	// pre sees a key before the page moves or closes: a page with a
	// page inside it (a profile being edited) takes esc back to itself.
	pre func(m *Model, s string) (tea.Cmd, bool)

	form func(m *Model) []section
	head func(m *Model, w int) []string // lines above a form's sections

	body func(m *Model, w int) []string
	key  func(m *Model, s string) tea.Cmd
	rows func(m *Model) int // the lines the cursor goes through
}

// The pages, in order.
const (
	pageProviders = iota
	pageCapabilities
	pageGeneral
	pageKeys
	pagePlugins
)

// settingsPages are Settings' pages.
func (m *Model) settingsPages() []page {
	return []page{
		providersPage,
		{name: "Capabilities", body: (*Model).capabilitiesBody, key: func(*Model, string) tea.Cmd { return nil }, rows: (*Model).capabilitiesLen},
		{name: "General", form: (*Model).generalSections},
		{name: "Keys", body: (*Model).keysBody, key: (*Model).keysKey, rows: (*Model).keysLen},
		pluginsPage(),
	}
}

// openAgentSettings shows agent k's provider on Settings › Providers,
// open.
func (m *Model) openAgentSettings(k agent.Kind) {
	m.setView(placeSettings)
	m.setSettingsPage(pageProviders)
	m.openItem(provItem{provider: agent.ProviderOf(k)})
}

// dialog is Settings while it's open: the page, the cursor, and a value
// being typed or a question being asked.
type dialog struct {
	page   int
	cursor int

	// pick is the line of Providers' list shown beside it; inside is
	// whether the cursor is in what it shows rather than in the list.
	pick     int
	inside   bool
	advanced bool // a provider shows its agent's advanced sections
	keyCtx   int  // which of keymap.Contexts Keys shows

	plugin      string // the plugin Plugins has open; empty lists them
	plugins     []pluginRow
	pluginsRead *pending[[]pluginRow] // plugins, being read off the UI

	// practice is a key sequence pressed on Keys, not the page's own, to
	// try out: it jumps to and lights the chip it's bound to, so a key
	// can be practiced without changing anything. practiceAt is when the
	// last one came, to tell a chord from a fresh key.
	practice   keymap.Seq
	practiceAt time.Time

	input    []rune
	asking   string // what the input line is for; empty when not typing
	onAnswer func(string) tea.Cmd

	agents   []agent.AgentDef   // the signed-in agent's definitions
	settings *settingsfile.File // the agent showing's own settings file
}

func (m *Model) openDialog(p int) {
	m.dialog = &dialog{page: p}
	m.loadDialog()
}

func (m *Model) loadDialog() {
	d := m.dialog
	d.agents = m.agentDefs(loginsKind)
	if d.page == pageProviders || d.page == pageCapabilities {
		agent.Recheck() // an agent installed since shows at once
	}
	if d.page == pagePlugins {
		d.pluginsRead = goPending(readPlugins)
	}
}

// curPage is the page showing.
func (m *Model) curPage() page {
	pages := m.settingsPages()
	return pages[min(m.dialog.page, len(pages)-1)]
}

// dialogLen is how many lines the cursor goes through.
func (m *Model) dialogLen() int {
	p := m.curPage()
	if p.form != nil {
		return len(flat(p.form(m)))
	}
	return p.rows(m)
}

// ask opens the input line for a typed value; answer gets it, trimmed,
// unless it's empty.
func (m *Model) ask(what, prefill string, answer func(string) tea.Cmd) {
	m.dialog.asking, m.dialog.input, m.dialog.onAnswer = what, []rune(prefill), answer
}

// confirmThen asks a yes or no question before doing yes.
func (m *Model) confirmThen(q string, yes func() tea.Cmd) {
	m.confirm = &confirmation{question: q, onYes: yes}
}

func (m *Model) dialogKey(k tea.KeyPressMsg, s string) tea.Cmd {
	d := m.dialog
	if d.asking != "" {
		switch s {
		case "esc":
			d.asking, d.input, d.onAnswer = "", nil, nil
		case "enter":
			v, f := strings.TrimSpace(string(d.input)), d.onAnswer
			d.asking, d.input, d.onAnswer = "", nil, nil
			if v != "" && f != nil {
				return f(v)
			}
		default:
			m.dialogEdit(k, s)
		}
		return nil
	}
	if p := m.curPage(); p.pre != nil {
		if cmd, used := p.pre(m, s); used {
			return cmd
		}
	}
	switch s {
	case "esc", "q", "ctrl+g", "ctrl+a":
		m.setView(placeAgents)
		m.refresh()
		return nil
	case "[", "]":
		m.setSettingsPage(d.page + map[string]int{"[": -1, "]": 1}[s])
		return nil
	case "up", "k":
		d.cursor = roundMove(d.cursor, -1, m.dialogLen())
		return nil
	case "down", "j":
		d.cursor = roundMove(d.cursor, 1, m.dialogLen())
		return nil
	}
	if p := m.curPage(); p.form == nil {
		return p.key(m, s)
	}
	return m.formKey(m.curPage().form(m), s)
}

func (m *Model) dialogEdit(k tea.KeyPressMsg, s string) {
	d := m.dialog
	switch s {
	case "backspace":
		if len(d.input) > 0 {
			d.input = d.input[:len(d.input)-1]
		}
	case "ctrl+u", "super+backspace":
		d.input = nil
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			d.input = append(d.input, []rune(k.Text)...)
		}
	}
}

// dialogBody renders the page at width w.
func (m *Model) dialogBody(w int) []string {
	p := m.curPage()
	out := []string{paint(cText+bold, p.name)}
	if p.form == nil {
		return append(append(out, ""), p.body(m, w)...)
	}
	if p.head != nil {
		out = append(out, p.head(m, w)...)
	}
	return append(out, m.formBody(p.form(m), p.keys, w)...)
}

// pagesKeys are the keys every page ends its key line with.
var pagesKeys = []string{"[ ]", "page", "esc", "back"}

// section is a titled run of settings on a form page.
type section struct {
	title string
	note  string // after the title, quieter
	rows  []setting
	// advanced sections are folded under one line until you open them.
	advanced bool
}

// setting is one line of a form: a value that ←→ go through or you type,
// or, with line and key, a line of its own (an agent definition, an
// environment variable).
type setting struct {
	label   string
	value   string   // as kept; "" is the default
	choices []string // what ←→ go through
	set     func(string)
	// run is set instead, for a change with work to start.
	run func(string) tea.Cmd

	what  string            // what it does, for About
	means map[string]string // what each value means
	unset string            // how "" shows; "default" when not given
	names map[string]string // how other values show, when not as kept
	typed bool              // enter types a value rather than choosing one

	line  func(w int) string                      // draws the row itself
	key   func(s string) (cmd tea.Cmd, used bool) // its own keys, before ←→
	keys  []string                                // its own keys, for the key line
	about func() (title, what, now string)        // About, when it's more than what and means
}

// shown is how the value is shown.
func (st setting) shown() string {
	if st.value == "" {
		return firstNonEmpty(st.unset, "default")
	}
	return firstNonEmpty(st.names[st.value], st.value)
}

// now is what the value means, in a sentence.
func (st setting) now() string {
	if s := st.means[st.value]; s != "" {
		return st.shown() + ": " + s
	}
	return st.shown() + "."
}

// flat is a form's rows in order.
func flat(secs []section) []setting {
	var out []setting
	for _, s := range secs {
		out = append(out, s.rows...)
	}
	return out
}

// cycle moves s to the choice dir away, round the ends.
func cycle(s setting, dir int) tea.Cmd {
	if len(s.choices) == 0 {
		return nil
	}
	i := 0
	for j, c := range s.choices {
		if c == s.value {
			i = j
		}
	}
	v := s.choices[(i+dir+len(s.choices))%len(s.choices)]
	if s.run != nil {
		return s.run(v)
	}
	s.set(v)
	return nil
}

// formKey handles a key on a form's highlighted row.
func (m *Model) formKey(secs []section, s string) tea.Cmd {
	d := m.dialog
	rows := flat(secs)
	if d.cursor >= len(rows) {
		return nil
	}
	st := rows[d.cursor]
	if st.key != nil {
		if cmd, used := st.key(s); used {
			return cmd
		}
	}
	changed := func(cmd tea.Cmd) tea.Cmd {
		_ = m.store.SaveConfig()
		m.rebuild()
		return cmd
	}
	switch s {
	case "enter", "right", "l", "space":
		if st.typed && (s == "enter" || len(st.choices) == 0) {
			m.ask(st.label, st.value, func(v string) tea.Cmd {
				if st.run != nil {
					return changed(st.run(v))
				}
				st.set(v)
				return changed(nil)
			})
			return nil
		}
		return changed(cycle(st, 1))
	case "left", "h":
		return changed(cycle(st, -1))
	}
	return nil
}

// formBody draws a form's sections, About for the highlighted row, and
// the keys.
func (m *Model) formBody(secs []section, pageKeys []string, w int) []string {
	cur := rowAt(secs, m.dialog.cursor)
	out := append(m.formRows(secs, m.dialog.cursor, w), m.about(cur, w)...)
	return append(out, "", m.formKeys(cur, pageKeys, w))
}

// rowAt is a form's row i, or none.
func rowAt(secs []section, i int) setting {
	if rows := flat(secs); i >= 0 && i < len(rows) {
		return rows[i]
	}
	return setting{}
}

// formRows draws a form's sections, row cur highlighted.
func (m *Model) formRows(secs []section, cur, w int) []string {
	rows := flat(secs)
	// One label and one value column for the page, so the › line up.
	labelW, valueW := 0, 12
	for _, st := range rows {
		if st.line == nil {
			labelW = max(labelW, cellw.String(st.label)+2)
			valueW = max(valueW, cellw.String(st.shown()))
		}
	}
	labelW, valueW = min(labelW, 32), min(valueW, max(12, min(40, w-labelW-24)))
	var out []string
	i := 0
	for _, sec := range secs {
		out = append(out, "")
		if sec.title != "" {
			title := dim(sec.title)
			if sec.note != "" {
				title += faint(" · " + sec.note)
			}
			out = append(out, title)
		}
		for _, st := range sec.rows {
			out = append(out, m.settingRow(i == cur, st, labelW, valueW, w))
			i++
		}
	}
	return out
}

// formKeys is the key line for row cur of a form.
func (m *Model) formKeys(cur setting, pageKeys []string, w int) string {
	keys := []string{}
	if cur.line == nil && len(cur.choices) > 0 {
		keys = append(keys, "←→", "change")
	}
	if cur.typed {
		keys = append(keys, "enter", "type it")
	}
	keys = append(keys, cur.keys...)
	return keysFit(w, append(append(keys, pageKeys...), pagesKeys...)...)
}

// settingRow is always one line, the highlighted one too, so moving the
// highlight never shifts the page; About explains it.
func (m *Model) settingRow(on bool, st setting, labelW, valueW, w int) string {
	var line string
	if st.line != nil {
		line = st.line(w - 4)
	} else {
		line = fit(st.label, labelW) + faint("‹ ") + paint(cText, fit(st.shown(), valueW)) + faint(" › ")
		if room := w - cellw.String(line) - 6; room > 12 {
			line += faint(ansi.Truncate(st.means[st.value], room, "…"))
		}
	}
	if on {
		return highlight(paint(cOrange, "▍")+" "+line, w)
	}
	return "  " + line
}

const aboutLines = 4

// about explains the highlighted row in a fixed-height section, so the
// page keeps its shape whatever is highlighted.
func (m *Model) about(st setting, w int) []string {
	title, what, now := st.label, st.what, st.now()
	if st.about != nil {
		title, what, now = st.about()
	}
	var body []string
	add := func(col, text string) {
		for _, l := range wrap(text, w-4) {
			body = append(body, "  "+paint(col, l))
		}
	}
	add(cSub, what)
	add(cText, now)
	out := []string{"", rule("About "+title, "", w)}
	for i := range aboutLines {
		if i < len(body) {
			out = append(out, body[i])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// choiceSetting is a setting from a list of values and what each means.
func choiceSetting(label, value, what string, choices [][2]string, set func(string)) setting {
	st := setting{label: label, value: value, what: what, set: set, means: map[string]string{}}
	for _, c := range choices {
		st.choices = append(st.choices, c[0])
		st.means[c[0]] = c[1]
	}
	return st
}
