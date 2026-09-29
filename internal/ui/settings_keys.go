package ui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/keymap"
)

// Settings, Keys: every action agtop's keys do and the keys it has, one
// place at a time (1-9 or ← → pick it). enter takes the keys you press
// next (a chord is several, ended with enter), a adds another, x takes
// them all away, r puts agtop's back. What you change goes in
// keybindings.json.

// keysPage is the page's own state: keys being taken for an action.
type keysPage struct {
	taking  string     // the action keys are being taken for
	adding  bool       // added to its keys, rather than replacing them
	pressed keymap.Seq // the keys pressed so far
}

// keyPlaces are the contexts with actions, in the order the strip
// shows them.
func (m *Model) keyPlaces() []keymap.Context {
	km := m.keyMap()
	var out []keymap.Context
	for _, c := range keymap.Contexts {
		for _, a := range km.Actions() {
			if a.Context == c {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// keyContext is the context Keys shows.
func (m *Model) keyContext() keymap.Context {
	cs := m.keyPlaces()
	if len(cs) == 0 {
		return ""
	}
	return cs[min(m.dialog.keyCtx, len(cs)-1)]
}

// keyRows are the actions of the context Keys shows, agtop's before
// plugins'.
func (m *Model) keyRows() []keymap.Action {
	c := m.keyContext()
	var out []keymap.Action
	for _, a := range m.keyMap().Actions() {
		if a.Context == c {
			out = append(out, a)
		}
	}
	return out
}

// showKey puts Keys on the action id: its context, and the cursor on it.
func (m *Model) showKey(id string) {
	a, ok := m.keyMap().Action(id)
	if !ok {
		return
	}
	d := m.dialog
	d.keyCtx = max(0, slices.Index(m.keyPlaces(), a.Context))
	for i, r := range m.keyRows() {
		if r.ID == id {
			d.cursor = i
		}
	}
}

// contextWhere is where a context's keys work, for the page to say.
func contextWhere(c keymap.Context) string {
	switch c {
	case keymap.Global:
		return "anywhere in agtop, over sheets and the command bar too"
	case keymap.List:
		return "in the Agents list and the Prompt under it"
	case keymap.Session:
		return "in an agent's Session: its conversation and message box"
	case keymap.Any:
		return "#commands, from the list or a Session: none has a key until you give it one"
	}
	return ""
}

func (m *Model) keysLen() int { return len(m.keyRows()) }

// keyCaps draws an action's keys as keycaps, a chord in one cap, the
// alternatives side by side.
func keyCaps(seqs []keymap.Seq) string {
	if len(seqs) == 0 {
		return faint("no key")
	}
	var out []string
	for _, s := range seqs {
		out = append(out, keycap(s.String(), false))
	}
	return strings.Join(out, faint(" or "))
}

func (m *Model) keysBody(w int) []string {
	d := m.dialog
	km := m.keyMap()
	ctxs := m.keyPlaces()
	ctx := m.keyContext()
	rows := m.keyRows()
	d.cursor = min(d.cursor, max(0, len(rows)-1))

	var out []string
	short := m.h < 36 // no room to explain
	for _, l := range wrap("What each key does, by where it works. Pick one and press enter to give it keys of your own; they replace agtop's for it. Yours are kept in "+tildify(keymap.Path())+".", w-4) {
		if short {
			break
		}
		out = append(out, dim(l))
	}
	if p := km.Problems(); len(p) > 0 {
		out = append(out, "", paint(cYellow, "! ")+paint(cText, "keybindings.json has bindings agtop can't use:"))
		for i, pr := range p {
			if i == 3 {
				out = append(out, faint("    … and "+strconv.Itoa(len(p)-3)+" more"))
				break
			}
			out = append(out, "    "+dim(pr.String()))
		}
	}

	// Where: a numbered strip, like Agents'.
	var strip []string
	for i, c := range ctxs {
		n := 0
		yours := 0
		for _, a := range km.Actions() {
			if a.Context == c {
				n++
				if km.Changed(a.ID) {
					yours++
				}
			}
		}
		name := dim(c.Title())
		if c == ctx {
			name = paint(cOrange, "▸") + paint(cText+bold, c.Title())
		}
		count := faint(" " + strconv.Itoa(n))
		if yours > 0 {
			count += paint(cBlue, " •"+strconv.Itoa(yours))
		}
		strip = append(strip, faint(strconv.Itoa(i+1)+" ")+name+count)
	}
	if !short {
		out = append(out, "")
	}
	out = append(out, strings.Join(strip, "     "), "  "+faint(contextWhere(ctx)), "")

	// The table.
	titleW := max(24, min(52, w-40))
	out = append(out, "    "+faint(fit("DOES", titleW)+"    KEYS"))
	// What's left of the screen once the header, the card under the table
	// and the key line are drawn.
	h := max(4, m.h-len(m.header())-21-len(out))
	from, to := window(len(rows), d.cursor, h)
	if from > 0 {
		out = append(out, "    "+faint("↑ "+strconv.Itoa(from)+" more"))
	} else {
		out = append(out, "")
	}
	for i := from; i < to; i++ {
		a := rows[i]
		var keys string
		if kp := m.keysTaking(); kp != nil && kp.taking == a.ID {
			keys = paint(cOrange, " press keys…")
		} else {
			keys = keyCaps(km.Keys(a.ID))
		}
		title := paint(cText, a.Title)
		if a.Source != "" {
			title += faint("  " + a.Source)
		}
		mark := "  "
		if km.Changed(a.ID) {
			mark = paint(cBlue, "• ")
		}
		line := mark + fit(title, titleW) + "    " + keys
		if i == d.cursor {
			out = append(out, highlight(paint(cOrange, "▍")+" "+line, w))
		} else {
			out = append(out, "  "+line)
		}
	}
	if to < len(rows) {
		out = append(out, "    "+faint("↓ "+strconv.Itoa(len(rows)-to)+" more"))
	} else {
		out = append(out, "")
	}

	// The one under the cursor, or the keys being taken for it.
	var cur keymap.Action
	if d.cursor < len(rows) {
		cur = rows[d.cursor]
	}
	label := func(s string) string { return "    " + dim(fit(s, 12)) }
	text := func(s string) []string {
		var l []string
		for _, t := range wrap(s, w-6) {
			l = append(l, "     "+dim(t))
		}
		return l
	}
	out = append(out, "")
	if kp := m.keysTaking(); kp != nil {
		verb := "New keys for "
		if kp.adding {
			verb = "Another key for "
		}
		pressed := faint("…")
		if len(kp.pressed) > 0 {
			pressed = keycap(kp.pressed.String(), true)
		}
		out = append(out, rule(verb+cur.Title, "", w), "", label("Pressed")+pressed, "")
		out = append(out, text("Press the key, or up to three for a chord (ctrl+x then p, say). enter keeps them; esc leaves it as it was.")...)
		if cur.Context != keymap.Global {
			out = append(out, text("A key that types a character, p say, can't be one on its own here: start a chord with it instead.")...)
		}
	} else if cur.ID != "" {
		out = append(out, rule(cur.Title, "", w), "", label("Keys")+keyCaps(km.Keys(cur.ID)))
		if km.Changed(cur.ID) {
			def := faint("none")
			if len(cur.Keys) > 0 {
				var seqs []keymap.Seq
				for _, k := range cur.Keys {
					seqs = append(seqs, keymap.Seq(strings.Fields(k)))
				}
				def = keyCaps(seqs)
			}
			out = append(out, label("agtop's")+def+paint(cBlue, "   • yours now: r puts these back"))
		}
		out = append(out, label("Works")+" "+dim(contextWhere(cur.Context)),
			label("Name")+" "+faint(cur.ID+", as keybindings.json calls it"))
	}
	keys := append([]string{"enter", "new keys", "a", "add a key", "x", "no key", "r", "agtop's", "1-" + strconv.Itoa(len(ctxs)) + " ← →", "where"}, pagesKeys...)
	if m.keysTaking() != nil {
		keys = []string{"keys", "press them", "enter", "keep", "esc", "cancel"}
	}
	return append(out, "", keysFit(w, keys...))
}

// keysTaking is the keys being taken, if they are.
func (m *Model) keysTaking() *keysPage {
	if m.keys.page.taking == "" {
		return nil
	}
	return &m.keys.page
}

func (m *Model) keysKey(s string) tea.Cmd {
	rows := m.keyRows()
	d := m.dialog
	if d.cursor >= len(rows) {
		return nil
	}
	a := rows[d.cursor]
	switch s {
	case "left", "right", "h", "l":
		n := len(m.keyPlaces())
		d.keyCtx = (min(d.keyCtx, n-1) + map[bool]int{true: -1, false: 1}[s == "left" || s == "h"] + n) % n
		d.cursor = 0
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(s[0] - '1'); i < len(m.keyPlaces()) {
			d.keyCtx, d.cursor = i, 0
		}
	case "enter", "a":
		m.keys.page = keysPage{taking: a.ID, adding: s == "a"}
		m.keys.capture = m.takeKey
		return nil
	case "x", "backspace", "delete":
		return m.saveBinding(a.ID, []string{})
	case "r":
		return m.saveBinding(a.ID, nil)
	}
	return nil
}

// takeKey is each key pressed while keys are being taken: every key comes
// here first, unmapped.
func (m *Model) takeKey(s string) tea.Cmd {
	kp := &m.keys.page
	done := func() tea.Cmd {
		id, seq, adding := kp.taking, kp.pressed, kp.adding
		m.keys.page = keysPage{}
		if len(seq) == 0 {
			return nil
		}
		var keys []string
		if adding {
			for _, k := range m.keyMap().Keys(id) {
				keys = append(keys, k.String())
			}
		}
		keys = append(keys, seq.String())
		if a, _ := m.keyMap().Action(id); keymap.Types(seq[0]) && a.Context != keymap.Global && !slices.Contains(a.Keys, seq[0]) {
			m.flash(seq[0]+" types a character, so it can't start a key: make it a chord, ctrl+x then "+seq[0]+" say", true)
			return nil
		}
		if c := m.keyMap().Conflicts(id, seq); len(c) > 0 {
			m.confirmThen(seq.String()+" is "+strings.Join(c, ", ")+"'s now: take it?", func() tea.Cmd { return m.saveBinding(id, keys) })
			return nil
		}
		return m.saveBinding(id, keys)
	}
	switch {
	case s == "esc":
		m.keys.page = keysPage{}
		return nil
	case s == "enter" && len(kp.pressed) > 0:
		return done()
	}
	kp.pressed = append(kp.pressed, s)
	if len(kp.pressed) == 3 {
		return done()
	}
	m.keys.capture = m.takeKey // the next key too
	return nil
}

// saveBinding sets id's keys (nil for agtop's) and writes
// keybindings.json, off the UI.
func (m *Model) saveBinding(id string, keys []string) tea.Cmd {
	f := m.keys.file.With(id, keys)
	m.setKeys(f)
	switch {
	case keys == nil:
		m.flash(id+": agtop's keys again", false)
	case len(keys) == 0:
		m.flash(id+": no key", false)
	default:
		m.flash(id+": "+m.keyMap().KeyText(id), false)
	}
	return sheetDo(func() (struct{}, error) { return struct{}{}, keymap.Save(f) }, func(m *Model, _ struct{}, err error) tea.Cmd {
		if err != nil {
			m.flash("keybindings.json: "+err.Error(), true)
		}
		return nil
	})
}
