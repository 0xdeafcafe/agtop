package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// Pane views, cycled with [ and ]: every session has a conversation and an
// overview; a running Claude Code session also has its live screen.
var paneViews = []string{"conversation", "overview", "changes"}

func (m *Model) views(c *hostConn) []string {
	v := append([]string{}, paneViews...)
	if len(c.sess.Tasks) > 0 {
		v = append(v, "tasks")
	}
	if c.client != nil || canQueue(m.agentByKey(c.key)) {
		v = append(v, "queue")
	}
	if len(c.subs) > 0 {
		v = append(v, "subagents")
	}
	if len(c.artifactsOf()) > 0 {
		v = append(v, "artifacts")
	}
	if c.client == nil {
		if a := m.focused(); a != nil && liveCapable(a) {
			v = append(v, "screen")
		}
	}
	return v
}

// refreshSubs looks for new subagent runs and follows the one opened.
func (m *Model) refreshSubs() {
	c := m.host
	if c == nil || c.path == "" {
		return
	}
	c.subs = c.subList.List(c.path)
	if c.subTails == nil {
		c.subTails = map[string]*convo.Tail{}
	}
	for _, sa := range c.subs {
		t := c.subTails[sa.ID]
		if t == nil {
			t = convo.SubagentTail(sa.Path)
			c.subTails[sa.ID] = t
		}
		_, _ = t.Read()
	}
	m.readSub()
}

// readSub takes in what the opened subagent has written, and closes its
// last turn once its step has finished.
func (m *Model) readSub() {
	c := m.host
	if c == nil || c.subTail == nil {
		return
	}
	_, _ = c.subTail.Read()
	if st := c.sess.Step(c.subToolUse()); st != nil && st.Status != convo.Running {
		if live := c.subTail.Sess.Live(); live != nil {
			c.subTail.Sess.Apply(headless.Result{Subtype: "success"}, time.Now())
		}
	}
}

// growMsg says a transcript the pane follows has grown.
type growMsg struct{ key string }

// tailPoll is how often the followed transcripts are checked for growth:
// one stat each, so what Claude Code writes shows within a frame or two
// rather than on the next second's tick.
const tailPoll = 25 * time.Millisecond

type watched struct {
	path string
	size int64
}

// syncWatch keeps a watch on the transcripts the pane follows: a Claude
// Code session's own, and the subagent opened.
func (m *Model) syncWatch() tea.Cmd {
	c := m.host
	if c == nil {
		return nil
	}
	var want []watched
	if c.tail != nil {
		want = append(want, watched{c.tail.Path, c.tail.Size()})
	}
	if c.subTail != nil {
		want = append(want, watched{c.subTail.Path, c.subTail.Size()})
	}
	if c.stopWatch != nil && slices.Equal(want, c.watching) {
		return nil
	}
	c.unwatch()
	if len(want) == 0 {
		return nil
	}
	stop, key := make(chan struct{}), c.key
	c.watching, c.stopWatch = want, stop
	return func() tea.Msg {
		t := time.NewTicker(tailPoll)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return nil
			case <-t.C:
			}
			for _, w := range want {
				if st, err := os.Stat(w.path); err == nil && st.Size() != w.size {
					return growMsg{key}
				}
			}
		}
	}
}

func (c *hostConn) unwatch() {
	if c.stopWatch != nil {
		close(c.stopWatch)
	}
	c.stopWatch, c.watching = nil, nil
}

func (m *Model) onGrow(msg growMsg) {
	if c := m.host; c != nil && c.key == msg.key {
		c.unwatch() // that watch is over; the next update starts another
		m.followTail()
		m.readSub()
	}
}

// subState is how a subagent run stands: the status its task-finished
// notice gave (or failed), and whether it is still working.
func (c *hostConn) subState(sa convo.Subagent) (status string, live bool) {
	t := convo.New()
	if tl := c.subTails[sa.ID]; tl != nil {
		t = tl.Sess
	}
	status = c.sess.TaskStatus[sa.ID]
	st := c.sess.Step(sa.ToolUseID)
	if st != nil && st.Status == convo.Failed && status == "" {
		status = "failed"
	}
	// No word that it finished, and it wrote recently: still working.
	live = status == "" && !t.Last.IsZero() && time.Since(t.Last) < 90*time.Second
	if st != nil && st.Status == convo.Running {
		live = true
	}
	return status, live
}

// runningSubs are the subagent runs still working, newest first.
func (c *hostConn) runningSubs() []convo.Subagent {
	var out []convo.Subagent
	for i := len(c.subs) - 1; i >= 0; i-- {
		if _, live := c.subState(c.subs[i]); live {
			out = append(out, c.subs[i])
		}
	}
	return out
}

// cycleSub steps through the conversation and its subagent runs (running
// ones first, then the rest, newest first), each shown in place.
func (m *Model) cycleSub(c *hostConn, dir int) {
	order := c.runningSubs()
	seen := map[string]bool{}
	for _, sa := range order {
		seen[sa.ID] = true
	}
	for i := len(c.subs) - 1; i >= 0; i-- {
		if !seen[c.subs[i].ID] {
			order = append(order, c.subs[i])
		}
	}
	if len(order) == 0 {
		return
	}
	// Position 0 is the main conversation.
	cur := 0
	if m.viewName(c) == "subagents" && c.subOpen != "" {
		for i, sa := range order {
			if sa.ID == c.subOpen {
				cur = i + 1
			}
		}
	}
	next := (cur + dir + len(order) + 1) % (len(order) + 1)
	if next == 0 {
		c.subOpen, c.subTail = "", nil
		c.view, c.sel, c.scroll = 0, "", 0
		return
	}
	for i, v := range m.views(c) {
		if v == "subagents" {
			c.view = i
		}
	}
	m.openSub(c, order[next-1].ID)
}

func (c *hostConn) subToolUse() string {
	for _, sa := range c.subs {
		if sa.ID == c.subOpen {
			return sa.ToolUseID
		}
	}
	return ""
}

// openSub drills into one subagent's own conversation.
func (m *Model) openSub(c *hostConn, id string) {
	for _, sa := range c.subs {
		if sa.ID == id {
			t := convo.SubagentTail(sa.Path)
			_, _ = t.Read()
			c.subTail, c.subOpen, c.subSel = t, id, ""
			c.sel, c.scroll = "", 0
			m.refreshSubs()
			return
		}
	}
}

// subagentLines is the subagents view: one row per run, or the opened
// run's own conversation under a breadcrumb.
func (m *Model) subagentLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	if c.subOpen != "" && c.subTail != nil {
		var sa convo.Subagent
		for _, x := range c.subs {
			if x.ID == c.subOpen {
				sa = x
			}
		}
		crumb := "  " + paint(cSub, "‹ subagents") + dim("  ·  ") + paint(cText+bold, sa.Type) + "  " + dim(oneLine(sa.Description)) + dim("   ← back · alt+↑↓ next run")
		lines := []convo.Line{{Text: fit(crumb, w)}, {Text: ""}}
		so := o
		so.Selected = c.sel
		return append(lines, c.subTail.Sess.Render(so)...)
	}
	// Wide enough: the runs on the left, the picked one's conversation
	// beside them, following as it works.
	if w >= 150 && len(c.subs) > 0 {
		lw := min(72, w*2/5)
		lo := o
		lo.Width = lw
		list := m.subagentList(c, lo)
		id := strings.TrimPrefix(c.sel, "sub:")
		if !strings.HasPrefix(c.sel, "sub:") {
			id = c.subs[len(c.subs)-1].ID
			if run := c.runningSubs(); len(run) > 0 {
				id = run[0].ID
			}
		}
		var detail []convo.Line
		if t := c.subTails[id]; t != nil {
			do := o
			do.Width, do.Selected, do.Focused = w-lw-3, "", false
			detail = t.Sess.Render(do)
		}
		// Exactly a screen's worth: the list scrolls to keep the picked run
		// in view, and the conversation shows its latest.
		h := max(8, m.paneH()-6)
		at := 0
		for i, l := range list {
			if l.Ref == c.sel {
				at = i
				break
			}
		}
		from := max(0, min(at-h/2, len(list)-h))
		list = list[from:min(len(list), from+h)]
		detail = detail[max(0, len(detail)-h):]
		out := make([]convo.Line, 0, h)
		for i := range h {
			var l, r convo.Line
			if i < len(list) {
				l = list[i]
			}
			if i < len(detail) {
				r = detail[i]
			}
			out = append(out, convo.Line{Text: fit(l.Text, lw) + " " + faint("│") + " " + r.Text, Ref: l.Ref})
		}
		return out
	}
	return m.subagentList(c, o)
}

// subagentList is the runs, one two-line row each, newest first.
func (m *Model) subagentList(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	running := 0
	type row struct {
		sa     convo.Subagent
		t      *convo.Session
		status string
		live   bool
	}
	var rows []row
	for i := len(c.subs) - 1; i >= 0; i-- { // newest first
		sa := c.subs[i]
		r := row{sa: sa, t: convo.New()}
		if t := c.subTails[sa.ID]; t != nil {
			r.t = t.Sess
		}
		r.status, r.live = c.subState(sa)
		if r.live {
			running++
		}
		rows = append(rows, r)
	}
	head := fmt.Sprintf("%d subagent runs", len(c.subs))
	if running > 0 {
		head = paint(cOrange, fmt.Sprintf("%d running", running)) + dim(" · "+head)
	} else {
		head = dim(head)
	}
	lines := []convo.Line{{Text: fit("  "+head+dim(" · enter opens one"), w)}, {Text: ""}}
	for i, r := range rows {
		sa := r.sa
		now := time.Now()
		end := r.t.Last
		if r.live {
			end = now
		}
		took := ""
		if !r.t.First.IsZero() && !end.IsZero() {
			took = dur(end.Sub(r.t.First).Round(time.Second))
		}
		mark, state := paint(cGreen, "✓"), dim("done")
		switch {
		case r.live:
			mark, state = paint(cOrange, spinner[(m.tick+i)%len(spinner)]), paint(cOrange, "running")
		case r.status == "killed" || r.status == "stopped":
			mark, state = dim("⏹"), dim(r.status)
		case r.status == "failed":
			mark, state = paint(cRed, "✗"), paint(cRed, "failed")
		case r.status != "":
			state = dim(r.status)
		}
		model := sa.Model
		if n := len(r.t.Requests); n > 0 && r.t.Requests[n-1].Model != "" {
			model = convo.PrettyModel(r.t.Requests[n-1].Model)
		}
		ref := "sub:" + sa.ID
		left := "  " + mark + " " + paint(cBlue, "⇉") + " " + paint(cText+bold, sa.Type) + "  " + paint(cSub, oneLine(sa.Description))
		right := state + "   " + dim(took) + "  "
		top := spread(left, right, w)
		tot := r.t.Totals(now)
		facts := []string{fmt.Sprintf("%d steps", tot.ToolCalls)}
		if tot.Requests > 0 {
			facts = append(facts, "in "+convo.Tokens(tot.In+tot.CacheRead+tot.CacheOut), "out "+convo.Tokens(tot.Out))
			if c := r.t.Cost(); c > 0 {
				facts = append(facts, money(c))
			}
		}
		if model != "" {
			facts = append(facts, model)
		}
		second := "      " + dim(strings.Join(facts, " · "))
		if lw := r.t.LastWords(); lw != "" {
			second += dim("  ·  ") + faint(ansi.Truncate(lw, max(10, w-cellw.String(second)-8), "…"))
		}
		if ref == o.Selected {
			bar := faint("▍")
			if o.Focused {
				bar = paint(cOrange, "▍")
			}
			lines = append(lines,
				convo.Line{Text: selBG + strings.ReplaceAll(bar+fit(top, w)[1:], reset, reset+selBG) + reset, Ref: ref},
				convo.Line{Text: selBG + strings.ReplaceAll(fit(second, w), reset, reset+selBG) + reset, Ref: ref})
		} else {
			lines = append(lines, convo.Line{Text: fit(top, w), Ref: ref}, convo.Line{Text: fit(second, w), Ref: ref})
		}
	}
	return lines
}

func (m *Model) viewName(c *hostConn) string {
	v := m.views(c)
	return v[c.view%len(v)]
}

// hostConn is the open connection to the selected agtop-mode session: its
// host client, the conversation built from what the host sends, and how the
// pane is being looked at.
type hostConn struct {
	key    string
	id     string
	client *host.Client // an agtop session's host; nil when read from a transcript
	tail   *convo.Tail  // a Claude Code session's transcript, followed as it grows
	sess   *convo.Session
	ready  bool // the replay has been drawn at least once

	view    int
	sel     string
	open    map[string]bool
	verbose bool
	scroll  int // rows up from the bottom; 0 follows the latest output

	input    []rune
	back     int
	anchor   int      // selection start + 1; 0 when nothing is selected
	images   []string // image files attached to the next message
	box      box      // the message box as last drawn, and where
	boxIdx   int
	boxY     int
	editQ    int    // queued message being edited in the box, +1; 0 when none
	editWas  string // its text before editing
	slashSel int    // the slash-command picker's selection
	pastes   pastes // long pastes shown as chips
	arts     []*artifact
	marks    map[string]bool // files marked reviewed in the changes view
	artsKey  string
	local    []headless.Command // custom commands and skills on disk
	skills   map[string]bool
	// cardFocus is set when ↑ has moved the keys from the box onto a card
	// waiting for an answer; only then do plain letters and digits answer.
	cardFocus bool

	// Answering Claude's questions, one at a time.
	qFor      string
	qIdx      int
	qCursor   int // the option ↑↓ is on while the card has the keys
	qPicked   map[int]bool
	qAnswer   map[string]string
	stopArmed time.Time
	lastSend  time.Time
	flushed   time.Time     // when the last batch of host lines was taken in
	watching  []watched     // the transcripts being watched for growth
	drawn     convo.Options // how the conversation was last drawn
	drewConvo bool
	seenRef   map[string]bool
	stopWatch chan struct{}
	// selMoved asks the next draw to scroll the selection into view;
	// rowRefs is what each drawn row of the pane belongs to, for clicks.
	selMoved bool
	rowRefs  []string
	bodyRefs []string // every selectable row of the current view, in order

	// Subagents: every run found beside the transcript, and the one opened.
	path     string
	subs     []convo.Subagent
	subTail  *convo.Tail
	subTails map[string]*convo.Tail // every run, followed for its numbers
	subOpen  string
	subList  convo.Subagents // finds the runs, reading each one's meta once
	subSel   string          // selection inside the opened subagent

	// Search: ctrl+f turns the message box into a search box.
	searching bool
	query     []rune
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

// frame is how long a burst of output collects before the pane redraws;
// the replay on connecting gathers for a little longer so it draws whole.
const (
	frame  = 16 * time.Millisecond
	replay = 33 * time.Millisecond
)

func openHost(a *fleet.Agent) tea.Cmd {
	key, id, path := a.Key, a.ID, a.TranscriptPath
	return func() tea.Msg {
		cl, err := host.Dial(id)
		if err != nil {
			return hostOpenMsg{key: key, err: err}
		}
		// A session that took over an existing conversation shows it: the
		// transcript up to when the host started, then the host's replay.
		sess := convo.New()
		if cfg, err := host.ReadConfig(id); err == nil && cfg.Resume {
			started := time.Now()
			if info, err := host.ReadInfo(id); err == nil && !info.StartedAt.IsZero() {
				started = info.StartedAt
			}
			sess = convo.History(path, started)
			if len(sess.Turns) == 0 && cfg.From != "" {
				sess = convo.History(filepath.Join(filepath.Dir(path), cfg.From+".jsonl"), started)
			}
		}
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, client: cl, sess: sess, open: map[string]bool{}, path: path}}
	}
}

// next waits for output and hands it over at once when the pane last drew
// a while ago; in a burst it gathers lines until a frame has passed since
// the last batch, so a busy session costs one redraw per frame, not one per
// line, and a delta never waits more than a frame.
func (c *hostConn) next() tea.Cmd {
	lines, key, last := c.client.Lines, c.key, c.flushed
	return func() tea.Msg {
		first, ok := <-lines
		if !ok {
			return hostLinesMsg{key: key, closed: true}
		}
		batch := [][]byte{first}
		wait := replay
		if !last.IsZero() {
			wait = frame - time.Since(last)
		}
		var due <-chan time.Time
		if wait > 0 {
			t := time.NewTimer(wait)
			defer t.Stop()
			due = t.C
		}
		for len(batch) < 4096 {
			var l []byte
			ok := true
			if due == nil { // only what is already here comes along
				select {
				case l, ok = <-lines:
				default:
					return hostLinesMsg{key: key, lines: batch}
				}
			} else {
				select {
				case l, ok = <-lines:
				case <-due:
					return hostLinesMsg{key: key, lines: batch}
				}
			}
			if !ok {
				return hostLinesMsg{key: key, lines: batch, closed: true}
			}
			batch = append(batch, l)
		}
		return hostLinesMsg{key: key, lines: batch}
	}
}

// syncHost keeps one connection open, to the agtop-mode agent the pane is
// showing, and closes it when the pane moves on.
func (m *Model) syncHost() tea.Cmd {
	a := m.focused()
	_, paneW, _ := m.layout()
	showing := a != nil && paneW > 0 && m.mode == modeList
	hosted := showing && a.Agtop && a.PID != 0
	fromFile := showing && !hosted && a.TranscriptPath != ""
	if !hosted && !fromFile {
		m.dropHost()
		if a == nil {
			m.paneFocus = false
		}
		return nil
	}
	if m.host != nil && m.host.key == a.Key && (m.host.client != nil) == hosted {
		return nil
	}
	if m.hostOpening == a.Key {
		return nil
	}
	m.dropHost()
	m.hostOpening = a.Key
	if fromFile {
		return openTail(a)
	}
	return openHost(a)
}

func (m *Model) dropHost() {
	if m.host != nil {
		m.host.unwatch()
	}
	if m.host != nil && m.host.client != nil {
		_ = m.host.client.Close()
	}
	m.host, m.hostOpening = nil, ""
}

// openTail reads a Claude Code session's transcript in the background the
// first time; after that a watch takes in what is new as it is written.
func openTail(a *fleet.Agent) tea.Cmd {
	key, id, path := a.Key, a.ID, a.TranscriptPath
	return func() tea.Msg {
		t := convo.NewTail(path)
		if _, err := t.Read(); err != nil {
			return hostOpenMsg{key: key, err: err}
		}
		return hostOpenMsg{key: key, c: &hostConn{key: key, id: id, tail: t, sess: t.Sess, open: map[string]bool{}, ready: true, path: path}}
	}
}

// followTail takes in new transcript lines, and closes the last turn once
// the agent has stopped working (transcripts don't always mark it).
func (m *Model) followTail() {
	c := m.host
	if c == nil || c.tail == nil {
		return
	}
	_, _ = c.tail.Read()
	a := m.agentByKey(c.key)
	if a == nil {
		return
	}
	if live := c.sess.Live(); live != nil && !a.Live() {
		c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
	}
	c.sess.Info.Cwd = a.Cwd
	c.sess.Info.CostUSD = a.Spend.Cost
	if c.sess.Info.Model == "" {
		c.sess.Info.Model = a.Spend.Model
	}
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
		m.flash("couldn't open the session: "+msg.err.Error(), true)
		if m.openFailed == nil {
			m.openFailed = map[string]time.Time{}
		}
		m.openFailed[msg.key] = time.Now() // zen moves past it for a while
		return nil
	}
	m.host = msg.c
	if m.host.client == nil {
		m.followTail()
		return nil
	}
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
	c.ready, c.flushed = true, time.Now()
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
	gap := w - cellw.String(left) - cellw.String(right)
	if gap < 2 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// agtopPane is the right pane for an agtop-mode agent, or nil when the pane
// shows something else.
func (m *Model) agtopPane(w, h int) []string {
	a := m.focused()
	if a == nil {
		return nil
	}
	c := m.host
	if c == nil || c.key != a.Key {
		if !a.Agtop {
			return nil // still loading; the summary shows meanwhile
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
	view := m.viewName(c)
	if c.searching {
		view = "search"
		body = s.SearchView(string(c.query), o)
	}
	if m.zen {
		view = "zen"
		body = m.zenBody(a, c, w)
	}
	switch view {
	case "search", "zen":
	case "screen":
		for _, l := range m.liveLines(w) {
			body = append(body, convo.Line{Text: l})
		}
		if len(body) == 0 {
			body = []convo.Line{{Text: ""}, {Text: dim("  connecting to its screen…")}}
		}
	case "overview":
		body = s.Overview(o)
	case "changes":
		o.Marks = c.marks
		body = s.ChangesView(o)
	case "queue":
		body = m.queueLines(c, o)
	case "tasks":
		body = m.taskLines(c, o)
	case "artifacts":
		body = m.artifactLines(c, o)
	case "subagents":
		if c.subOpen != "" {
			o.Selected = c.subSel
		}
		body = m.subagentLines(c, o)
	default:
		body = s.Render(o)
		c.drawn, c.drewConvo = o, true
		if len(body) == 0 {
			body = []convo.Line{{Text: ""}, {Text: dim("  nothing yet · type below to start")}}
		}
	}
	// Bottom-anchored: the latest output sits just above the dock unless
	// you've scrolled up. A selection that just moved is scrolled into view.
	c.bodyRefs = c.bodyRefs[:0]
	if c.seenRef == nil {
		c.seenRef = map[string]bool{}
	}
	clear(c.seenRef)
	prev := ""
	for _, l := range body {
		// A ref's rows sit together, so most repeats are the row before's.
		if l.Ref != "" && l.Ref != prev && !c.seenRef[l.Ref] {
			c.seenRef[l.Ref] = true
			c.bodyRefs = append(c.bodyRefs, l.Ref)
		}
		prev = l.Ref
	}
	if c.selMoved && c.sel != "" {
		c.selMoved = false
		for i, l := range body {
			if l.Ref != c.sel {
				continue
			}
			end := len(body) - c.scroll
			switch {
			case i < end-bodyH:
				c.scroll = len(body) - (i + bodyH)
			case i >= end:
				c.scroll = len(body) - (i + 1)
			}
			break // the first row of a selection is the one to show
		}
	}
	c.scroll = max(0, min(c.scroll, len(body)-bodyH))
	// Scrolled up, the "more below" pill takes a row of its own rather
	// than covering the last one (which may be the selected one).
	rows := bodyH
	if c.scroll > 0 {
		rows--
	}
	end := len(body) - c.scroll
	start := max(0, end-rows)
	out := append([]string{}, head...)
	c.rowRefs = make([]string, len(head), h)
	for _, l := range body[start:end] {
		out = append(out, l.Text)
		c.rowRefs = append(c.rowRefs, l.Ref)
	}
	// Scrolled into a turn whose heading is off the top: pin the heading
	// there, so you always know whose turn you're reading.
	if m.viewName(c) == "conversation" && start > 0 && end > start && !c.searching {
		for i := start; i >= 0; i-- {
			if r := body[i].Ref; isTurnRef(r) {
				if i < start && !isTurnRef(body[start].Ref) {
					out[len(head)] = body[i].Text
					c.rowRefs[len(head)] = r
				}
				break
			}
		}
	}
	if c.scroll > 0 {
		pill := selBG + " " + paint(cText, fmt.Sprintf("↓ %d more · end follows", c.scroll)) + " " + reset
		out = append(out, spread("", pill, w))
		c.rowRefs = append(c.rowRefs, "")
	}
	for len(out) < h-len(dock) {
		out = append(out, "")
	}
	c.boxY = m.paneTop + len(out) + c.boxIdx
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
	case c.client == nil:
		switch {
		case a.NeedsYou() || a.Waiting():
			state = paint(cYellow+bold, "● needs you")
		case a.Live():
			state = paint(cOrange, "✻ working")
		case a.PID != 0:
			state = dim("◦ idle")
		default:
			state = dim("⏹ stopped")
		}
	case info.Limit != nil:
		state = paint(cYellow, "⏸ "+limitText(info.Limit))
	case info.Retry != nil && info.Retry.GaveUp:
		state = paint(cRed, "✗ API error · "+info.Retry.Why)
	case info.Retry != nil:
		state = paint(cYellow, fmt.Sprintf("⟳ retry %d of %d in %s", info.Retry.Attempt, info.Retry.Max, dur(time.Until(info.Retry.Next).Round(time.Second))))
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
	if c.client == nil {
		switch {
		case a.Agtop:
			conn = dim("stopped · a message resumes it")
		case a.Headless:
			conn = dim("Claude Code · " + a.Where())
		case a.Interactive:
			conn = dim("Claude Code · "+a.Where()+" · ") + paint(cOrange, "/agtop") + dim(" copies it here")
		case m.moveWhenIdle[a.Key]:
			conn = paint(cOrange, "moves to agtop mode when this turn ends")
		default:
			conn = dim("Claude Code · ") + paint(cOrange, "/agtop") + dim(" moves it here")
		}
	}
	if (info.Limit != nil || info.Retry != nil) && !info.CacheWarm.IsZero() {
		if time.Now().Before(info.CacheWarm) {
			conn = dim("cache warm until ") + paint(cGreen, info.CacheWarm.Local().Format("15:04")) + "   " + conn
		} else {
			conn = paint(cYellow, "cache cold") + "   " + conn
		}
	}
	row2 := spread("  "+meta, conn+" ", w)

	var tabs []string
	views := m.views(c)
	for i, v := range views {
		if i == c.view%len(views) {
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
	if m.viewName(c) == "screen" {
		chips = dim("typing goes into it · ctrl+] comes back · ctrl+f full screen") + "  "
	}
	row3 := spread("  "+strings.Join(tabs, " ")+dim("   [ ]"), chips, w)
	// The chrome's own background marks it off; no half-block edge.
	return []string{onBg(bgChrome, row1, w), onBg(bgChrome, row2, w), onBg(bgChrome, row3, w)}
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
	out := []string{onBg(bgChrome, "", w)} // a row of the dock's own ground
	line := func(txt string) { out = append(out, onBg(bgChrome, txt, w)) }

	// A Claude Code agent's own screen says what the transcript can't yet:
	// that it's working, for how long and on how many tokens. That one
	// line stands in for the task line (it names the same task).
	working := ""
	if l := m.live; c.client == nil && l != nil && l.key == c.key && l.ready.Load() {
		working = readScreen(l.lines()).working
	}
	now, done, total := s.Current()
	if working != "" {
		count := ""
		if total > 0 {
			count = paint(cSub, fmt.Sprintf("%d/%d", done, total)) + "  "
		}
		line(spread("  "+paint(cOrange, ansi.Truncate(working, w-12, "…")), count, w))
	} else if total > 0 {
		t := ""
		if now != nil {
			t = paint(cOrange, "■ ") + paint(cText, oneLine(firstNonEmpty(now.Active, now.Subject)))
		} else {
			t = dim("no task in progress")
		}
		line(spread("  "+t+"  "+paint(cSub, fmt.Sprintf("%d/%d", done, total)), "", w))
	}
	if l := s.Info.Limit; l != nil && l.Ask {
		card := "\x1b[48;2;42;36;25m"
		cl := func(txt string) { out = append(out, onBg(card, txt, w)) }
		cl(paint(cYellow, "▍") + " " + paint(cYellow+bold, "⏸ usage limit") + "   " + paint(cText, limitText(l)))
		cl(paint(cYellow, "▍") + "     " + dim("Continue by itself when the limit resets? Anything you send meanwhile waits in the queue."))
		cl(paint(cYellow, "▍") + "   " + cardHint(c, paint(cText+bold, "y")+" "+paint(cSub, "continue at the reset")+"   "+paint(cText+bold, "n")+" "+paint(cSub, "wait for me")))
	}
	if r := s.Info.Retry; r != nil && r.GaveUp {
		line("  " + paint(cRed, "✗ "+r.Reason) + dim(" · "+r.Why+" · send anything to try again"))
	}
	if p := s.Pending(); len(p) > 0 && p[0].Approval.Tool == "AskUserQuestion" {
		out = append(out, m.questionCard(c, p[0].Approval, w)...)
	} else if len(p) > 0 {
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
		k := func(key, label string) string { return paint(cText+bold, key) + " " + paint(cSub, label) }
		edge := paint(cYellow, "▍")
		if c.cardFocus {
			edge = paint(cOrange, "▍")
		}
		cl(edge + "   " + cardHint(c, k("y", "allow once")+"   "+k("a", "always allow")+"   "+k("n", "deny")))
	}
	if run := c.runningSubs(); len(run) > 0 && m.viewName(c) == "conversation" {
		var names []string
		for _, sa := range run {
			names = append(names, sa.Type)
		}
		left := "  " + paint(cOrange, spinner[m.tick%len(spinner)]) + " " + paint(cBlue, "⇉ ") +
			paint(cSub+bold, fmt.Sprintf("%d running", len(run))) + "  " + dim(ansi.Truncate(strings.Join(names, " · "), max(10, w-44), "…"))
		line(spread(left, keys("alt+↓", "view them")+"  ", w))
	}
	if q := m.queueOf(c).items; len(q) > 0 {
		when := " · sends when this turn ends"
		if c.client == nil {
			when = " · sends within 15s, or when it's idle"
		}
		line(spread("  "+paint(cSub+bold, fmt.Sprintf("queue %d", len(q)))+dim(when), "", w))
		for i, item := range q {
			if i >= 3 {
				line(dim(fmt.Sprintf("   … %d more", len(q)-i)))
				break
			}
			line("   " + dim(fmt.Sprint(i+1)) + "  " + paint(cSub, ansi.Truncate(shortImages(oneLine(item)), w-8, "…")))
		}
	}
	top := dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 28, "…"))
	switch {
	case c.client == nil && a.Interactive:
		top += dim(" · " + a.Where() + ", so it can't take messages here")
	case c.client == nil && a.Agtop:
		top += dim(" · stopped; ") + paint(cOrange, "enter resumes it") + dim(" with your message")
	case c.client == nil:
		top += dim(" · enter replies through Claude Code")
	case isQuestion(s.Pending()):
		top += dim(" · ") + paint(cYellow, "pick above, or type your own answer · enter sends it")
	case len(s.Pending()) > 0:
		top += dim(" · ") + paint(cYellow, "answer the card first, or type a note")
	case s.Live() != nil:
		top += dim(" · working, so ") + paint(cOrange, "enter queues") + dim(" · ctrl+s sends now")
	default:
		top += dim(" · enter sends")
	}
	typing := m.paneFocus && c.sel == "" && !c.cardFocus
	if m.paneFocus && !typing {
		top = dim("typing returns here · ↓ past the last row or esc")
	}
	b := box{w: w, focused: typing, topL: top, text: c.input, cursor: max(0, len(c.input)-c.back), anchor: c.anchor - 1,
		lead: paint(cOrange, "❯ "), holder: "a message for this agent", maxRows: 6}
	if c.searching {
		b = box{w: w, focused: m.paneFocus, topL: paint(cOrange, "search this session") + dim(" · ↑↓ pick · enter jumps · esc closes"),
			text: c.query, cursor: len(c.query), lead: paint(cOrange, "⌕ "), holder: "words, or is:failed file:host.go turn:3", maxRows: 1}
	}
	if mode := s.Info.PermissionMode; mode != "" {
		b.topR = paint(cOrange, mode)
	}
	if l := chips(c.images, w); l != "" {
		out = append(out, onBg(bgChrome, l, w))
	}
	out = append(out, m.slashLines(c, w)...)
	if c.editQ > 0 {
		b.topL = paint(cOrange, fmt.Sprintf("editing queued message %d", c.editQ)) + dim(" · enter saves it back · esc cancels")
	}
	c.box, c.boxIdx = b, len(out)
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
	if c.searching {
		return m.searchKey(c, k, s)
	}
	if s == "ctrl+f" && !m.zen { // zen draws only the agent's card, not results
		c.searching, c.query, c.sel = true, nil, ""
		return nil
	}
	empty := len(c.input) == 0
	if cmd, used := m.slashKey(c, s); used {
		return cmd
	}
	if m.viewName(c) == "queue" {
		queueKey := m.queueKey
		if c.client == nil {
			queueKey = m.localQueueKey
		}
		if cmd, used := queueKey(c, s); used {
			return cmd
		}
	}
	if s == "esc" && c.editQ > 0 {
		c.editQ, c.input, c.back = 0, c.input[:0], 0
		return nil
	}
	if cmd, used := m.cardKey(c, s, empty); used {
		return cmd
	}
	if (s == "backspace" || s == "ctrl+h") && empty && len(c.images) > 0 {
		c.images = c.images[:len(c.images)-1]
		return nil
	}
	switch s {
	case "esc":
		switch {
		case !empty:
			c.input, c.back = c.input[:0], 0
		case c.sel != "":
			c.sel = "" // first esc drops the step selection
		case m.zen:
			// Zen keeps the keys on the agent; tab leaves zen.
		default:
			m.leavePane()
		}
		return nil
	case "ctrl+c":
		switch {
		case c.anchor > 0 && c.anchor-1 != len(c.input)-c.back:
			// a selection: copy it (handled by the editor below)
		case !empty:
			c.input, c.back, c.anchor = c.input[:0], 0, 0
			return nil
		default:
			return m.quitKey()
		}
	case "enter", "right":
		if step, ok := strings.CutPrefix(c.sel, "jump:"); ok && empty && s == "enter" && step != "" {
			// From a hunk to the step that made it, in the conversation.
			turn, _, _ := strings.Cut(step, ":")
			c.open[turn], c.open[step] = true, true
			if p := c.sess.ParentRef(step); p != "" {
				c.open[p] = true
			}
			c.view, c.sel, c.selMoved = 0, step, true
			return nil
		}
		if url, ok := strings.CutPrefix(c.sel, "art:"); ok && empty && m.viewName(c) == "artifacts" {
			return browse(url)
		}
		if empty && m.viewName(c) == "subagents" && strings.HasPrefix(c.sel, "sub:") && c.subOpen == "" {
			m.openSub(c, strings.TrimPrefix(c.sel, "sub:"))
			return nil
		}
		// Enter on a subagent's step opens its own conversation.
		if empty && s == "enter" && m.viewName(c) == "conversation" {
			if _, id, ok := strings.Cut(c.sel, ":s:"); ok {
				for _, sa := range c.subs {
					if sa.ToolUseID == id {
						for i, v := range m.views(c) {
							if v == "subagents" {
								c.view = i
							}
						}
						m.openSub(c, sa.ID)
						return nil
					}
				}
			}
		}
		if s == "right" {
			if !empty {
				break // move the cursor
			}
			if c.sel != "" {
				c.open[c.sel] = true
			}
			return nil
		}
		if empty && m.viewName(c) == "screen" && m.canEmbed() {
			m.embedded = true // keys go to Claude Code's own screen
			return nil
		}
		if empty && len(c.images) > 0 {
			return m.sendPane(c, false)
		}
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
		// ← only ever means back: out of an opened subagent, off a
		// selected row, then to Agents. It never folds anything.
		if empty {
			switch {
			case c.subOpen != "" && m.viewName(c) == "subagents":
				c.subOpen, c.subTail, c.sel = "", nil, ""
			case c.sel != "":
				c.sel = ""
			case m.zen:
				// Zen keeps the keys on the agent; tab leaves zen.
			default:
				m.leavePane()
			}
			return nil
		}
	case "alt+r":
		// Mark a file reviewed in the changes view, or unmark it.
		if path, ok := strings.CutPrefix(c.sel, "chg:"); ok && m.viewName(c) == "changes" {
			if c.marks == nil {
				c.marks = map[string]bool{}
			}
			c.marks[path] = !c.marks[path]
			return nil
		}
	case "alt+down", "alt+up":
		// Step through the subagent runs in place, like Claude Code's
		// switcher; the main conversation sits at either end.
		d := 1
		if s == "alt+up" {
			d = -1
		}
		m.cycleSub(c, d)
		return nil
	case "[", "]":
		if empty {
			d, n := 1, len(m.views(c))
			if s == "[" {
				d = -1
			}
			c.view = (c.view%n + d + n) % n
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
		if c.client == nil {
			return m.stopOrRemove(m.focused())
		}
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
		if c.client == nil {
			return nil
		}
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
		if m.zen {
			m.zenSkip()
			return nil
		}
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
	if k.Text != "" && c.sel != "" {
		c.sel = "" // typing returns to the box: nothing stays highlighted
	}
	if s == "ctrl+g" {
		return editDraft(&c.pastes, c.input, true)
	}
	if (s == "backspace" || s == "ctrl+h") && c.anchor == 0 {
		if buf, pos, ok := dropChip(c.input, len(c.input)-c.back); ok {
			c.input, c.back = buf, len(buf)-pos
			return nil
		}
	}
	buf, pos, anchor, copied, _ := editSel(c.input, max(0, len(c.input)-c.back), c.anchor-1, k, s)
	c.input, c.back, c.anchor = buf, len(buf)-pos, anchor+1
	if s == "space" || s == "enter" {
		// A path you typed to an image becomes an attachment once it's done.
		c.input, c.images = pullImages(c.input, c.images)
		c.back = min(c.back, len(c.input))
	}
	if copied != "" {
		m.copyText(copied)
	}
	return nil
}

// clickBox places the cursor where a click lands inside either input box,
// and gives that box the keys. It reports whether the click was in one.
func (m *Model) clickBox(x, y int) bool {
	if c := m.host; c != nil && len(c.box.text) >= 0 && c.box.w > 0 {
		x0 := 2
		if m.listW > 0 {
			x0 = m.listW + 3
		}
		rows := len(c.box.lines()) - 2
		if y > c.boxY && y <= c.boxY+rows && x >= x0 && x < x0+c.box.w {
			m.paneFocus = true
			pos := c.box.at(y-c.boxY-1, x-x0)
			c.back, c.anchor = len(c.input)-pos, 0
			return true
		}
	}
	b := m.promptBox
	if m.zen || b.w == 0 {
		return false
	}
	rows := len(b.lines()) - 2
	if y > m.promptBoxY && y <= m.promptBoxY+rows && x < b.w {
		m.paneFocus, m.embedded = false, false
		m.setCursor(b.at(y-m.promptBoxY-1, x))
		m.anchor = 0
		return true
	}
	return false
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
	text = strings.TrimSpace(c.pastes.expand(text))
	c.pastes = pastes{}
	if c.editQ > 0 {
		i, was := c.editQ-1, c.editWas
		c.editQ, c.input, c.back = 0, c.input[:0], 0
		if c.client == nil {
			m.editLocal(c, i, was, text)
			return nil
		}
		return hostCmd(func() error { return c.client.EditQueued(i, was, text) })
	}
	if strings.HasPrefix(text, "/") {
		if cmd, ok := m.runAgtopCommand(c, text); ok {
			c.input, c.back = c.input[:0], 0
			return cmd
		}
	}
	images := c.images
	// Paths typed or dropped without a paste become attachments too.
	if rest, imgs := extractImages(text); imgs != nil {
		images, text = append(images, imgs...), rest
	}
	c.input, c.back, c.images = c.input[:0], 0, nil
	c.scroll = 0
	c.lastSend = time.Now()
	if a := m.focused(); a != nil {
		m.markSeen(a)
	}
	if c.client == nil {
		return m.sendOffline(c, text, images)
	}
	if len(images) > 0 {
		return hostCmd(func() error { return c.client.SendImages(text, images) })
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
	// What the renderer opens by default: recent turns and failures. The
	// options the pane last drew with find it in the renderer's cache;
	// any others would redraw every turn, twice.
	o := convo.Options{Width: max(40, m.w/2), Now: time.Now(), Open: c.open}
	if c.drewConvo {
		o = c.drawn
	}
	for _, l := range c.sess.Render(o) {
		if l.Ref == ref {
			return !strings.Contains(ansi.Strip(l.Text), "▸")
		}
	}
	return false
}

// moveSel moves the selection over rows you can act on: turns and steps.
func (m *Model) moveSel(c *hostConn, d int) {
	refs := c.bodyRefs
	if len(refs) == 0 {
		return
	}
	cur := c.sel
	if c.subOpen != "" && m.viewName(c) == "subagents" {
		cur = c.subSel
	}
	i := len(refs)
	for j, r := range refs {
		if r == cur {
			i = j
		}
	}
	if i+d >= len(refs) && cur != "" {
		// ↓ past the last row: back to typing, nothing highlighted.
		c.sel, c.subSel = "", ""
		return
	}
	i = max(0, min(len(refs)-1, i+d))
	if c.subOpen != "" && m.viewName(c) == "subagents" {
		c.subSel = refs[i]
	}
	c.sel = refs[i]
	c.selMoved = true
}

// clickRow selects the row under a click in the pane; clicking the selected
// row again opens or closes it.
func (m *Model) clickRow(c *hostConn, y int) {
	i := y - m.paneTop
	if i < 0 || i >= len(c.rowRefs) || c.rowRefs[i] == "" {
		return
	}
	ref := c.rowRefs[i]
	if ref == c.sel {
		if id, ok := strings.CutPrefix(ref, "sub:"); ok {
			m.openSub(c, id)
			return
		}
		c.open[ref] = !m.isOpen(c, ref)
		return
	}
	c.sel = ref
}

// leavePane gives the keys back to the list. On a narrow screen, where the
// conversation filled it, the list comes back too.
func (m *Model) leavePane() {
	m.paneFocus = false
	if m.listW == 0 {
		m.preview, m.full = false, false
	}
}

// sendOffline sends from the message box of a session agtop isn't hosting:
// a stopped agtop session resumes with it, a Claude Code session gets it as
// a reply through its daemon.
func (m *Model) sendOffline(c *hostConn, text string, images []string) tea.Cmd {
	a := m.agentByKey(c.key)
	switch {
	case a == nil:
		return nil
	case a.Interactive:
		m.flash(a.DisplayName+" is "+a.Where()+"; agtop can't send to it", true)
		return nil
	case a.Agtop:
		cfg, err := host.ReadConfig(a.ID)
		if err != nil {
			cfg = host.Config{ID: a.ID, SessionID: a.SessionID, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName}
		}
		cfg.Resume, cfg.Prompt, cfg.Images = true, text, images
		m.flash("resuming "+a.DisplayName+"…", false)
		return func() tea.Msg {
			if _, err := host.Spawn(cfg); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "resumed " + a.DisplayName}
		}
	}
	text = withImages(text, images)
	if busy(a) || len(m.queueOf(c).items) > 0 {
		// It's working: the message waits in the queue and goes when it
		// is idle, together with anything else waiting.
		m.queueLocal(a.Key, text)
		m.flash(fmt.Sprintf("queued · %d waiting · goes to %s within 15s", len(m.localQ[a.Key].items), a.DisplayName), false)
		return nil
	}
	m.loader.Nudge(a.Key)
	m.markSeen(a)
	return cmdErr("sent to "+a.DisplayName, func() error { return actions.Reply(a.Acct, a.ID, text) })
}

// focusPane moves keys into the selected agtop-mode agent's pane.
func (m *Model) focusPane(a *fleet.Agent) tea.Cmd {
	if a == nil {
		return nil
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
	images := m.images
	if rest, imgs := extractImages(text); imgs != nil {
		images, text = append(images, imgs...), rest
	}
	m.images = nil
	name := sessionName(text)
	if name == "" && len(images) > 0 {
		name = "about " + filepath.Base(images[0])
	}
	if name == "" {
		name = "fresh session in " + filepath.Base(dir)
	}
	cfg := host.Config{
		Account: m.store.Config.ActiveAccount(), Cwd: dir, Prompt: text, Images: images, Name: name,
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission, LimitMode: d.OnLimit,
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
	case a.SessionID == "":
		m.flash("can't find "+a.DisplayName+"'s conversation to resume", true)
		return nil
	case a.Headless:
		m.flash(a.DisplayName+" is driven by another program; agtop can't take it over", true)
		return nil
	case !a.Interactive && busy(a) && !m.moveWhenIdle[a.Key]:
		// Stopping it now would lose the turn in progress.
		if m.moveWhenIdle == nil {
			m.moveWhenIdle = map[string]bool{}
		}
		m.moveWhenIdle[a.Key] = true
		m.flash(a.DisplayName+" moves to agtop mode when this turn ends · /agtop again moves it now", false)
		return nil
	}
	delete(m.moveWhenIdle, a.Key)
	d := m.store.Config.Dispatch
	cfg := host.Config{
		SessionID: a.SessionID, Resume: true, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName,
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission, LimitMode: d.OnLimit,
	}
	old := a.Key
	if a.Interactive {
		// Its terminal keeps the original; agtop carries on with a copy.
		cfg.Fork, cfg.From = true, a.SessionID
		m.flash("copying "+a.DisplayName+" into agtop mode · the terminal one is left as it is", false)
		return func() tea.Msg {
			c, err := host.Spawn(cfg)
			if err != nil {
				return doneMsg{err: err}
			}
			// The original goes to Done, the copy takes its name: one agent
			// carrying on, not two.
			return movedToAgtopMsg{from: old, started: hostStartedMsg{id: c.ID, name: cfg.Name, acct: cfg.Account.Name}}
		}
	}
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

// movePending moves agents waiting to go to agtop mode once they're idle.
func (m *Model) movePending() tea.Cmd {
	var cmds []tea.Cmd
	for key := range m.moveWhenIdle {
		a := m.agentByKey(key)
		switch {
		case a == nil || a.Agtop:
			delete(m.moveWhenIdle, key)
		case !busy(a):
			cmds = append(cmds, m.moveToAgtop(a))
		}
	}
	return tea.Batch(cmds...)
}

type movedToAgtopMsg struct {
	from    string
	started hostStartedMsg
}

func limitText(l *host.Limit) string {
	win := map[string]string{"five_hour": "5h limit", "seven_day": "7d limit", "seven_day_opus": "7d Opus limit"}[l.Window]
	if win == "" {
		win = "usage limit"
	}
	t := win
	if !l.ResetsAt.IsZero() {
		t += " · resets " + l.ResetsAt.Local().Format("15:04")
	}
	switch {
	case l.Continue:
		t += " · continues then"
	case !l.Ask:
		t += " · waiting for you"
	}
	return t
}

// --- Claude's questions (the AskUserQuestion tool) ---

type question struct {
	Question    string `json:"question"`
	Header      string `json:"header"`
	MultiSelect bool   `json:"multiSelect"`
	Options     []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

func isQuestion(p []*convo.Step) bool {
	return len(p) > 0 && p[0].Approval != nil && p[0].Approval.Tool == "AskUserQuestion"
}

func questions(req *headless.PermissionRequest) (title string, qs []question) {
	var in struct {
		Title     string     `json:"title"`
		Questions []question `json:"questions"`
	}
	_ = json.Unmarshal(req.Input, &in)
	return in.Title, in.Questions
}

// syncQuestion resets the answering state when a new question arrives.
func (c *hostConn) syncQuestion(req *headless.PermissionRequest) {
	if c.qFor != req.ID {
		c.qFor, c.qIdx, c.qCursor, c.qPicked, c.qAnswer = req.ID, 0, 0, map[int]bool{}, map[string]string{}
	}
}

func (m *Model) questionCard(c *hostConn, req *headless.PermissionRequest, w int) []string {
	c.syncQuestion(req)
	title, qs := questions(req)
	if c.qIdx >= len(qs) {
		return nil
	}
	q := qs[c.qIdx]
	card := "\x1b[48;2;42;36;25m"
	var out []string
	cl := func(txt string) { out = append(out, onBg(card, txt, w)) }
	head := paint(cYellow+bold, "? Claude asks")
	if title != "" {
		head += "   " + paint(cText, title)
	}
	count := ""
	if len(qs) > 1 {
		count = fmt.Sprintf("question %d of %d", c.qIdx+1, len(qs))
	}
	if q.Header != "" {
		count = strings.TrimSpace(q.Header + "   " + count)
	}
	cl(spread(paint(cYellow, "▍")+" "+head, paint(cSub, count)+"  ", w))
	for _, l := range wrap(q.Question, w-8) {
		cl(paint(cYellow, "▍") + "     " + paint(cText+bold, l))
	}
	edge := paint(cYellow, "▍")
	if c.cardFocus {
		edge = paint(cOrange, "▍")
	}
	cl(edge)
	descW := min(w-14, 100)
	for i, o := range q.Options {
		label := strings.TrimSpace(o.Label)
		rec := strings.Contains(strings.ToLower(label), "(recommended)")
		if rec {
			label = strings.TrimSpace(strings.Replace(strings.Replace(label, "(Recommended)", "", 1), "(recommended)", "", 1))
		}
		num := paint(cSub, fmt.Sprint(i+1))
		box := ""
		if q.MultiSelect {
			box = dim("☐ ")
			if c.qPicked[i] {
				box = paint(cGreen, "☑ ")
			}
		}
		name := paint(cText+bold, label)
		if rec {
			name += "  " + paint(cGreen, "recommended")
		}
		sel := c.cardFocus && c.qCursor == i
		row := edge + "   " + num + "  " + box + name
		if sel {
			row = edge + " " + paint(cOrange, "▸") + " " + num + "  " + box + name
		}
		if sel {
			out = append(out, onBg(selBG, row, w))
		} else {
			cl(row)
		}
		// Descriptions wrap under their option, two lines at most.
		for j, l := range wrap(oneLine(o.Description), descW) {
			if j == 2 {
				cl(edge + "        " + dim("…"))
				break
			}
			if o.Description == "" {
				break
			}
			cl(edge + "        " + dim(l))
		}
	}
	own := dim("✎ answer in your own words: type below and press enter")
	if c.cardFocus && c.qCursor == len(q.Options) {
		own = paint(cOrange, "▸ ") + paint(cText, "✎ answer in your own words: type below and press enter")
		out = append(out, onBg(selBG, edge+"   "+own, w))
	} else {
		cl(edge + "   " + own)
	}
	cl(edge)
	keysHint := "↑↓ choose · enter picks · 1–" + fmt.Sprint(len(q.Options)) + " · s skips"
	if q.MultiSelect {
		keysHint = "↑↓ choose · space toggles · enter confirms · s skips"
	}
	if c.cardFocus {
		cl(edge + "   " + cardHint(c, paint(cSub, keysHint)))
	} else {
		cl(edge + "   " + dim("↑ to choose an option   ·   or type your own answer below"))
	}
	return out
}

// questionKey answers the current question: a digit picks (or toggles, for
// multi-select), enter confirms toggles or sends typed text as the answer,
// and esc skips. It reports whether it used the key.
func (m *Model) questionKey(c *hostConn, req *headless.PermissionRequest, s string, empty bool) (tea.Cmd, bool) {
	c.syncQuestion(req)
	_, qs := questions(req)
	if c.qIdx >= len(qs) {
		return nil, false
	}
	q := qs[c.qIdx]
	if c.cardFocus && empty {
		switch s {
		case "up":
			c.qCursor = max(0, c.qCursor-1)
			return nil, true
		case "down":
			if c.qCursor >= len(q.Options) {
				return nil, false // past the last row: back to the box
			}
			c.qCursor++
			return nil, true
		case "space":
			if q.MultiSelect && c.qCursor < len(q.Options) {
				c.qPicked[c.qCursor] = !c.qPicked[c.qCursor]
				return nil, true
			}
		case "enter":
			switch {
			case c.qCursor >= len(q.Options):
				c.cardFocus = false // "your own words": type in the box
				return nil, true
			case !q.MultiSelect:
				return m.answerQuestion(c, req, qs, q.Options[c.qCursor].Label), true
			case len(c.qPicked) == 0:
				c.qPicked[c.qCursor] = true
			}
		}
	}
	if empty && len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		i := int(s[0] - '1')
		if i >= len(q.Options) {
			return nil, true
		}
		if q.MultiSelect {
			c.qPicked[i] = !c.qPicked[i]
			return nil, true
		}
		return m.answerQuestion(c, req, qs, q.Options[i].Label), true
	}
	switch s {
	case "enter":
		if !empty {
			text := strings.TrimSpace(string(c.input))
			c.input, c.back = c.input[:0], 0
			return m.answerQuestion(c, req, qs, text), true
		}
		if q.MultiSelect && len(c.qPicked) > 0 {
			var picked []string
			for i, o := range q.Options {
				if c.qPicked[i] {
					picked = append(picked, o.Label)
				}
			}
			return m.answerQuestion(c, req, qs, strings.Join(picked, ", ")), true
		}
		return nil, true
	}
	return nil, false
}

// answerQuestion records one answer and, after the last question, replies:
// Claude Code takes the answers as the tool's input, keyed by question text.
func (m *Model) answerQuestion(c *hostConn, req *headless.PermissionRequest, qs []question, answer string) tea.Cmd {
	c.qAnswer[qs[c.qIdx].Question] = answer
	c.qIdx++
	c.qPicked, c.qCursor = map[int]bool{}, 0
	if c.qIdx < len(qs) {
		return nil
	}
	in := map[string]any{}
	_ = json.Unmarshal(req.Input, &in)
	in["answers"] = c.qAnswer
	b, _ := json.Marshal(in)
	id := req.ID
	c.qFor = ""
	return hostCmd(func() error { return c.client.Allow(id, b, false) })
}

// searchKey handles keys while the message box is a search box.
func (m *Model) searchKey(c *hostConn, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+f":
		c.searching, c.query, c.sel = false, nil, ""
		return nil
	case "up", "down":
		m.moveSel(c, map[string]int{"up": -1, "down": 1}[s])
		return nil
	case "enter":
		if c.sel == "" && len(c.bodyRefs) > 0 {
			c.sel = c.bodyRefs[0]
		}
		if c.sel == "" {
			return nil
		}
		// Jump: back to the conversation with the match's turn opened and
		// the match selected.
		turn, _, isStep := strings.Cut(c.sel, ":")
		c.open[turn] = true
		if isStep {
			c.open[c.sel] = true
			if p := c.sess.ParentRef(c.sel); p != "" {
				c.open[p] = true // a subagent's step shows under its opened parent
			}
		}
		c.view, c.searching, c.query, c.selMoved = 0, false, nil, true
		return nil
	}
	buf, _, ok := edit(c.query, len(c.query), k, s)
	if ok && !strings.Contains(string(buf), "\n") {
		c.query, c.sel = buf, ""
	}
	return nil
}

// cardKind is the card waiting in the dock, if any.
func cardKind(c *hostConn) string {
	switch {
	case c.sess.Info.Limit != nil && c.sess.Info.Limit.Ask:
		return "limit"
	case isQuestion(c.sess.Pending()):
		return "question"
	case len(c.sess.Pending()) > 0:
		return "approval"
	}
	return ""
}

// cardKey answers a waiting card. Plain letters and digits answer only
// once ↑ has put the keys on the card, so typing a message that starts
// with "yes" or "1." can never answer by accident; alt+y, alt+a and alt+n
// answer from anywhere. It reports whether it used the key.
func (m *Model) cardKey(c *hostConn, s string, empty bool) (tea.Cmd, bool) {
	kind := cardKind(c)
	if kind == "" {
		c.cardFocus = false
		return nil, false
	}
	done := func(cmd tea.Cmd) (tea.Cmd, bool) {
		c.cardFocus = false
		return cmd, true
	}
	pending := c.sess.Pending()
	switch kind {
	case "limit":
		yes, no := s == "alt+y", s == "alt+n"
		if c.cardFocus {
			yes, no = yes || s == "y" || s == "enter", no || s == "n"
		}
		if yes || no {
			return done(hostCmd(func() error { return c.client.ContinueAtReset(yes) }))
		}
	case "approval":
		req := pending[0].Approval
		switch {
		case s == "alt+y" || c.cardFocus && (s == "y" || s == "enter"):
			return done(m.answerHost(c, req, true, false))
		case s == "alt+a" || c.cardFocus && s == "a":
			return done(m.answerHost(c, req, true, true))
		case s == "alt+n" || c.cardFocus && s == "n":
			return done(m.answerHost(c, req, false, false))
		}
	case "question":
		req := pending[0].Approval
		if c.cardFocus && s == "s" {
			id := req.ID
			return done(hostCmd(func() error {
				return c.client.Deny(id, "The user skipped the question; carry on with your best judgement.", false)
			}))
		}
		// Typed text and enter answer in your own words; digits pick only
		// while the card has the keys.
		if !empty && s == "enter" || c.cardFocus {
			if cmd, used := m.questionKey(c, req, s, empty); used {
				if cmd != nil {
					c.cardFocus = false
				}
				return cmd, true
			}
		}
	}
	switch {
	case !c.cardFocus && empty && s == "up" && c.sel == "":
		c.cardFocus = true
		return nil, true
	case c.cardFocus && (s == "esc" || s == "down"):
		c.cardFocus = false
		return nil, true
	case c.cardFocus:
		c.cardFocus = false // anything else goes back to typing
	}
	return nil, false
}

// cardHint is the last line of a card: its keys when it has focus, how to
// give it focus when not.
func cardHint(c *hostConn, keys string) string {
	if c.cardFocus {
		return paint(cOrange, "▸ ") + keys + dim("   ·   esc back to typing")
	}
	return dim("↑ to answer   ·   or alt+y alt+a alt+n from the box")
}

// isTurnRef is a turn's own row ("t12"), not one of its steps.
func isTurnRef(r string) bool {
	if len(r) < 2 || r[0] != 't' {
		return false
	}
	for _, c := range r[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
