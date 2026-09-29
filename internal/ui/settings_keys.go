package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/keymap"
)

// Settings, Keys: every action rush's keys do and the keys it has, one
// place at a time (1-9 or ← → pick it). enter takes the keys you press
// next (a chord is several, ended with enter), a adds another, x takes
// them all away, r puts rush's back. What you change goes in
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

// keyRows are the actions of the context Keys shows, rush's before
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
		return "anywhere in rush, over sheets and the command bar too"
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
// alternatives side by side. lit, when it matches one, lights it: what's
// being practiced.
func keyCaps(seqs []keymap.Seq, lit string) string {
	if len(seqs) == 0 {
		return faint("no key")
	}
	var out []string
	for _, s := range seqs {
		out = append(out, keycap(s.String(), lit != "" && s.String() == lit))
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
	for _, l := range wrap("What each key does, by where it works. Pick one and press enter to give it keys of your own; they replace rush's for it. Yours are kept in "+tildify(keymap.Path())+".", w-4) {
		if short {
			break
		}
		out = append(out, dim(l))
	}
	if p := km.Problems(); len(p) > 0 {
		out = append(out, "", paint(cYellow, "! ")+paint(cText, "keybindings.json has bindings rush can't use:"))
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
	practicing := ""
	if len(m.practiced(d.practice)) > 0 {
		practicing = d.practice.String()
	}
	for i := from; i < to; i++ {
		a := rows[i]
		var keys string
		if kp := m.keysTaking(); kp != nil && kp.taking == a.ID {
			keys = paint(cOrange, " press keys…")
		} else {
			keys = keyCaps(km.Keys(a.ID), practicing)
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
	out = append(out, "")
	if cur.ID != "" {
		out = append(out, rule(cur.Title, "", w), "", label("Keys")+keyCaps(km.Keys(cur.ID), ""))
		if km.Changed(cur.ID) {
			def := faint("none")
			if len(cur.Keys) > 0 {
				var seqs []keymap.Seq
				for _, k := range cur.Keys {
					seqs = append(seqs, keymap.Seq(strings.Fields(k)))
				}
				def = keyCaps(seqs, "")
			}
			out = append(out, label("rush's")+def+paint(cBlue, "   • yours now: r puts these back"))
		}
		out = append(out, label("Works")+" "+dim(contextWhere(cur.Context)),
			label("Name")+" "+faint(cur.ID+", as keybindings.json calls it"))
	}
	keys := append([]string{"enter", "new keys", "a", "add a key", "x", "no key", "r", "rush's", "1-" + strconv.Itoa(len(ctxs)) + " ← →", "where"}, pagesKeys...)
	return append(out, "", keysFit(w, keys...))
}

// keysModal asks for the keys being taken in a box over base.
func (m *Model) keysModal(base string) string {
	kp := m.keysTaking()
	a, _ := m.keyMap().Action(kp.taking)
	bw := min(m.w-4, 64)
	verb := "New keys for "
	if kp.adding {
		verb = "Another key for "
	}
	pressed := faint("…")
	if len(kp.pressed) > 0 {
		pressed = keycap(kp.pressed.String(), true)
	}
	body := []string{paint(cText+bold, verb+a.Title), faint(a.ID), "",
		dim(fit("Now", 10)) + keyCaps(m.keyMap().Keys(a.ID), ""),
		dim(fit("Pressed", 10)) + pressed, ""}
	for _, l := range wrap("Press the key, or up to three for a chord (ctrl+x then p, say).", bw-4) {
		body = append(body, dim(l))
	}
	if a.Context != keymap.Global {
		for _, l := range wrap("A key that types a character, p say, can't be one on its own here: start a chord with it instead.", bw-4) {
			body = append(body, dim(l))
		}
	}
	body = append(body, "", keys("enter", "keep", "esc", "cancel"))
	return m.modalOver(base, body, bw, cOrange)
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
	default:
		return m.practiceKey(s)
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

// saveBinding sets id's keys (nil for rush's) and writes
// keybindings.json, off the UI.
func (m *Model) saveBinding(id string, keys []string) tea.Cmd {
	f := m.keys.file.With(id, keys)
	m.setKeys(f)
	switch {
	case keys == nil:
		m.flash(id+": rush's keys again", false)
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

// practiceKey is a key pressed on Keys that isn't the page's own: tried
// against every action's keys (a chord's first key waits for the rest, up
// to chordWait), so a key can be pressed to see, live, what it's bound to.
// A match jumps the page to it and lights its chip; a miss clears back to
// nothing, ready for the next key tried.
func (m *Model) practiceKey(s string) tea.Cmd {
	d := m.dialog
	if time.Since(d.practiceAt) > chordWait {
		d.practice = nil
	}
	d.practiceAt = time.Now()
	try := func(seq keymap.Seq) bool {
		if a := m.practiced(seq); len(a) > 0 {
			d.practice = seq
			m.showKey(a[0].ID)
			return true
		}
		if len(seq) < 3 && m.practicedPrefix(seq) {
			d.practice = seq
			return true
		}
		return false
	}
	if !try(append(slices.Clone(d.practice), s)) && !try(keymap.Seq{s}) {
		d.practice = nil
	}
	return nil
}

// practiced are the actions whose keys are seq, exactly.
func (m *Model) practiced(seq keymap.Seq) []keymap.Action {
	if len(seq) == 0 {
		return nil
	}
	s := seq.String()
	var out []keymap.Action
	for _, a := range m.keyMap().Actions() {
		for _, k := range m.keyMap().Keys(a.ID) {
			if k.String() == s {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// practicedPrefix is whether seq begins a longer chord bound to something.
func (m *Model) practicedPrefix(seq keymap.Seq) bool {
	s := seq.String()
	for _, a := range m.keyMap().Actions() {
		for _, k := range m.keyMap().Keys(a.ID) {
			if full := k.String(); len(full) > len(s) && strings.HasPrefix(full, s) && full[len(s)] == ' ' {
				return true
			}
		}
	}
	return false
}
