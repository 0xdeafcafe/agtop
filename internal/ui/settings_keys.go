package ui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/keymap"
)

// Settings, Keys: every action agtop's keys do, where each works, and the
// keys it has. enter takes the keys you press next (a chord is several,
// ended with enter), a adds another, x takes them all away, r puts
// agtop's back. What you change goes in keybindings.json.

// keysPage is the page's own state: keys being taken for an action.
type keysPage struct {
	taking  string     // the action keys are being taken for
	adding  bool       // added to its keys, rather than replacing them
	pressed keymap.Seq // the keys pressed so far
}

// keyRows are the actions in the order the page lists them: by context,
// agtop's before plugins'.
func (m *Model) keyRows() []keymap.Action {
	km := m.keyMap()
	var out []keymap.Action
	for _, c := range keymap.Contexts {
		for _, a := range km.Actions() {
			if a.Context == c {
				out = append(out, a)
			}
		}
	}
	return out
}

func (m *Model) keysLen() int { return len(m.keyRows()) }

func (m *Model) keysBody(w int) []string {
	d := m.dialog
	km := m.keyMap()
	rows := m.keyRows()
	var out []string
	if p := km.Problems(); len(p) > 0 {
		out = append(out, paint(cYellow, "keybindings.json has bindings agtop can't use:"))
		for i, pr := range p {
			if i == 3 {
				out = append(out, faint("  … and "+strconv.Itoa(len(p)-3)+" more"))
				break
			}
			out = append(out, "  "+dim(pr.String()))
		}
		out = append(out, "")
	}
	titleW := max(20, min(56, w-44))
	h := max(6, m.h-14-len(out))
	from, to := window(len(rows), d.cursor, h)
	last := keymap.Context("")
	if from > 0 {
		last = rows[from-1].Context
	}
	for i := from; i < to; i++ {
		a := rows[i]
		if a.Context != last {
			out = append(out, "", dim(a.Context.Title()))
			last = a.Context
		}
		keys := km.KeyText(a.ID)
		if kp := m.keysTaking(); kp != nil && kp.taking == a.ID {
			keys = paint(cOrange, "press keys… "+kp.pressed.String())
		} else if keys == "" {
			keys = faint("—")
		} else {
			keys = paint(cText, keys)
		}
		title := a.Title
		if a.Source != "" {
			title = a.Title + faint("  · "+a.Source)
		}
		mark := "  "
		if km.Changed(a.ID) {
			mark = paint(cBlue, "• ")
		}
		line := mark + fit(title, titleW) + "  " + keys
		if i == d.cursor {
			out = append(out, highlight(paint(cOrange, "▍")+" "+line, w))
		} else {
			out = append(out, "  "+line)
		}
	}
	var cur keymap.Action
	if d.cursor < len(rows) {
		cur = rows[d.cursor]
	}
	out = append(out, "", rule("About "+cur.ID, "", w))
	what := "Default: " + firstNonEmpty(strings.Join(cur.Keys, " · "), "no key") + "."
	if cur.Command() {
		what = "A command: it has no key until you give it one."
	}
	out = append(out, "  "+paint(cSub, cur.Title), "  "+dim(what), "  "+faint("A key that types a character can't start one: use a chord (ctrl+x then d, say). "+tildify(keymap.Path())))
	keys := append([]string{"enter", "new keys", "a", "add a key", "x", "no key", "r", "agtop's"}, pagesKeys...)
	if m.keysTaking() != nil {
		keys = append([]string{"keys", "press them", "enter", "done", "esc", "cancel"}, pagesKeys...)
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
