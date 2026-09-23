package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// Pane views for an agtop-mode session, cycled with [ and ].
var paneViews = []string{"conversation", "overview"}

// hostConn is the open connection to the selected agtop-mode session: its
// host client, the conversation built from what the host sends, and how the
// pane is being looked at.
type hostConn struct {
	key    string
	id     string
	client *host.Client
	sess   *convo.Session
	ready  bool // the replay has been drawn at least once

	view    int
	sel     string
	open    map[string]bool
	verbose bool
	scroll  int // rows up from the bottom; 0 follows the latest output

	input     []rune
	back      int
	stopArmed time.Time
	lastSend  time.Time
}

type hostOpenMsg struct {
	key string
	c   *hostConn
	err error
}

type hostLinesMsg struct {
	key    string
	lines  [][]byte
	closed bool
}

var cBright = rgb(240, 236, 228)

// frame is how long a burst of output collects before the pane redraws.
const frame = 33 * time.Millisecond

func openHost(a *fleet.Agent) tea.Cmd {
	key, id := a.Key, a.ID
	return func() tea.Msg {
		cl, err := host.Dial(id)
		if err != nil {
			return hostOpenMsg{key: key, err: err}
		}
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, client: cl, sess: convo.New(), open: map[string]bool{}}}
	}
}

// next waits for output, then gathers whatever else arrives within a frame,
// so a busy session costs one redraw per frame, not one per line.
func (c *hostConn) next() tea.Cmd {
	lines := c.client.Lines
	key := c.key
	return func() tea.Msg {
		first, ok := <-lines
		if !ok {
			return hostLinesMsg{key: key, closed: true}
		}
		batch := [][]byte{first}
		deadline := time.After(frame)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					return hostLinesMsg{key: key, lines: batch, closed: true}
				}
				batch = append(batch, l)
				if len(batch) >= 4096 {
					return hostLinesMsg{key: key, lines: batch}
				}
			case <-deadline:
				return hostLinesMsg{key: key, lines: batch}
			}
		}
	}
}

// syncHost keeps one connection open, to the agtop-mode agent the pane is
// showing, and closes it when the pane moves on.
func (m *Model) syncHost() tea.Cmd {
	a := m.focused()
	_, paneW, _ := m.layout()
	want := a != nil && a.Agtop && a.PID != 0 && paneW > 0 && m.mode == modeList
	if !want {
		if m.host != nil {
			_ = m.host.client.Close()
			m.host = nil
		}
		m.hostOpening = ""
		if a == nil || !a.Agtop {
			m.paneFocus = false
		}
		return nil
	}
	if m.host != nil && m.host.key == a.Key {
		return nil
	}
	if m.hostOpening == a.Key {
		return nil
	}
	if m.host != nil {
		_ = m.host.client.Close()
		m.host = nil
	}
	m.hostOpening = a.Key
	return openHost(a)
}

func (m *Model) onHostOpen(msg hostOpenMsg) tea.Cmd {
	if msg.key != m.hostOpening {
		if msg.c != nil {
			_ = msg.c.client.Close()
		}
		return nil
	}
	m.hostOpening = ""
	if msg.err != nil {
		m.flash("couldn't reach the session: "+msg.err.Error(), true)
		return nil
	}
	m.host = msg.c
	return m.host.next()
}

func (m *Model) onHostLines(msg hostLinesMsg) tea.Cmd {
	c := m.host
	if c == nil || c.key != msg.key {
		return nil
	}
	now := time.Now()
	for _, l := range msg.lines {
		ev, err := host.Decode(l)
		if err != nil || ev == nil {
			continue
		}
		if st, ok := ev.(host.Stamp); ok {
			now = st.At
			continue
		}
		c.sess.Apply(ev, now)
		if e, ok := ev.(host.ErrorEvent); ok {
			m.flash(e.Error, true)
		}
	}
	c.ready = true
	if msg.closed {
		_ = c.client.Close()
		m.host = nil
		return nil
	}
	return c.next()
}

// --- drawing ---

var (
	bgChrome = "\x1b[48;2;30;28;26m" // L3: pane header and dock
	bgTabOn  = "\x1b[48;2;17;16;14m" // the active view opens into the body
)

func onBg(bg, s string, w int) string {
	s = fit(s, w)
	return bg + strings.ReplaceAll(s, reset, reset+bg) + reset
}

func spread(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// agtopPane is the right pane for an agtop-mode agent, or nil when the pane
// shows something else.
func (m *Model) agtopPane(w, h int) []string {
	a := m.focused()
	if a == nil || !a.Agtop {
		return nil
	}
	c := m.host
	if c == nil || c.key != a.Key {
		if a.PID == 0 {
			return m.stoppedPane(a, w, h)
		}
		return []string{"", dim("  connecting to " + oneLine(a.DisplayName) + "…")}
	}
	s := c.sess
	head := m.paneHeader(a, c, w)
	dock := m.paneDock(a, c, w)
	bodyH := max(3, h-len(head)-len(dock))

	o := convo.Options{Width: w, Now: time.Now(), Tick: m.tick, Open: c.open, Verbose: c.verbose,
		Selected: c.sel, Focused: m.paneFocus}
	var body []convo.Line
	if c.view == 1 {
		body = s.Overview(o)
	} else {
		body = s.Render(o)
		if len(body) == 0 {
			body = []convo.Line{{Text: ""}, {Text: dim("  nothing yet · type below to start")}}
		}
	}
	// Bottom-anchored: the latest output sits just above the dock unless
	// you've scrolled up.
	c.scroll = max(0, min(c.scroll, len(body)-bodyH))
	end := len(body) - c.scroll
	start := max(0, end-bodyH)
	out := append([]string{}, head...)
	for _, l := range body[start:end] {
		out = append(out, l.Text)
	}
	if c.scroll > 0 && len(out) > len(head) {
		pill := selBG + " " + paint(cText, fmt.Sprintf("↓ %d more · end follows", c.scroll)) + " " + reset
		out[len(out)-1] = spread("", pill, w)
	}
	for len(out) < h-len(dock) {
		out = append(out, "")
	}
	return append(out, dock...)
}

func (m *Model) paneHeader(a *fleet.Agent, c *hostConn, w int) []string {
	s := c.sess
	info := s.Info
	mark := faint("▍")
	if m.paneFocus {
		mark = paint(cOrange, "▍")
	}
	state := dim("idle")
	switch {
	case len(s.Pending()) > 0:
		state = paint(cYellow+bold, "● needs you")
	case s.Live() != nil:
		state = paint(cOrange, "✻ working "+dur(time.Since(s.Live().Start)))
	case info.Error != "":
		state = paint(cRed, "✗ stopped mid-turn")
	case info.ClaudePID == 0:
		state = dim("◦ idle · resting")
	}
	right := ""
	if s.Context > 0 {
		model := info.Model
		if s.Model != "" {
			model = s.Model
		}
		win := int(claude.ContextWindow(model))
		right = dim("ctx ") + ctxBar(float64(s.Context)/float64(win)*100) + " " + paint(cSub, fmt.Sprintf("%.0f%%", float64(s.Context)/float64(win)*100))
	}
	cost := info.CostUSD
	if cost > 0 {
		right += "   " + paint(cText+bold, money(cost))
	}
	title := faint("SESSION  ")
	if m.paneFocus {
		title = paint(cOrange+bold, "SESSION  ")
	}
	row1 := spread(mark+title+paint(cBright+bold, oneLine(a.DisplayName))+"   "+state, right+" ", w)

	meta := dim(tildify(a.Cwd))
	if a.Branch != "" {
		meta += dim(" · ") + paint(cSub, a.Branch)
	}
	if m := firstNonEmpty(s.Model, info.Model); m != "" {
		meta += dim(" · " + convo.PrettyModel(m))
	}
	if info.Effort != "" {
		meta += dim(" · " + info.Effort)
	}
	if info.PermissionMode != "" {
		mode := info.PermissionMode
		col := cSub
		switch mode {
		case "plan":
			col = cBlue
		case "auto", "acceptEdits", "bypassPermissions":
			col = cOrange
		}
		meta += dim(" · ") + paint(col, mode)
	}
	conn := paint(cGreen, "●") + dim(" connected")
	row2 := spread("  "+meta, conn+" ", w)

	var tabs []string
	for i, v := range paneViews {
		if i == c.view {
			tabs = append(tabs, bgTabOn+paint(cText+bold, " "+v+" ")+reset+bgChrome)
		} else {
			tabs = append(tabs, paint(cSub, " "+v+" "))
		}
	}
	failed := 0
	for _, t := range s.Tools {
		failed += t.Failed
	}
	chips := ""
	if failed > 0 {
		chips = paint(cRed, fmt.Sprintf("✗ %d failed", failed)) + "  "
	}
	if c.verbose {
		chips += paint(cOrange, "ctrl+o all shown") + "  "
	}
	row3 := spread("  "+strings.Join(tabs, " ")+dim("   [ ]"), chips, w)
	return []string{onBg(bgChrome, row1, w), onBg(bgChrome, row2, w), onBg(bgChrome, row3, w),
		paint(rgb(30, 28, 26), strings.Repeat("▀", w))}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// paneDock is the raised area at the bottom of the pane: the current task,
// an approval waiting, the queue, and the input box.
func (m *Model) paneDock(a *fleet.Agent, c *hostConn, w int) []string {
	s := c.sess
	out := []string{paint(rgb(30, 28, 26), strings.Repeat("▄", w))}
	line := func(txt string) { out = append(out, onBg(bgChrome, txt, w)) }

	if now, done, total := s.Current(); total > 0 {
		t := ""
		if now != nil {
			t = paint(cOrange, "■ ") + paint(cText, oneLine(firstNonEmpty(now.Active, now.Subject)))
		} else {
			t = dim("no task in progress")
		}
		line(spread("  "+t+"  "+paint(cSub, fmt.Sprintf("%d/%d", done, total)), "", w))
	}
	if p := s.Pending(); len(p) > 0 {
		st := p[0]
		req := st.Approval
		card := "\x1b[48;2;42;36;25m"
		cl := func(txt string) { out = append(out, onBg(card, txt, w)) }
		count := ""
		if len(p) > 1 {
			count = fmt.Sprintf("1 of %d", len(p))
		}
		cl(spread(paint(cYellow, "▍")+" "+paint(cYellow+bold, "● needs you")+"   "+paint(cText, approvalTitle(req)), paint(cSub, count)+"  ", w))
		for _, l := range approvalBody(req, a.Cwd, w-6) {
			cl(paint(cYellow, "▍") + "     " + l)
		}
		armed := len(c.input) == 0
		k := func(key, label string) string {
			if !armed {
				return dim(key + " " + label)
			}
			return paint(cText+bold, key) + " " + paint(cSub, label)
		}
		cl(spread(paint(cYellow, "▍")+"   "+k("y", "allow once")+"   "+k("a", "always allow")+"   "+k("n", "deny"), dim("clear the prompt to answer")+"  ", w))
	}
	if q := s.Info.Queue; len(q) > 0 {
		line(spread("  "+paint(cSub+bold, fmt.Sprintf("queue %d", len(q)))+dim(" · sends when this turn ends"), "", w))
		for i, item := range q {
			if i >= 3 {
				line(dim(fmt.Sprintf("   … %d more", len(q)-i)))
				break
			}
			line("   " + dim(fmt.Sprint(i+1)) + "  " + paint(cSub, ansi.Truncate(oneLine(item), w-8, "…")))
		}
	}
	top := dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 28, "…"))
	switch {
	case len(s.Pending()) > 0:
		top += dim(" · ") + paint(cYellow, "answer the card first, or type a note")
	case s.Live() != nil:
		top += dim(" · working, so ") + paint(cOrange, "enter queues") + dim(" · ctrl+s sends now")
	default:
		top += dim(" · enter sends")
	}
	b := box{w: w, focused: m.paneFocus, topL: top, text: c.input, cursor: max(0, len(c.input)-c.back),
		lead: paint(cOrange, "❯ "), holder: "a message for this agent", maxRows: 6}
	if mode := s.Info.PermissionMode; mode != "" {
		b.topR = paint(cOrange, mode)
	}
	out = append(out, b.lines()...)
	hint := keysFit(w-4, "enter", "send", "esc · ←", "back to the list", "↑↓", "pick a step", "[ ]", "views", "ctrl+o", "show all", "ctrl+x", "stop turn")
	if c.sel != "" {
		hint = keysFit(w-4, "enter · space", "open or close", "↑↓", "pick a step", "esc", "done picking", "ctrl+o", "show all")
	}
	if !m.paneFocus {
		hint = keysFit(w-4, "enter · →", "type here", "ctrl+n", "next needing you")
		b.holder = "enter or → to talk to this agent"
		out = append(out[:len(out)-len(b.lines())], b.lines()...)
	}
	return append(out, "  "+hint)
}

func approvalTitle(r *headless.PermissionRequest) string {
	switch r.Tool {
	case "Bash":
		return "run a command"
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "change a file"
	case "WebFetch", "WebSearch":
		return "go online"
	case "AskUserQuestion":
		return "answer a question"
	}
	return "use " + r.Tool
}

func approvalBody(r *headless.PermissionRequest, cwd string, w int) []string {
	in := map[string]any{}
	_ = jsonUnmarshal(r.Input, &in)
	str := func(k string) string { v, _ := in[k].(string); return v }
	rel := func(p string) string {
		for _, base := range []string{cwd, "/private" + cwd} {
			if base != "" && strings.HasPrefix(p, base+"/") {
				return strings.TrimPrefix(p, base+"/")
			}
		}
		return tildify(p)
	}
	var out []string
	switch {
	case str("command") != "":
		lines := strings.Split(str("command"), "\n")
		out = append(out, paint(cBright, "$ ")+paint(cText, ansi.Truncate(lines[0], w-4, "…")))
		if len(lines) > 1 {
			out = append(out, dim(fmt.Sprintf("  +%d more lines", len(lines)-1)))
		}
	case str("file_path") != "":
		out = append(out, paint(cText, rel(str("file_path"))))
		// An edit shows what it changes, so you can judge it here.
		add := func(prefix, col, text string, limit int) {
			ls := strings.Split(strings.TrimRight(text, "\n"), "\n")
			for i, l := range ls {
				if i >= limit {
					out = append(out, dim(fmt.Sprintf("  … %d more", len(ls)-i)))
					return
				}
				out = append(out, paint(col, prefix+" ")+paint(cText, ansi.Truncate(strings.ReplaceAll(l, "\t", "  "), w-4, "…")))
			}
		}
		if old := str("old_string"); old != "" {
			add("−", cRed, old, 4)
			add("+", cGreen, str("new_string"), 4)
		} else if c := str("content"); c != "" {
			add("+", cGreen, c, 4)
		}
	case str("url") != "":
		out = append(out, paint(cText, str("url")))
	case str("query") != "":
		out = append(out, paint(cText, str("query")))
	}
	why := oneLine(ansi.Strip(firstNonEmpty(r.Reason, r.Description)))
	if why != "" && !strings.Contains(strings.Join(out, " "), why) && !strings.HasSuffix(str("file_path"), why) {
		out = append(out, dim(ansi.Truncate(why, w, "…")))
	}
	return out
}

func (m *Model) stoppedPane(a *fleet.Agent, w, h int) []string {
	out := []string{
		onBg(bgChrome, spread(faint("▍")+paint(cBright+bold, oneLine(a.DisplayName))+"   "+dim("⏹ stopped"), "", w), w),
		onBg(bgChrome, "  "+dim(tildify(a.Cwd)), w),
		"",
		"  " + dim("This session's host isn't running. Its conversation is saved."),
		"  " + dim("Enter resumes it here, headless, where you left off."),
	}
	return out
}

// --- keys ---

// paneKey handles a key while an agtop-mode session's pane has focus. The
// prompt is always live, so letters type; actions are chords, arrows on an
// empty prompt, and the approval card's letters when the prompt is empty.
func (m *Model) paneKey(k tea.KeyPressMsg, s string) tea.Cmd {
	c := m.host
	if c == nil {
		m.paneFocus = false
		return nil
	}
	empty := len(c.input) == 0
	pending := c.sess.Pending()
	if empty && len(pending) > 0 {
		req := pending[0].Approval
		switch s {
		case "y", "enter":
			return m.answerHost(c, req, true, false)
		case "a":
			return m.answerHost(c, req, true, true)
		case "n":
			return m.answerHost(c, req, false, false)
		}
	}
	switch s {
	case "esc":
		switch {
		case !empty:
			c.input, c.back = c.input[:0], 0
		case c.sel != "":
			c.sel = "" // first esc drops the step selection
		default:
			m.leavePane()
		}
		return nil
	case "ctrl+c":
		if !empty {
			c.input, c.back = c.input[:0], 0
			return nil
		}
	case "enter":
		if empty {
			if c.sel != "" {
				c.open[c.sel] = !m.isOpen(c, c.sel)
			}
			return nil
		}
		return m.sendPane(c, false)
	case "ctrl+s":
		if !empty {
			return m.sendPane(c, true)
		}
	case "up", "down":
		if empty {
			m.moveSel(c, map[string]int{"up": -1, "down": 1}[s])
			return nil
		}
	case "space":
		if empty && c.sel != "" {
			c.open[c.sel] = !m.isOpen(c, c.sel)
			return nil
		}
	case "left":
		// ← on an empty box always goes back to the list, like esc.
		if empty {
			m.leavePane()
			return nil
		}
	case "[", "]":
		if empty {
			d := 1
			if s == "[" {
				d = -1
			}
			c.view = (c.view + d + len(paneViews)) % len(paneViews)
			c.scroll = 0
			return nil
		}
	case "ctrl+o":
		c.verbose = !c.verbose
		return nil
	case "pgup":
		c.scroll += 10
		return nil
	case "pgdown":
		c.scroll = max(0, c.scroll-10)
		return nil
	case "end":
		if empty {
			c.scroll = 0
			return nil
		}
	case "ctrl+x":
		if c.sess.Live() != nil && time.Since(c.stopArmed) > 2*time.Second {
			c.stopArmed = time.Now()
			m.flash("stopping the turn · ctrl+x again stops the session", false)
			return hostCmd(func() error { return c.client.Interrupt() })
		}
		m.confirm = &confirmation{
			question: "Stop " + m.focused().DisplayName + "?",
			detail:   "ends Claude Code and its host · the conversation is kept and enter resumes it",
			onYes:    func() tea.Cmd { return hostCmd(func() error { return c.client.Stop() }) },
		}
		return nil
	case "shift+tab":
		modes := []string{"default", "acceptEdits", "plan", "auto"}
		cur := c.sess.Info.PermissionMode
		next := modes[0]
		for i, md := range modes {
			if md == cur {
				next = modes[(i+1)%len(modes)]
			}
		}
		m.flash("permission mode: "+next, false)
		return hostCmd(func() error { return c.client.SetPermissionMode(next) })
	case "ctrl+n":
		m.paneFocus = false
		return m.nextNeedingYou()
	case "alt+left", "alt+right":
		if empty && m.listW > 0 {
			d := 2
			if s == "alt+left" {
				d = -2
			}
			m.setSideWidth(m.sideWidth() + d)
			return nil
		}
	}
	buf, pos, _ := edit(c.input, max(0, len(c.input)-c.back), k, s)
	c.input, c.back = buf, len(buf)-pos
	return nil
}

func hostCmd(f func() error) tea.Cmd {
	return func() tea.Msg {
		if err := f(); err != nil {
			return doneMsg{err: err}
		}
		return nil
	}
}

func (m *Model) answerHost(c *hostConn, req *headless.PermissionRequest, allow, always bool) tea.Cmd {
	id := req.ID
	if allow {
		return hostCmd(func() error { return c.client.Allow(id, nil, always) })
	}
	return hostCmd(func() error { return c.client.Deny(id, "", false) })
}

// sendPane sends the prompt: now, or queued when the agent is busy (the
// host decides). A trailing backslash continues onto a new line instead.
func (m *Model) sendPane(c *hostConn, now bool) tea.Cmd {
	text := string(c.input)
	if strings.HasSuffix(text, "\\") && !now {
		c.input = append(c.input[:len(c.input)-1], '\n')
		c.back = 0
		return nil
	}
	text = strings.TrimSpace(text)
	c.input, c.back = c.input[:0], 0
	c.scroll = 0
	c.lastSend = time.Now()
	if a := m.focused(); a != nil {
		m.markSeen(a)
	}
	if now {
		return hostCmd(func() error { return c.client.SendNow(text) })
	}
	return hostCmd(func() error { return c.client.Send(text) })
}

func (m *Model) isOpen(c *hostConn, ref string) bool {
	if v, ok := c.open[ref]; ok {
		return v
	}
	// What the renderer opens by default: recent turns and failures.
	for _, l := range c.sess.Render(convo.Options{Width: max(40, m.w/2), Now: time.Now(), Open: c.open}) {
		if l.Ref == ref {
			return !strings.Contains(ansi.Strip(l.Text), "▸")
		}
	}
	return false
}

// moveSel moves the selection over rows you can act on: turns and steps.
func (m *Model) moveSel(c *hostConn, d int) {
	o := convo.Options{Width: max(40, m.w/2), Now: time.Now(), Open: c.open, Verbose: c.verbose}
	var refs []string
	seen := map[string]bool{}
	for _, l := range c.sess.Render(o) {
		if l.Ref != "" && !seen[l.Ref] {
			seen[l.Ref] = true
			refs = append(refs, l.Ref)
		}
	}
	if len(refs) == 0 {
		return
	}
	i := len(refs)
	for j, r := range refs {
		if r == c.sel {
			i = j
		}
	}
	i = max(0, min(len(refs)-1, i+d))
	c.sel = refs[i]
	c.scroll = 0
}

// leavePane gives the keys back to the list. On a narrow screen, where the
// conversation filled it, the list comes back too.
func (m *Model) leavePane() {
	m.paneFocus = false
	if m.listW == 0 {
		m.preview, m.full = false, false
	}
}

// focusPane moves keys into the selected agtop-mode agent's pane.
func (m *Model) focusPane(a *fleet.Agent) tea.Cmd {
	if a == nil || !a.Agtop {
		return nil
	}
	if a.PID == 0 {
		return m.resume(a)
	}
	m.preview, m.paneFocus = true, true
	return m.loadPreview()
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// resume brings a stopped agtop-mode session back: a new host, the same
// conversation, the model, effort and mode it last had.
func (m *Model) resume(a *fleet.Agent) tea.Cmd {
	cfg, err := host.ReadConfig(a.ID)
	if err != nil {
		cfg = host.Config{ID: a.ID, SessionID: a.SessionID, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName}
	}
	cfg.Resume, cfg.Prompt = true, ""
	m.flash("resuming "+a.DisplayName+"…", false)
	m.preview, m.paneFocus = true, true
	return func() tea.Msg {
		if _, err := host.Spawn(cfg); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "resumed " + a.DisplayName}
	}
}

// startHosted starts a new agtop-mode session: agtop's own host running
// Claude Code headless, with the model, effort and mode from Settings.
func (m *Model) startHosted(text, dir string) tea.Cmd {
	d := m.store.Config.Dispatch
	cfg := host.Config{
		Account: m.store.Config.ActiveAccount(), Cwd: dir, Prompt: text, Name: sessionName(text),
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission,
	}
	m.flash("starting a new session…", false)
	return func() tea.Msg {
		c, err := host.Spawn(cfg)
		if err != nil {
			return doneMsg{err: err}
		}
		return hostStartedMsg{id: c.ID, name: cfg.Name, acct: cfg.Account.Name}
	}
}

type hostStartedMsg struct{ id, name, acct string }

// sessionName is the first few words of the task, until the session names
// itself.
func sessionName(text string) string {
	words := strings.Fields(text)
	if len(words) > 6 {
		words = words[:6]
	}
	n := strings.Join(words, " ")
	if r := []rune(n); len(r) > 48 {
		n = string(r[:47]) + "…"
	}
	return n
}

// sendHosted sends a message to an agtop-mode agent from the main prompt,
// through its host.
func sendHosted(a *fleet.Agent, text string) tea.Cmd {
	id := a.ID
	return func() tea.Msg {
		c, err := host.Dial(id)
		if err != nil {
			return doneMsg{err: err}
		}
		defer c.Close()
		if err := c.Send(text); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "sent to " + a.DisplayName}
	}
}

// moveToAgtop switches a Claude Code session to agtop mode: the daemon's
// copy stops (the conversation is kept) and the same conversation resumes
// under agtop's own host, which runs it headless from then on.
func (m *Model) moveToAgtop(a *fleet.Agent) tea.Cmd {
	switch {
	case a.Agtop:
		m.flash(a.DisplayName+" already runs in agtop mode", false)
		return nil
	case a.Interactive:
		m.flash(a.DisplayName+" is open in a terminal; close it there first", true)
		return nil
	case a.SessionID == "":
		m.flash("can't find "+a.DisplayName+"'s conversation to resume", true)
		return nil
	}
	d := m.store.Config.Dispatch
	cfg := host.Config{
		SessionID: a.SessionID, Resume: true, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName,
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission,
	}
	old := a.Key
	m.flash("moving "+a.DisplayName+" to agtop mode…", false)
	return func() tea.Msg {
		if a.PID != 0 || a.Live() {
			if err := actions.Stop(a.Acct, a.ID, a.PID); err != nil {
				return doneMsg{err: fmt.Errorf("couldn't stop the Claude Code copy: %w", err)}
			}
		}
		c, err := host.Spawn(cfg)
		if err != nil {
			return doneMsg{err: err}
		}
		return movedToAgtopMsg{from: old, started: hostStartedMsg{id: c.ID, name: cfg.Name, acct: cfg.Account.Name}}
	}
}

type movedToAgtopMsg struct {
	from    string
	started hostStartedMsg
}
