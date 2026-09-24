package ui

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/host"
)

// --- /btw: side questions in a floating panel ---

// A btw thread is a side conversation with Claude about the agent's work:
// answered from the conversation so far (Claude Code's side_question),
// without adding to it or stopping the turn under way. It floats over the
// Session's top right, so the conversation and the message box stay in
// use while an answer comes; ctrl+b (for btw) moves the keys between the
// two. Pulled
// out (ctrl+f), it becomes an agent of its own: a fork of the conversation
// whose first message carries the side thread on.
type btwThread struct {
	qa      []btwQA
	input   []rune
	pos     int
	waiting time.Time // when the question out was asked; zero when none
	asked   *hostConn // the connection it was asked on: its answer comes there
	err     string
	scroll  int    // rows up from the latest
	focused bool   // it has the keys, not the message box
	at      [4]int // where it was drawn: x, y, w, h on screen
}

type btwQA struct{ Question, Response string }

// btwFor is the agent's side thread, if it has one open.
func (m *Model) btwFor(key string) *btwThread { return m.btws[key] }

// openBtw opens the agent's side thread (or the one already open), with
// the keys, and asks q when there is one.
func (m *Model) openBtw(c *hostConn, q string) tea.Cmd {
	if m.btws == nil {
		m.btws = map[string]*btwThread{}
	}
	t := m.btws[c.key]
	if t == nil {
		t = &btwThread{}
		m.btws[c.key] = t
	}
	t.focused, m.paneFocus = true, true
	if q = strings.TrimSpace(q); q == "" {
		return nil
	}
	return t.ask(m, c, q)
}

func (t *btwThread) ask(m *Model, c *hostConn, q string) tea.Cmd {
	var history []map[string]string
	for _, x := range t.qa {
		if x.Response != "" {
			history = append(history, map[string]string{"question": x.Question, "response": x.Response})
		}
	}
	if history == nil {
		history = []map[string]string{}
	}
	cmd := m.askClaude(c, map[string]any{"subtype": "side_question", "question": q, "history": history}, func(m *Model, r host.Reply) tea.Cmd {
		t.waiting = time.Time{}
		var a struct {
			Response string `json:"response"`
		}
		switch {
		case r.Error != "":
			t.err = r.Error
		case json.Unmarshal(r.Body, &a) != nil || strings.TrimSpace(a.Response) == "":
			t.err = "no answer came back: ask again, or ask in the conversation"
		default:
			t.qa[len(t.qa)-1].Response = a.Response
		}
		return nil
	})
	if cmd == nil {
		return nil // askClaude said why
	}
	t.qa = append(t.qa, btwQA{Question: q})
	t.waiting, t.asked, t.err, t.scroll = time.Now(), c, "", 0
	return cmd
}

// btwKey handles keys meant for the side thread: all of them while it has
// the keys, and ctrl+b, which moves them there, while it hasn't (bar in a
// memory file, where it's bold).
func (m *Model) btwKey(c *hostConn, k tea.KeyPressMsg, s string) (tea.Cmd, bool) {
	t := m.btwFor(c.key)
	if t == nil || !t.focused {
		switch {
		case s == "ctrl+b" && !c.memEdit:
			return m.openBtw(c, ""), true
		case s == "ctrl+d" && t != nil && len(c.input) == 0:
			delete(m.btws, c.key) // tucked away, it closes from the chat too
			return nil, true
		}
		return nil, false
	}
	switch s {
	case "esc", "ctrl+b":
		// Back to the conversation. A thread with something in it stays,
		// tucked away; one never asked anything goes.
		t.focused = false
		if len(t.qa) == 0 {
			delete(m.btws, c.key)
		}
	case "ctrl+d":
		delete(m.btws, c.key)
	case "enter":
		q := strings.TrimSpace(string(t.input))
		if q == "" {
			return nil, true
		}
		if !t.waiting.IsZero() {
			m.flash("one question at a time: the last one's still being answered", true)
			return nil, true
		}
		t.input, t.pos = nil, 0
		return t.ask(m, c, q), true
	case "ctrl+f":
		return m.forkBtw(c, t), true
	case "ctrl+s":
		// The last answer into the message box, to send after all.
		if n := len(t.qa); n > 0 && t.qa[n-1].Response != "" {
			last := t.qa[n-1]
			c.input, c.back = []rune("About my side question (\""+last.Question+"\"), you said:\n\n"+last.Response+"\n\n"), 0
			t.focused = false
		}
	case "up", "pgup":
		t.scroll += map[string]int{"up": 1, "pgup": 8}[s]
	case "down", "pgdown":
		t.scroll = max(0, t.scroll-map[string]int{"down": 1, "pgdown": 8}[s])
	default:
		if in, pos, ok := edit(t.input, t.pos, k, s); ok {
			t.input, t.pos = in, pos
			return nil, true
		}
		return nil, false // ctrl+x and the like still reach the Session
	}
	return nil, true
}

// forkBtw pulls the side thread out into an agent of its own: /fork of the
// whole conversation, its first message the side thread so far.
func (m *Model) forkBtw(c *hostConn, t *btwThread) tea.Cmd {
	a := m.agentByKey(c.key)
	if a == nil || len(t.qa) == 0 {
		return nil
	}
	name := oneLine(t.qa[0].Question)
	if r := []rune(name); len(r) > 40 {
		name = string(r[:39]) + "…"
	}
	m.openFork(c, a, "btw: "+name)
	f, ok := m.sheet.(*forkSheet)
	if !ok {
		return nil
	}
	var b strings.Builder
	b.WriteString("While you were working I asked some side questions. Let's carry that thread on here.\n")
	for _, x := range t.qa {
		b.WriteString("\nMe: " + x.Question + "\n")
		if x.Response != "" {
			b.WriteString("You: " + x.Response + "\n")
		}
	}
	f.first, f.row = []rune(b.String()), fkFirst
	f.firstPos = len(f.first)
	t.focused = false
	return nil
}

// clickBtw gives the side thread the keys when it's clicked.
func (m *Model) clickBtw(c *hostConn, x, y int) bool {
	t := m.btwFor(c.key)
	if t == nil || t.at[2] == 0 || x < t.at[0] || x >= t.at[0]+t.at[2] || y < t.at[1] || y >= t.at[1]+t.at[3] {
		return false
	}
	t.focused, m.paneFocus = true, true
	return true
}

var bgBtw = "\x1b[48;2;36;33;30m" // a raised surface: it floats over the conversation

// btwOverlay draws the agent's side thread over the pane's rows from top
// down, at its right; w is the pane's width and room how many rows it may
// take.
func (m *Model) btwOverlay(c *hostConn, out []string, top, room, w int) {
	t := m.btwFor(c.key)
	if t == nil {
		return
	}
	if !t.focused && len(t.qa) == 0 {
		delete(m.btws, c.key) // nothing asked, and you've gone back to the chat
		return
	}
	if !t.waiting.IsZero() && t.asked != c {
		// Asked on a connection since closed (you went to another agent):
		// its answer went with it.
		t.waiting, t.err = time.Time{}, "the answer was lost when you left this agent: ask again"
	}
	pw := min(w-2, max(44, w*45/100))
	if w < 70 {
		pw = w - 2
	}
	maxH := room - 1
	if !t.focused {
		maxH = min(room-1, 11)
	}
	panel := t.lines(c, pw, maxH, m.paneFocus)
	if len(panel) == 0 || room < 5 {
		return
	}
	leftW := w - pw - 1
	for i, p := range panel {
		r := top + i
		if r >= len(out) {
			break
		}
		left := fit(ansi.Truncate(out[r], leftW, ""), leftW)
		out[r] = left + reset + " " + p
	}
	t.at = [4]int{m.paneX() + leftW + 1, m.paneTop + top, pw, len(panel)}
}

// lines draws the panel pw wide and at most maxH tall: a title, the
// thread (the latest at the bottom), the question box and its keys.
func (t *btwThread) lines(c *hostConn, pw, maxH int, paneFocused bool) []string {
	on := t.focused && paneFocused
	edge := faint("▏")
	if on {
		edge = paint(cOrange, "▍")
	}
	iw := pw - 3 // inside the edge and a space either side
	row := func(s string) string { return onBg(bgBtw, edge+" "+s, pw) }

	var thread []string
	qa := t.qa
	if !t.focused && len(qa) > 1 {
		qa = qa[len(qa)-1:] // tucked away, it shows the latest only
	}
	for i, x := range qa {
		if i > 0 {
			thread = append(thread, "")
		}
		for j, l := range wrap(x.Question, iw-2) {
			lead := paint(cOrange, "❯ ")
			if j > 0 {
				lead = "  "
			}
			thread = append(thread, lead+paint(cText+bold, l))
		}
		last := i == len(qa)-1
		switch {
		case x.Response != "":
			for _, l := range c.sess.Answer(x.Response, iw) {
				thread = append(thread, l.Text)
			}
		case last && !t.waiting.IsZero():
			thread = append(thread, dim("  thinking · "+dur(time.Since(t.waiting))))
		case last && t.err != "":
			for _, l := range wrap(t.err, iw-2) {
				thread = append(thread, "  "+paint(cRed, l))
			}
		}
	}
	if len(t.qa) == 0 {
		thread = append(thread, dim("ask about what's going on: it doesn't go into the conversation"))
	}

	head := paint(cOrange+bold, "btw") + dim("  side question · not in the conversation")
	var foot []string
	if t.focused {
		foot = append(foot, paint(cOrange, "❯ ")+textField(t.input, t.pos, on, "ask…", iw-2))
		foot = append(foot, keysFit(iw, "enter", "ask", "↑↓", "scroll", "esc", "back to the chat"))
		if len(t.qa) > 0 {
			foot = append(foot, keysFit(iw, "ctrl+f", "its own chat", "ctrl+s", "into the box", "ctrl+d", "close"))
		} else {
			foot = append(foot, keysFit(iw, "ctrl+d", "close"))
		}
	} else {
		foot = append(foot, keysFit(iw, "ctrl+b", "ask more", "ctrl+d", "close"))
	}

	room := max(1, maxH-2-len(foot))
	switch {
	case len(thread) <= room:
	case !t.focused:
		// Tucked away: the latest question and the start of its answer.
		thread = append(thread[:room-1], dim("  … ctrl+b reads the rest"))
	default:
		// Anchored to its latest line; ↑ scrolls back.
		t.scroll = max(0, min(t.scroll, len(thread)-room))
		end := len(thread) - t.scroll
		thread = thread[max(0, end-room):end]
	}
	out := []string{row(head), row("")}
	for _, l := range thread {
		out = append(out, row(l))
	}
	for _, l := range foot {
		out = append(out, row(l))
	}
	return out
}
