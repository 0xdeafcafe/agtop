package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/plugin"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// --- undo ---

// undoStack is an input box's undo: what the box held before each change.
// A run of typing is one step, as in any editor.
type undoStack struct {
	past, future []undoState
	typing       bool // the last step saved was typing, so more joins it
}

type undoState struct {
	text []rune
	back int
}

const maxUndo = 200

// save keeps text as a state to come back to. Typing straight after
// typing joins the step before it.
func (u *undoStack) save(text []rune, back int, typing bool) {
	if typing && u.typing {
		return
	}
	u.typing = typing
	u.past = append(u.past, undoState{slices.Clone(text), back})
	if len(u.past) > maxUndo {
		u.past = u.past[len(u.past)-maxUndo:]
	}
	u.future = nil
}

// undo swaps the box's text for the one before it; redo goes the other way.
func (u *undoStack) undo(text []rune, back int) ([]rune, int, bool) {
	return u.step(&u.past, &u.future, text, back)
}

func (u *undoStack) redo(text []rune, back int) ([]rune, int, bool) {
	return u.step(&u.future, &u.past, text, back)
}

func (u *undoStack) step(from, to *[]undoState, text []rune, back int) ([]rune, int, bool) {
	u.typing = false
	if len(*from) == 0 {
		return text, back, false
	}
	st := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, undoState{slices.Clone(text), back})
	return st.text, min(st.back, len(st.text)), true
}

func isUndo(s string) bool { return s == "super+z" || s == "ctrl+_" || s == "ctrl+/" }
func isRedo(s string) bool { return s == "super+shift+z" || s == "ctrl+y" }

// undoKey runs undo or redo on the Session's box.
func (m *Model) undoKey(c *hostConn, s string) bool {
	var buf []rune
	var back int
	var ok bool
	switch {
	case isUndo(s):
		buf, back, ok = c.undo.undo(c.input, c.back)
		if !ok {
			m.flash("nothing to undo · ctrl+r has what you typed before", false)
		}
	case isRedo(s):
		buf, back, ok = c.undo.redo(c.input, c.back)
	default:
		return false
	}
	if ok {
		c.input, c.back, c.anchor = buf, back, 0
	}
	return true
}

// --- drafts ---

// Drafts come in three kinds (state.DraftKinds): kept on purpose with
// alt+s, or when something else takes the box's place; sent; and cleared
// from the box without sending. alt+p brings the drafts back, newest
// first; ctrl+r shows all three.

// boxDraft is what's in the Session's box as a Draft of a kind. Pastes are
// kept whole, to come back as chips.
func (m *Model) boxDraft(c *hostConn, kind string) (state.Draft, bool) {
	text := strings.TrimSpace(c.pastes.expand(string(c.input), true))
	if text == "" {
		return state.Draft{}, false
	}
	d := state.Draft{Text: text, At: time.Now(), Agent: c.key, Kind: kind}
	if a := m.agentByKey(c.key); a != nil {
		d.Name = a.DisplayName
	}
	return d, true
}

// promptDraft is boxDraft for the Prompt under Agents.
func (m *Model) promptDraft(kind string) (state.Draft, bool) {
	text := strings.TrimSpace(m.pastes.expand(string(m.input), true))
	if text == "" {
		return state.Draft{}, false
	}
	return state.Draft{Text: text, At: time.Now(), Kind: kind}, true
}

// keepLater saves d without holding up the frame.
func keepLater(d state.Draft, ok bool) {
	if ok {
		go func() { _ = state.AddDraft(d) }()
	}
}

// keepDraft keeps what's in the Session's box as a kind of draft.
func (m *Model) keepDraft(c *hostConn, kind string) {
	keepLater(m.boxDraft(c, kind))
}

// wipeBox clears the Session's box, keeping what was in it to undo and in
// the cleared drafts.
func (m *Model) wipeBox(c *hostConn) {
	if len(c.input) == 0 {
		return
	}
	c.undo.save(c.input, c.back, false)
	m.keepDraft(c, state.KindCleared)
	m.emitBox(plugin.EvInputCleared, c.key, string(c.input))
	c.input, c.back, c.anchor = nil, 0, 0
	m.flash("cleared · "+undoHint+" brings it back · ctrl+r keeps it under Cleared", false)
}

const undoHint = "cmd+z or ctrl+/"

// wipePrompt is wipeBox for the Prompt under Agents.
func (m *Model) wipePrompt() {
	if len(m.input) == 0 {
		return
	}
	keepLater(m.promptDraft(state.KindCleared))
	m.emitBox(plugin.EvInputCleared, "", string(m.input))
	m.input, m.back, m.anchor = nil, 0, 0
	m.flash("cleared · #drafts keeps it under Cleared", false)
}

// Keys for drafts, the same in a Session's box and in the Prompt.
const (
	keySaveDraft   = "alt+s"
	keyRecallDraft = "alt+p"
)

// saveDraft is alt+s: what's in the Session's box kept as a draft, and the
// box emptied for the next thing.
func (m *Model) saveDraft(c *hostConn) {
	d, ok := m.boxDraft(c, state.KindDraft)
	if !ok {
		m.flash("nothing to keep · "+keySaveDraft+" keeps what you've typed as a draft", false)
		return
	}
	c.undo.save(c.input, c.back, false)
	_ = state.AddDraft(d)
	c.input, c.back, c.anchor = nil, 0, 0
	c.recall = recall{}
	m.flash(draftKept(), false)
}

// savePromptDraft is saveDraft for the Prompt under Agents.
func (m *Model) savePromptDraft() {
	d, ok := m.promptDraft(state.KindDraft)
	if !ok {
		m.flash("nothing to keep · "+keySaveDraft+" keeps what you've typed as a draft", false)
		return
	}
	_ = state.AddDraft(d)
	m.input, m.back, m.anchor = nil, 0, 0
	m.recall = recall{}
	m.flash(draftKept(), false)
}

func draftKept() string {
	n := draftCount()
	return fmt.Sprintf("kept as a draft · %d draft%s · %s brings the latest back", n, plural(n), keyRecallDraft)
}

// recall is alt+p going back through the drafts: them as they were at
// the first press, which is in the box, and the text put there.
type recall struct {
	list  []string
	at    int
	shown string
}

// next is the draft alt+p puts in a box holding cur: the newest, or,
// pressed again with the one it put there untouched, the one before,
// round to the newest again. again says it was pressed again.
func (r *recall) next(cur string) (text string, again, ok bool) {
	cur = strings.TrimSpace(cur)
	if len(r.list) > 0 && cur == r.shown {
		r.at = (r.at + 1) % len(r.list)
		again = true
	} else {
		*r = recall{}
		for _, d := range state.DraftsOf(state.KindDraft) {
			if strings.TrimSpace(d.Text) != cur {
				r.list = append(r.list, d.Text)
			}
		}
		if len(r.list) == 0 {
			return "", false, false
		}
	}
	r.shown = strings.TrimSpace(r.list[r.at])
	return r.list[r.at], again, true
}

// said is what alt+p says it put in the box.
func (r *recall) said() string {
	n := len(r.list)
	switch {
	case n == 1:
		return "your draft · ctrl+r has what you sent and cleared too"
	case r.at == n-1:
		return fmt.Sprintf("your oldest draft, %d of %d · %s goes round to the newest", n, n, keyRecallDraft)
	case r.at == 0:
		return fmt.Sprintf("your latest draft, 1 of %d · %s again for older", n, keyRecallDraft)
	}
	return fmt.Sprintf("draft %d of %d · %s again for older", r.at+1, n, keyRecallDraft)
}

const noDrafts = "no drafts yet · " + keySaveDraft + " keeps what you've typed as one · ctrl+r has what you sent and cleared"

// recallDraft is alt+p in a Session's box: the latest draft, then older
// ones. What the box held first is kept as a draft, so nothing is lost.
func (m *Model) recallDraft(c *hostConn) {
	text, again, ok := c.recall.next(c.pastes.expand(string(c.input), true))
	if !ok {
		m.flash(noDrafts, false)
		return
	}
	if !again {
		m.keepDraft(c, state.KindDraft)
	}
	c.undo.save(c.input, c.back, false)
	c.input, c.back, c.anchor = c.pastes.unfold(text), 0, 0
	c.sel = ""
	m.flash(c.recall.said(), false)
}

// recallPromptDraft is recallDraft for the Prompt under Agents.
func (m *Model) recallPromptDraft() {
	text, again, ok := m.recall.next(m.pastes.expand(string(m.input), true))
	if !ok {
		m.flash(noDrafts, false)
		return
	}
	if !again {
		keepLater(m.promptDraft(state.KindDraft))
	}
	m.input, m.back, m.anchor = m.pastes.unfold(text), 0, 0
	m.flash(m.recall.said(), false)
}

// draftsHolder is what an empty box says: what, and how many drafts wait
// with the key that brings them back; none when there are none.
func draftsHolder(what, none string) string {
	if n := draftCount(); n > 0 {
		return fmt.Sprintf("%s · %d draft%s, %s for the latest", what, n, plural(n), keyRecallDraft)
	}
	return what + none
}

// draftSheet lists what you've typed before, in three tabs: drafts, sent
// and cleared, newest first; enter puts one back in the box.
type draftSheet struct {
	all    []state.Draft
	kind   int // which of state.DraftKinds shows
	filter []rune
	cur    int
	host   *hostConn // the box it goes back into; nil for the Prompt
}

var draftKindNames = map[string]string{state.KindDraft: "Drafts", state.KindSent: "Sent", state.KindCleared: "Cleared"}

func (m *Model) openDrafts(c *hostConn) {
	d := &draftSheet{all: state.Drafts(), host: c}
	d.kind = d.startKind()
	m.sheet = d
}

// startKind is the tab the sheet opens on: the kind of what you just did,
// a minute ago or less; else the drafts, when there are any.
func (d *draftSheet) startKind() int {
	if len(d.all) == 0 {
		return 0
	}
	newest := slices.Index(state.DraftKinds, d.all[0].Kind)
	if time.Since(d.all[0].At) < time.Minute {
		return max(0, newest)
	}
	if d.count(state.KindDraft) > 0 {
		return 0
	}
	return max(0, newest)
}

func (d *draftSheet) kindOf() string { return state.DraftKinds[d.kind] }

func (d *draftSheet) count(kind string) int {
	n := 0
	for _, x := range d.all {
		if x.Kind == kind {
			n++
		}
	}
	return n
}

func (d *draftSheet) shown() []state.Draft {
	q := strings.ToLower(strings.TrimSpace(string(d.filter)))
	var out []state.Draft
	for _, x := range d.all {
		if x.Kind != d.kindOf() {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(x.Text), q) || strings.Contains(strings.ToLower(x.Name), q) {
			out = append(out, x)
		}
	}
	return out
}

func (d *draftSheet) width(m *Model) int { return 112 }

func (d *draftSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Drafts", "what you kept, sent and cleared · enter puts one back in the box", w), ""}
	var tabs []string
	for _, k := range state.DraftKinds {
		tabs = append(tabs, fmt.Sprintf("%s %d", draftKindNames[k], d.count(k)))
	}
	out = append(out, sheetTabs(tabs, d.kind), "")
	out = append(out, dim("find ")+textField(d.filter, len(d.filter), true, "type to search", w-5), "")
	list := d.shown()
	d.cur = max(0, min(d.cur, len(list)-1))
	rows := max(3, h-len(out)-4)
	if len(list) == 0 {
		msg := map[string]string{
			state.KindDraft:   "no drafts yet · " + keySaveDraft + " in a box keeps what you've typed here, and " + keyRecallDraft + " brings it back",
			state.KindSent:    "nothing sent yet · the messages you send land here, to use again",
			state.KindCleared: "nothing cleared · what you wipe from a box with esc or ctrl+c lands here",
		}[d.kindOf()]
		if len(d.filter) > 0 {
			msg = "none of these has that · tab looks in the next"
		}
		out = append(out, "  "+faint(msg))
	}
	from, to := window(len(list), d.cur, rows)
	for i := from; i < to; i++ {
		x := list[i]
		meta := faint(age(time.Since(x.At)) + " ago")
		if x.Name != "" {
			meta = faint(ansi.Truncate(oneLine(x.Name), 22, "…")+" · ") + meta
		}
		text := shortImages(oneLine(x.Text))
		room := max(8, w-4-ansi.StringWidth(meta)-4)
		line := paint(cText, ansi.Truncate(text, room, "…"))
		pad := max(1, w-4-ansi.StringWidth(line)-ansi.StringWidth(meta))
		out = append(out, sheetRow(line+strings.Repeat(" ", pad)+meta, i == d.cur, w))
	}
	if len(list) > 0 {
		lines := strings.Split(strings.TrimSpace(list[d.cur].Text), "\n")
		if len(lines) > 1 {
			out = append(out, "", faint(ansi.Truncate(strings.Join(lines[1:min(len(lines), 3)], " ⏎ "), w, "…")))
		}
	}
	pairs := []string{"↑↓", "choose", "enter", "put it in the box", "tab", "drafts · sent · cleared"}
	if d.kindOf() != state.KindDraft {
		pairs = append(pairs, keySaveDraft, "keep as a draft")
	}
	return append(out, "", keysFit(w, append(pairs, "ctrl+d", "forget it", "esc", "close")...))
}

func (d *draftSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	list := d.shown()
	switch s {
	case "esc", "ctrl+c", "ctrl+r":
		m.sheet = nil
	case "tab", "shift+tab":
		n := len(state.DraftKinds)
		d.kind = (d.kind + map[string]int{"tab": 1, "shift+tab": n - 1}[s]) % n
		d.cur = 0
	case "up", "ctrl+p":
		d.cur = max(0, d.cur-1)
	case "down", "ctrl+n":
		d.cur = min(len(list)-1, d.cur+1)
	case "pgup":
		d.cur = max(0, d.cur-10)
	case "pgdown":
		d.cur = min(len(list)-1, d.cur+10)
	case "ctrl+d":
		if d.cur < len(list) {
			x := list[d.cur]
			d.all = slices.DeleteFunc(d.all, func(o state.Draft) bool { return o.Text == x.Text && o.Kind == x.Kind })
			go func() { _ = state.RemoveDraft(x.Kind, x.Text) }()
		}
	case keySaveDraft:
		if d.cur < len(list) && d.kindOf() != state.KindDraft {
			x := list[d.cur]
			x.Kind, x.At = state.KindDraft, time.Now()
			_ = state.AddDraft(x)
			d.all = state.Drafts()
			m.flash(draftKept(), false)
		}
	case "enter":
		if d.cur < len(list) {
			m.sheet = nil
			m.restoreDraft(d.host, list[d.cur].Text)
		}
	default:
		buf, pos, _ := edit(d.filter, len(d.filter), k, s)
		if pos == len(buf) {
			d.filter = buf
			d.cur = 0
		}
	}
	return nil
}

// restoreDraft puts text back in a box. What the box held is kept, to
// undo back to and in the drafts, so nothing is lost either way.
func (m *Model) restoreDraft(c *hostConn, text string) {
	if c == nil || c != m.host {
		keepLater(m.promptDraft(state.KindDraft))
		m.input, m.back, m.anchor = []rune(strings.TrimSpace(text)), 0, 0
		return
	}
	if len(c.input) > 0 {
		m.keepDraft(c, state.KindDraft)
	}
	c.undo.save(c.input, c.back, false)
	c.input, c.back, c.anchor = c.pastes.unfold(text), 0, 0
	m.paneFocus = true
	c.sel = ""
}

// keepSent keeps a message as it was sent, for the drafts sheet.
func (m *Model) keepSent(c *hostConn, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.emitBox(plugin.EvInputSent, c.key, strings.TrimSpace(text))
	d := state.Draft{Text: strings.TrimSpace(text), At: time.Now(), Agent: c.key, Kind: state.KindSent}
	if a := m.agentByKey(c.key); a != nil {
		d.Name = a.DisplayName
	}
	keepLater(d, true)
}

// clearPrompt empties the Prompt under Agents; a message being written
// there is kept in the drafts first.
func (m *Model) clearPrompt() {
	if m.inKind == inPrompt || m.inKind == inReply {
		m.wipePrompt()
		return
	}
	m.input, m.back, m.anchor = m.input[:0], 0, 0
}

// boxVert moves the Session box's cursor a row up or down through the
// text as it's wrapped, keeping its column. Past the first row it goes to
// the start; past the last, the end. shift extends the selection.
func (m *Model) boxVert(c *hostConn, d int, shift bool) {
	b := c.box
	b.text = c.input
	segs := b.segs()
	pos := len(c.input) - c.back
	row := len(segs) - 1
	for i, sg := range segs {
		if pos >= sg.from && pos <= sg.to {
			row = i
			break
		}
	}
	if shift && c.anchor == 0 {
		c.anchor = pos + 1
	} else if !shift {
		c.anchor = 0
	}
	switch to := row + d; {
	case to < 0:
		pos = 0
	case to >= len(segs):
		pos = len(c.input)
	default:
		col := 0
		for p := segs[row].from; p < pos; p++ {
			col += runeW(c.input[p])
		}
		sg, width := segs[to], 0
		pos = sg.from
		for pos < sg.to && width+runeW(c.input[pos]) <= col {
			width += runeW(c.input[pos])
			pos++
		}
	}
	c.back = len(c.input) - pos
}

// draftCount is how many drafts wait, cheap enough for every frame.
func draftCount() int { return state.DraftCount(state.KindDraft) }
