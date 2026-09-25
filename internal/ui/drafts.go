package ui

import (
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// keepDraft keeps what's in the Session's box, sent or about to be
// cleared, for the drafts sheet. Pastes are kept whole, to come back as
// chips.
func (m *Model) keepDraft(c *hostConn, sent bool) {
	text := strings.TrimSpace(c.pastes.expand(string(c.input), true))
	if text == "" {
		return
	}
	d := state.Draft{Text: text, At: time.Now(), Agent: c.key, Sent: sent}
	if a := m.agentByKey(c.key); a != nil {
		d.Name = a.DisplayName
	}
	go func() { _ = state.AddDraft(d) }()
}

// wipeBox clears the Session's box, keeping what was in it to undo and in
// the drafts.
func (m *Model) wipeBox(c *hostConn) {
	if len(c.input) == 0 {
		return
	}
	c.undo.save(c.input, c.back, false)
	m.keepDraft(c, false)
	c.input, c.back, c.anchor = nil, 0, 0
	m.flash("cleared · "+undoHint+" brings it back · ctrl+r for past drafts", false)
}

const undoHint = "cmd+z or ctrl+/"

// wipePrompt is wipeBox for the Prompt under Agents.
func (m *Model) wipePrompt() {
	if len(m.input) == 0 {
		return
	}
	if text := strings.TrimSpace(m.pastes.expand(string(m.input), true)); text != "" {
		d := state.Draft{Text: text, At: time.Now()}
		go func() { _ = state.AddDraft(d) }()
	}
	m.input, m.back, m.anchor = nil, 0, 0
	m.flash("cleared · #drafts brings it back", false)
}

// draftSheet lists what you've typed before, sent and cleared, newest
// first; enter puts one back in the box.
type draftSheet struct {
	all    []state.Draft
	filter []rune
	cur    int
	host   *hostConn // the box it goes back into; nil for the Prompt
}

func (m *Model) openDrafts(c *hostConn) {
	m.sheet = &draftSheet{all: state.Drafts(), host: c}
}

func (d *draftSheet) shown() []state.Draft {
	q := strings.ToLower(strings.TrimSpace(string(d.filter)))
	if q == "" {
		return d.all
	}
	var out []state.Draft
	for _, x := range d.all {
		if strings.Contains(strings.ToLower(x.Text), q) || strings.Contains(strings.ToLower(x.Name), q) {
			out = append(out, x)
		}
	}
	return out
}

func (d *draftSheet) width(m *Model) int { return 112 }

func (d *draftSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Drafts", "what you've typed before, sent and cleared", w), ""}
	out = append(out, dim("find ")+textField(d.filter, len(d.filter), true, "type to search", w-5), "")
	list := d.shown()
	d.cur = max(0, min(d.cur, len(list)-1))
	rows := max(3, h-len(out)-4)
	if len(list) == 0 {
		msg := "nothing yet · a message you send, or clear with esc, lands here"
		if len(d.filter) > 0 {
			msg = "no draft has that"
		}
		out = append(out, "  "+faint(msg))
	}
	from, to := window(len(list), d.cur, rows)
	for i := from; i < to; i++ {
		x := list[i]
		kind := paint(cOrange, "cleared")
		if x.Sent {
			kind = dim("sent   ")
		}
		meta := faint(age(time.Since(x.At)) + " ago")
		if x.Name != "" {
			meta = faint(ansi.Truncate(oneLine(x.Name), 22, "…")+" · ") + meta
		}
		text := shortImages(oneLine(x.Text))
		room := max(8, w-4-ansi.StringWidth(kind)-ansi.StringWidth(meta)-4)
		line := kind + "  " + paint(cText, ansi.Truncate(text, room, "…"))
		pad := max(1, w-4-ansi.StringWidth(line)-ansi.StringWidth(meta))
		out = append(out, sheetRow(line+strings.Repeat(" ", pad)+meta, i == d.cur, w))
	}
	if len(list) > 0 {
		lines := strings.Split(strings.TrimSpace(list[d.cur].Text), "\n")
		if len(lines) > 1 {
			out = append(out, "", faint(ansi.Truncate(strings.Join(lines[1:min(len(lines), 3)], " ⏎ "), w, "…")))
		}
	}
	return append(out, "", keysFit(w, "↑↓", "choose", "enter", "put it in the box", "ctrl+d", "forget it", "esc", "close"))
}

func (d *draftSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	list := d.shown()
	switch s {
	case "esc", "ctrl+c", "ctrl+r":
		m.sheet = nil
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
			text := list[d.cur].Text
			d.all = slices.DeleteFunc(d.all, func(x state.Draft) bool { return x.Text == text })
			go func() { _ = state.RemoveDraft(text) }()
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
		m.wipePrompt()
		m.input, m.back = []rune(strings.TrimSpace(text)), 0
		return
	}
	if len(c.input) > 0 {
		m.keepDraft(c, false)
	}
	c.undo.save(c.input, c.back, false)
	c.input, c.back, c.anchor = c.pastes.unfold(text), 0, 0
	m.paneFocus = true
	c.sel = ""
}

// boxVert moves the Session box's cursor a row up or down through the
// text as it's wrapped, keeping its column. Past the first row it goes to
// the start; past the last, the end. shift extends the selection.
func (m *Model) boxVert(c *hostConn, d int, shift bool) {
	lw := c.box.leadW()
	segs := wrapSegs(c.input, max(20, c.box.w)-4-lw)
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

// keepSent keeps a message as it was sent, for the drafts sheet.
func (m *Model) keepSent(c *hostConn, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	d := state.Draft{Text: strings.TrimSpace(text), At: time.Now(), Agent: c.key, Sent: true}
	if a := m.agentByKey(c.key); a != nil {
		d.Name = a.DisplayName
	}
	go func() { _ = state.AddDraft(d) }()
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

// --- the box kept on disk ---

// A session's box is kept on disk as you write in it (state.BoxDraft), and
// comes back when its box opens again, in this agtop or the next one. It's
// written a moment after typing stops, never per key and never while
// drawing, and at once when agtop quits or is told to end.

// draftDelay is how long typing must pause before the box is written.
const draftDelay = 300 * time.Millisecond

// boxMark is the box as last noted: kept once its session is open, and
// which change a pending write is for.
type boxMark struct {
	on             bool
	text           []rune
	back           int
	pasteN, imageN int
	gen            int
}

// pendingDrafts are boxes changed and not yet written, by agent key.
var (
	pendingDrafts sync.Map // string -> state.BoxDraft
	draftWrite    sync.Mutex
)

type draftSaveMsg struct {
	key string
	gen int
}

// boxDraft is what the box holds now.
func (c *hostConn) boxDraft() state.BoxDraft {
	d := state.BoxDraft{Text: string(c.input), Back: c.back, PasteN: c.pastes.n, ImageN: c.imgs.N, At: time.Now()}
	if len(c.pastes.text) > 0 {
		d.Pastes = make(map[int]string, len(c.pastes.text))
		for k, v := range c.pastes.text {
			d.Pastes[k] = v
		}
	}
	d.Images = c.imgs.clone().Path
	return d
}

// restoreBox puts a kept draft back in the box.
func (c *hostConn) restoreBox(d state.BoxDraft) {
	c.input = []rune(d.Text)
	c.back = max(0, min(d.Back, len(c.input)))
	c.pastes = pastes{n: d.PasteN, text: d.Pastes}
	c.imgs = imageRefs{N: d.ImageN, Path: d.Images}
}

// openDraft brings back the box's draft when its session opens, unless
// something is already in it, and from then on keeps it.
func (m *Model) openDraft(c *hostConn) {
	if len(c.input) == 0 {
		if d, ok := state.ReadBoxDraft(c.key); ok {
			c.restoreBox(d)
			m.paneFocus = true
		}
	}
	c.draft.on = true
	c.markDraft()
}

// markDraft notes the box as it is now.
func (c *hostConn) markDraft() {
	c.draft.text = append(c.draft.text[:0], c.input...)
	c.draft.back, c.draft.pasteN, c.draft.imageN = c.back, c.pastes.n, c.imgs.N
}

// draftChanged says whether the box differs from when it was last noted.
func (c *hostConn) draftChanged() bool {
	return !slices.Equal(c.input, c.draft.text) || c.back != c.draft.back ||
		c.pastes.n != c.draft.pasteN || c.imgs.N != c.draft.imageN
}

// noteDraft runs after each update: a box that changed is kept in memory at
// once and written once typing pauses. A queued message being edited in
// the box isn't a draft; the box is noted again once that's done.
func (m *Model) noteDraft() tea.Cmd {
	c := m.host
	if c == nil || !c.draft.on || c.editQ > 0 || !c.draftChanged() {
		return nil
	}
	c.markDraft()
	c.draft.gen++
	pendingDrafts.Store(c.key, c.boxDraft())
	key, gen := c.key, c.draft.gen
	return tea.Tick(draftDelay, func(time.Time) tea.Msg { return draftSaveMsg{key: key, gen: gen} })
}

// draftSave writes a draft once typing has paused: the box hasn't changed
// since, or its session is no longer open.
func (m *Model) draftSave(msg draftSaveMsg) {
	if c := m.host; c != nil && c.key == msg.key && c.draft.gen != msg.gen {
		return // still typing; a later tick writes it
	}
	go writeDraft(msg.key)
}

// writeDraft writes the pending draft for key, if there is one. Writes
// are one at a time, so the last change is the one that stays.
func writeDraft(key string) {
	draftWrite.Lock()
	defer draftWrite.Unlock()
	if v, ok := pendingDrafts.LoadAndDelete(key); ok {
		_ = state.SaveBoxDraft(key, v.(state.BoxDraft))
	}
}

// FlushDrafts writes every draft not yet written. agtop calls it as it
// quits, and when it is told to end.
func FlushDrafts() {
	pendingDrafts.Range(func(k, _ any) bool {
		writeDraft(k.(string))
		return true
	})
}

// EndOnSignals writes the drafts and ends the program when agtop is told
// to end (SIGTERM) or loses its terminal (SIGHUP), as when the app it's
// embedded in closes the view. The returned func stops watching.
func EndOnSignals(kill func()) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			FlushDrafts()
			kill()
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
