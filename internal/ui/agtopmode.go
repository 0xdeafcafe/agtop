package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/fswait"
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
	v = append(v, "memory")
	if c.client == nil {
		if a := m.focused(); a != nil && liveCapable(a) {
			v = append(v, "screen")
		}
	}
	return v
}

// refreshSubs looks for new subagent runs and follows the one opened. The
// runs' rows need each one's numbers, read only while the subagents view is
// on screen (the running ones' while the conversation is): what has grown is read here, and runs never read yet are read
// in the background by the command it returns.
func (m *Model) refreshSubs() tea.Cmd {
	c := m.host
	if c == nil || c.path == "" {
		return nil
	}
	c.subs = c.subList.List(c.path)
	if c.subPeek != nil {
		_, _ = c.subPeek.Read()
	}
	m.readSub()
	// The conversation shows what each running one is doing, so those are
	// followed there too.
	follow := c.subs
	switch m.viewName(c) {
	case "subagents":
	case "conversation":
		follow = c.runningSubs()
	default:
		follow = nil
	}
	if len(follow) == 0 {
		return nil
	}
	if c.subTails == nil {
		c.subTails = map[string]*convo.Tail{}
	}
	var unread []convo.Subagent
	for _, sa := range follow {
		switch t := c.subTails[sa.ID]; {
		case t == nil:
			unread = append(unread, sa)
		case t.Size() != sa.Size:
			_, _ = t.Read()
		}
	}
	if len(unread) == 0 || c.subReading {
		return nil
	}
	c.subReading = true
	key := c.key
	return func() tea.Msg {
		read := make(map[string]*convo.Tail, len(unread))
		for i := len(unread) - 1; i >= 0; i-- { // newest first
			t := convo.SubagentStats(unread[i].Path)
			_, _ = t.Read()
			read[unread[i].ID] = t
		}
		return subStatsMsg{key: key, tails: read}
	}
}

// subStatsMsg brings subagent runs' numbers read in the background.
type subStatsMsg struct {
	key   string
	tails map[string]*convo.Tail
}

func (m *Model) onSubStats(msg subStatsMsg) {
	c := m.host
	if c == nil || c.key != msg.key {
		return
	}
	c.subReading = false
	for id, t := range msg.tails {
		if c.subTails[id] == nil {
			c.subTails[id] = t
		}
	}
}

// subDetail is the whole conversation of a run, for showing it beside the
// list: the opened run's, or one read now and kept while it's the one picked.
func (c *hostConn) subDetail(id string) *convo.Tail {
	if c.subTail != nil && c.subOpen == id {
		return c.subTail
	}
	if c.subPeek != nil && c.subPeekID == id {
		return c.subPeek
	}
	for _, sa := range c.subs {
		if sa.ID == id {
			c.subPeek, c.subPeekID = convo.SubagentTail(sa.Path), id
			_, _ = c.subPeek.Read()
			return c.subPeek
		}
	}
	return nil
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

type watched struct {
	path string
	size int64
}

// syncWatch keeps a watch on the transcripts the pane follows: a Claude
// Code session's own, and the subagent opened. The kernel says when one is
// written, so what Claude Code writes shows at once, and a quiet one costs
// nothing.
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
	files := make([]fswait.File, len(want))
	for i, w := range want {
		files[i] = fswait.File{Path: w.path, Size: w.size}
	}
	return func() tea.Msg {
		if fswait.Grown(stop, files) {
			return growMsg{key}
		}
		return nil
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
	// When its transcript last grew: known without reading it.
	var last time.Time
	if sa.Mod > 0 {
		last = time.Unix(0, sa.Mod)
	}
	status = c.sess.TaskStatus[sa.ID]
	st := c.sess.Step(sa.ToolUseID)
	if st != nil && st.Status == convo.Failed && status == "" {
		status = "failed"
	}
	// No word that it finished, and it wrote recently: still working.
	live = status == "" && !last.IsZero() && time.Since(last) < 90*time.Second
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
			c.subTail, c.subOpen, c.subSel, c.subBack = t, id, "", false
			c.sel, c.scroll = "", 0
			m.refreshSubs() // numbers for the list come on the next tick
			return
		}
	}
}

// watchingSub is whether the pane shows one subagent's own conversation.
func (m *Model) watchingSub(c *hostConn) bool {
	return c.subOpen != "" && c.subTail != nil && m.viewName(c) == "subagents"
}

// closeSub leaves an opened subagent for wherever it was opened from.
func (m *Model) closeSub(c *hostConn) {
	if c.subBack {
		// Watched from the dock: back to the conversation, on its row.
		c.view, c.sel = 0, "run:"+c.subOpen
	} else {
		c.sel = ""
	}
	c.subOpen, c.subTail, c.subBack = "", nil, false
}

// subBanner is the row pinned under the header while you watch a subagent,
// so it's plain whose conversation this is and how to get back.
func (m *Model) subBanner(c *hostConn, w int) string {
	if !m.watchingSub(c) {
		return ""
	}
	var sa convo.Subagent
	for _, x := range c.subs {
		if x.ID == c.subOpen {
			sa = x
		}
	}
	t := c.subTail.Sess
	status, live := c.subState(sa)
	now := time.Now()
	state := paint(cGreen, "✓ done")
	switch {
	case live:
		state = paint(cOrange, spinner[m.tick%len(spinner)]+" running")
	case status == "stopped":
		state = dim("⏹ " + status)
	case status == "failed":
		state = paint(cRed, "✗ failed")
	case status != "":
		state = dim(status)
	}
	end := t.Last
	if live {
		end = now
	}
	if !t.First.IsZero() && !end.IsZero() {
		state += dim(" " + dur(end.Sub(t.First).Round(time.Second)))
	}
	state += dim(fmt.Sprintf(" · %d steps", t.Totals(now).ToolCalls))
	back := "the list"
	if c.subBack {
		back = "the conversation"
	}
	left := paint(cBlue, "▍") + paint(cBlue+bold, "⇉ WATCHING SUBAGENT  ") + paint(cBright+bold, sa.Type) + "  " +
		paint(cSub, oneLine(sa.Description)) + "   " + state
	right := paint(cText, "esc") + dim(" back to "+back) + " "
	if c.subHover == "subback" {
		right = paint(cBright+bold, "esc") + paint(cText, " back to "+back) + " " // a click goes back too
	}
	// The right side stays; the description gives way.
	if lw := w - cellw.String(right) - 2; cellw.String(left) > lw {
		left = ansi.Truncate(paint(cBlue, "▍")+paint(cBlue+bold, "⇉ WATCHING SUBAGENT  ")+paint(cBright+bold, sa.Type)+"  "+state+"  "+paint(cSub, oneLine(sa.Description)), max(10, lw), "…")
	}
	return onBg(bgSub, spread(left, right, w), w)
}

// subPeekAfter is how long the pointer rests on a run before the wide
// subagents view shows its conversation beside the list.
const subPeekAfter = 300 * time.Millisecond

type subHoverMsg struct{}

// subHoverAt is what of the subagents view is under the pointer: a run's
// rows, or the banner that goes back from one being watched.
func (m *Model) subHoverAt(x, y int) string {
	c := m.host
	if c == nil || m.mode != modeList || m.dialog != nil || m.picker != nil || m.zen || m.embedded ||
		c.txt.drag || (m.listW > 0 && x <= m.listW+1) || m.viewName(c) != "subagents" {
		return ""
	}
	i := y - m.paneTop
	if i < 0 || i >= len(c.rowRefs) {
		return ""
	}
	r := c.rowRefs[i]
	// Wide, a run's conversation sits beside the list on its rows; only
	// the list itself is the run's.
	if strings.HasPrefix(r, "sub:") && c.subOpen == "" && c.paneW >= 150 && x-m.paneX() >= min(72, c.paneW*2/5) {
		return ""
	}
	if strings.HasPrefix(r, "sub:") || r == "subback" {
		return r
	}
	return ""
}

// subMouseMove follows the pointer over the subagents view. It says whether
// that changed what shows, and asks for a redraw once it has rested long
// enough for the run's conversation to show beside the list.
func (m *Model) subMouseMove(x, y int) (bool, tea.Cmd) {
	c := m.host
	if c == nil {
		return false, nil
	}
	r := m.subHoverAt(x, y)
	if r == c.subHover {
		return false, nil
	}
	c.subHover, c.subHoverAt = r, time.Now()
	if !strings.HasPrefix(r, "sub:") || c.subOpen != "" {
		return true, nil
	}
	return true, tea.Tick(subPeekAfter, func(time.Time) tea.Msg { return subHoverMsg{} })
}

// subagentLines is the subagents view: one row per run, or the opened
// run's own conversation under a breadcrumb.
func (m *Model) subagentLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	if c.subOpen != "" && c.subTail != nil {
		// The banner pinned above the body says whose this is.
		so := o
		so.Selected = c.sel
		return append([]convo.Line{{Text: ""}}, c.subTail.Sess.Render(so)...)
	}
	// Wide enough: the runs on the left, the picked one's conversation
	// beside them, following as it works.
	if w >= 150 && len(c.subs) > 0 {
		lw := min(72, w*2/5) // subHoverAt knows this too
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
		// A run the pointer rests on shows beside the list until it moves off.
		if h, ok := strings.CutPrefix(c.subHover, "sub:"); ok && time.Since(c.subHoverAt) >= subPeekAfter {
			id = h
		}
		var detail []convo.Line
		if t := c.subDetail(id); t != nil {
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

// noSession stands in for a run not read yet; it's only ever read.
var noSession = convo.New()

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
		r := row{sa: sa, t: noSession}
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
		case r.status == "stopped":
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
		} else if ref == c.subHover {
			lines = append(lines, convo.Line{Text: hoverLine(top, w), Ref: ref}, convo.Line{Text: hoverLine(second, w), Ref: ref})
		} else {
			lines = append(lines, convo.Line{Text: fit(top, w), Ref: ref}, convo.Line{Text: fit(second, w), Ref: ref})
		}
	}
	return lines
}

// dockRuns are the running subagents the dock shows, each one a row ↑ from
// the box can pick.
func (c *hostConn) dockRuns() []convo.Subagent {
	run := c.runningSubs()
	return run[:min(len(run), 3)]
}

// runningPreview is what the conversation shows of the subagents still
// working: a heading, then two lines each (three runs at most): what it was
// asked, and what it's doing right now after what it just did.
func (m *Model) runningPreview(c *hostConn, run []convo.Subagent, w int) []string {
	what := "subagent working"
	if len(run) > 1 {
		what = "subagents working"
	}
	shown := c.dockRuns()
	picked := strings.HasPrefix(c.sel, "run:")
	hint := ""
	switch {
	case picked:
		hint = keys("enter", "watch it")
	case m.paneFocus && len(c.input) == 0 && len(m.queueOf(c).items) == 0:
		hint = keys("↑", "pick one to watch")
	}
	out := []string{spread("  "+paint(cBlue, "⇉ ")+paint(cSub+bold, fmt.Sprintf("%d %s", len(run), what)), hint+"  ", w)}
	now := time.Now()
	for i, sa := range shown {
		var doing, trail string
		var facts []string
		if t := c.subTails[sa.ID]; t != nil {
			s := t.Sess
			if d, since := s.Doing(); d != "" {
				doing = paint(cText, d)
				if el := now.Sub(since); el >= 5*time.Second {
					doing += dim(" " + dur(el.Round(time.Second)))
				}
			} else {
				doing = dim("thinking…")
			}
			for _, d := range s.Did() {
				trail += faint("  ‹ " + d)
			}
			if trail == "" {
				if lw := s.LastWords(); lw != "" {
					trail = faint("  ‹ " + lw)
				}
			}
			facts = append(facts, fmt.Sprintf("%d steps", s.Totals(now).ToolCalls))
			if !s.First.IsZero() {
				facts = append(facts, dur(now.Sub(s.First).Round(time.Second)))
			}
		} else {
			doing = dim("starting…")
		}
		right := dim(strings.Join(facts, " · ")) + "  "
		top := spread("    "+paint(cOrange, spinner[(m.tick+i)%len(spinner)])+" "+paint(cText+bold, sa.Type)+
			"  "+paint(cSub, ansi.Truncate(oneLine(sa.Description), max(12, w-cellw.String(ansi.Strip(right))-cellw.String(sa.Type)-10), "…")), right, w)
		act := ansi.Truncate("      "+paint(cOrange, "›")+" "+doing+trail, w-2, "…")
		if c.sel == "run:"+sa.ID {
			top, act = picked1(top, w, m.paneFocus), picked1(act, w, m.paneFocus)
		}
		out = append(out, top, act)
	}
	if rest := len(run) - len(shown); rest > 0 {
		out = append(out, dim(fmt.Sprintf("      … %d more in the subagents view", rest)))
	}
	return out
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
	editHeld bool   // editing held the queue, to let go once it's saved
	slashSel int    // the slash-command picker's selection
	pastes   pastes // long pastes shown as chips
	arts     []*artifact
	marks    map[string]bool // files marked reviewed in the changes view
	bodyBuf  []convo.Line    // the conversation\'s lines, reused frame to frame
	artsKey  string
	mem      []memFile // the memory view's files, and when they were read
	memAt    time.Time
	memTop   int                // the first of them in view
	memEd    *docEditor         // the picked file, open in the editor below them
	memEdit  bool               // the editor has the keys
	local    []headless.Command // custom commands and skills on disk
	skills   map[string]bool
	// cardFocus is set when ↑ has moved the keys from the box onto a card
	// waiting for an answer; only then do plain letters and digits answer.
	cardFocus bool

	// Answering Claude's questions, one at a time.
	qFor      string
	qIdx      int
	qCursor   int                  // the option ↑↓ is on while the card has the keys
	qPicks    map[int]map[int]bool // ticked options, by question
	qAnswer   map[string]string
	stopArmed time.Time
	lastSend  time.Time
	sending   []sending     // sent, and not yet seen to arrive
	coldOK    time.Time     // the cold cache you said to send to anyway
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
	// top is the row at the top of the window when scrolled up, so output
	// arriving below (a replay, an agent at work) doesn't carry you down.
	top struct {
		ref, view   string
		off, scroll int
	}
	bodyRefs []string // every selectable row of the current view, in order
	// Text dragged over with the mouse. rowBody is which row of shown each
	// drawn row of the pane is (-1 for the header, pill and dock), and
	// bodyTop and bodyRows where the body sits among them.
	txt      textSel
	shown    []convo.Line
	rowBody  []int
	bodyTop  int
	bodyRows int
	paneW    int

	// Subagents: every run found beside the transcript, and the one opened.
	path       string
	subs       []convo.Subagent
	subTail    *convo.Tail
	subTails   map[string]*convo.Tail // every run, followed for its row's numbers only
	subBack    bool                   // the open run was picked in the dock: ← goes back there
	subPeek    *convo.Tail            // the run picked in the list, in full, shown beside it
	subPeekID  string
	subReading bool // runs' numbers are being read in the background
	subOpen    string
	subList    convo.Subagents // finds the runs, reading each one's meta once
	subSel     string          // selection inside the opened subagent
	subHover   string          // the run under the pointer, or "subback" for the banner
	subHoverAt time.Time
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
	key, id, path, acct := a.Key, a.ID, a.TranscriptPath, a.Acct
	return func() tea.Msg {
		cl, err := host.Dial(id)
		if err != nil {
			return hostOpenMsg{key: key, err: err}
		}
		// The transcript up to where the host's replay begins, then the
		// replay: a session that took over an existing conversation shows
		// it, and a long one whose replay was trimmed shows all of it.
		sess := convo.New()
		info, infoErr := host.ReadInfo(id)
		if infoErr == nil && info.SessionID != "" && info.Cwd != "" {
			// The list may not have caught up with a rewind yet.
			path = acct.TranscriptPath(info.Cwd, info.SessionID)
		}
		trimmed := infoErr == nil && !info.ReplayFrom.IsZero()
		if cfg, err := host.ReadConfig(id); err == nil && (cfg.Resume || trimmed) {
			started := time.Now()
			if infoErr == nil && !info.StartedAt.IsZero() {
				started = info.StartedAt
				for _, t := range []time.Time{info.RewoundAt, info.ReplayFrom} {
					if t.After(started) {
						started = t
					}
				}
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
	if c := m.host; c != nil && (len(c.sess.Turns) > 40 || len(c.subTails) > 20) {
		// A big conversation was just let go: hand its memory back now
		// rather than whenever the collector gets round to it.
		freeSoon()
	}
	m.host, m.hostOpening = nil, ""
}

var freeing atomic.Bool

// freeSoon returns unused memory to the system in the background, at most
// one at a time.
func freeSoon() {
	if freeing.CompareAndSwap(false, true) {
		go func() {
			defer freeing.Store(false)
			time.Sleep(100 * time.Millisecond) // let the frame that dropped it finish
			debug.FreeOSMemory()
		}()
	}
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
	if d, ok := m.rewound[msg.key]; ok && m.host.client != nil {
		delete(m.rewound, msg.key)
		m.host.input, m.host.back = []rune(d), 0
		m.paneFocus = true
	}
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
	bgSub    = "\x1b[48;2;24;31;42m" // watching a subagent: its own, cooler ground
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
	var head []string
	if !m.zenFull() {
		head = m.paneHeader(a, c, w) // zen is only the agent and its box
	}
	if l := m.subBanner(c, w); l != "" {
		head = append(head, l)
	}
	dock := m.paneDock(a, c, w, h)
	bodyH := max(3, h-len(head)-len(dock))

	o := convo.Options{Width: w, Now: time.Now(), Tick: m.tick, Open: c.open, Verbose: c.verbose,
		Selected: c.sel, Focused: m.paneFocus}
	var body []convo.Line
	view := m.viewName(c)
	if m.zen {
		view = "zen"
		body = m.zenBody(a, c, w)
	}
	switch view {
	case "zen":
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
	case "memory":
		body = m.memoryLines(c, o, bodyH)
	case "subagents":
		if c.subOpen != "" {
			o.Selected = c.subSel
		}
		body = m.subagentLines(c, o)
	default:
		body = s.RenderInto(o, c.bodyBuf)
		c.bodyBuf = body
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
	// Scrolled up and not moved since the last draw: keep the same row at
	// the top, whatever was added below or above it.
	if t := c.top; c.scroll > 0 && c.scroll == t.scroll && t.view == view && t.ref != "" && !c.selMoved {
		for i, l := range body {
			if l.Ref == t.ref {
				rows := bodyH - 1 // scrolled up, the pill takes a row
				c.scroll = len(body) - (i + t.off + rows)
				break
			}
		}
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
	// A turn's heading (your message) can take several rows. A window
	// starting inside one shows it from its first row instead of cutting
	// its top off.
	if m.viewName(c) == "conversation" && start > 0 && isTurnRef(body[start].Ref) {
		f := headingStart(body, start)
		if f < start && start-f <= rows/2 {
			start, end = f, min(len(body), f+rows)
		}
	}
	c.top.ref, c.top.view, c.top.scroll = "", view, c.scroll
	if c.scroll > 0 {
		// The first row shown that belongs to something, and how far the
		// window's top (before a heading moved it) is from that thing's
		// first row.
		for i := start; i < end && c.top.ref == ""; i++ {
			c.top.ref = body[i].Ref
		}
		for i, l := range body {
			if c.top.ref != "" && l.Ref == c.top.ref {
				c.top.off = len(body) - c.scroll - rows - i
				break
			}
		}
	}
	out := append([]string{}, head...)
	c.rowRefs = make([]string, len(head), h)
	if m.watchingSub(c) {
		c.rowRefs[len(head)-1] = "subback" // clicking the banner goes back
	}
	c.rowBody = c.rowBody[:0]
	for range head {
		c.rowBody = append(c.rowBody, -1)
	}
	for i, l := range body[start:end] {
		out = append(out, l.Text)
		c.rowRefs = append(c.rowRefs, l.Ref)
		c.rowBody = append(c.rowBody, start+i)
	}
	c.shown, c.bodyTop, c.bodyRows, c.paneW = body, len(head), end-start, w
	if c.txt.view != view && !c.txt.drag {
		c.txt = textSel{} // made in another view, whose rows these aren't
	}
	// Scrolled into a turn whose heading is off the top: pin the heading
	// there, so you always know whose turn you're reading.
	if m.viewName(c) == "conversation" && start > 0 && end > start {
		for i := start; i >= 0; i-- {
			if r := body[i].Ref; isTurnRef(r) {
				if i < start && !isTurnRef(body[start].Ref) {
					// Its first row: the start of what you said.
					f := headingStart(body, i)
					out[len(head)] = body[f].Text
					c.rowRefs[len(head)] = r
					c.rowBody[len(head)] = f
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
	c.paintSel(out)
	for len(out) < h-len(dock) {
		out = append(out, "")
	}
	c.boxY = m.paneTop + len(out) + c.boxIdx
	return append(out, dock...)
}

// headingStart is the first row of the turn heading that row i is part of.
func headingStart(body []convo.Line, i int) int {
	for i > 0 && body[i-1].Ref == body[i].Ref {
		i--
	}
	return i
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
	// Filling the screen, it's the only thing there to name, and what's on
	// the right lines up with the conversation's own right edge.
	alone := m.paneAlone()
	hw := w
	if alone {
		hw = min(w, maxPane-3)
	}
	title := faint("SESSION  ")
	switch {
	case alone:
		title = ""
	case m.paneFocus:
		title = paint(cOrange+bold, "SESSION  ")
	}
	// The rest is the agent header you build in /statusline: its first
	// line right of the name, its second under it.
	x := &barCtx{m: m, a: a, c: c}
	left1 := mark + title + paint(cBright+bold, oneLine(a.DisplayName)) + "   " + state
	right := m.barLine(barAgent, 0, x, hw-cellw.String(left1)-4)
	row1 := spread(left1, right+" ", hw)

	conn := paint(cGreen, "●") + dim(" connected")
	if c.client == nil {
		switch {
		case a.Agtop:
			conn = dim("stopped · a message resumes it")
		case a.Past:
			conn = dim("past conversation · a message resumes it in agtop mode")
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
	meta := m.barLine(barAgent, 1, x, hw-cellw.String(conn)-6)
	row2 := spread("  "+meta, conn+" ", hw)

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
	if alone {
		// Nothing says the list is behind it but this.
		chips += paint(cText, "esc") + dim(" back to the list") + " "
	}
	if m.viewName(c) == "screen" {
		chips = dim("typing goes into it · ctrl+] comes back · ctrl+f full screen") + "  "
	}
	row3 := spread("  "+strings.Join(tabs, " ")+dim("   [ ]"), chips, hw)
	// The chrome's own background marks it off; no half-block edge.
	return []string{onBg(bgChrome, row1, w), onBg(bgChrome, row2, w), onBg(bgChrome, row3, w)}
}

// paneAlone is when the Session fills the screen with no list beside it.
func (m *Model) paneAlone() bool {
	return m.listW == 0 && !m.zenFull()
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
func (m *Model) paneDock(a *fleet.Agent, c *hostConn, w, h int) []string {
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
		out = append(out, m.questionCard(c, p[0].Approval, w, h*3/5)...)
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
		for _, l := range m.runningPreview(c, run, w) {
			line(l)
		}
	}
	for _, l := range m.sendingLines(c, w) {
		line(l)
	}
	if qs := m.queueOf(c); len(qs.items) > 0 {
		q := qs.items
		when := dim(" · sends when this turn ends")
		if c.client == nil {
			when = dim(" · sends within 15s, or when it's idle")
		}
		if lq := m.localQ[c.key]; c.client == nil && lq != nil && time.Now().Before(lq.retry) {
			when = paint(cYellow, " · send failed, trying again in "+dur(time.Until(lq.retry).Round(time.Second)))
		}
		if qs.held {
			// Never silently stuck: say it's held and how it goes.
			when = paint(cYellow, " · held") + dim(" · ctrl+s sends it now")
		}
		inView := m.viewName(c) == "queue"
		pick, picked := queueSel(c, len(q))
		var how string
		if m.paneFocus && !picked && len(c.input) == 0 && !inView {
			how = keys("↑", "edit or reorder", "ctrl+s", "send now") + "  "
		}
		line(spread("  "+paint(cSub+bold, fmt.Sprintf("queue %d", len(q)))+when, how, w))
		if !inView {
			// Three at a time, keeping the picked one in sight.
			start := 0
			if picked && pick >= 3 {
				start = pick - 2
			}
			if start > 0 {
				line(dim(fmt.Sprintf("   … %d before", start)))
			}
			for i := start; i < min(len(q), start+3); i++ {
				row := "   " + dim(fmt.Sprint(i+1)) + "  " + paint(cSub, ansi.Truncate(shortImages(oneLine(q[i])), w-8, "…"))
				if picked && i == pick {
					row = picked1(row, w, m.paneFocus)
				}
				line(row)
			}
			if rest := len(q) - (start + 3); rest > 0 {
				line(dim(fmt.Sprintf("   … %d more", rest)))
			}
		}
	}
	top := dim("to ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 28, "…"))
	if m.watchingSub(c) {
		// What you type goes to the main session; subagents take no messages.
		top = dim("to the main session, ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 28, "…")) + dim(", not the subagent")
	}
	switch {
	case typingHash(string(c.input)):
		top = dim("agtop command for ") + paint(cText, ansi.Truncate(oneLine(a.DisplayName), 28, "…")) + dim(" · enter runs it")
	case c.client == nil && a.Interactive:
		top += dim(" · " + a.Where() + ", so it can't take messages here")
	case c.client == nil && a.Past:
		top += dim(" · ") + paint(cOrange, "enter resumes it") + dim(" in agtop mode with your message")
	case c.client == nil && a.Agtop:
		top += dim(" · stopped; ") + paint(cOrange, "enter resumes it") + dim(" with your message")
	case c.client == nil && busy(a):
		top += dim(" · working, so ") + paint(cOrange, "enter queues")
		if len(c.input) > 0 {
			top += dim(" · ctrl+s sends it now")
		}
	case c.client == nil:
		top += dim(" · enter replies through Claude Code")
	case isQuestion(s.Pending()):
		top += dim(" · ") + paint(cYellow, "pick above, or type your own answer · enter sends it")
	case len(s.Pending()) > 0:
		top += dim(" · ") + paint(cYellow, "answer the card first, or type a note")
	case s.Live() != nil:
		top += dim(" · working, so ") + paint(cOrange, "enter queues")
		if len(c.input) > 0 {
			top += dim(" · ctrl+s sends it now")
		}
	default:
		top += dim(" · enter sends")
	}
	typing := m.paneFocus && c.sel == "" && !c.cardFocus
	switch {
	case m.paneFocus && c.cardFocus:
		top = dim("answering the card above · esc returns here")
	case m.paneFocus && c.memEdit:
		top = dim("editing " + filepath.Base(c.memEd.path) + " above · esc returns here")
	case m.paneFocus && !typing:
		top = dim("typing returns here · ↓ past the last row or esc")
	}
	b := box{w: w, focused: typing, topL: top, text: c.input, cursor: max(0, len(c.input)-c.back), anchor: c.anchor - 1,
		lead: paint(cOrange, "❯ "), holder: "a message for this agent", maxRows: 6}
	if mode := s.Info.PermissionMode; mode != "" {
		b.topR = paint(cOrange, mode)
	}
	pick := -1
	if i, ok := imageSel(c); ok {
		pick = i
	}
	if l := chips(c.images, w, pick, m.paneFocus); l != "" {
		out = append(out, onBg(bgChrome, l, w))
	}
	out = append(out, m.slashLines(c, w)...)
	if c.editQ > 0 {
		b.topL = paint(cOrange, fmt.Sprintf("editing queued message %d", c.editQ)) + dim(" · enter saves it back · esc cancels")
	}
	c.box, c.boxIdx = b, len(out)
	out = append(out, b.lines()...)
	hint := keysFit(w-4, "enter", "send", "ctrl+f", "find in chat", "esc · ←", "back to the list", "↑", "pick a step", "[ ]", "views", "ctrl+o", "show all", "ctrl+x", "stop turn")
	if m.watchingSub(c) {
		back := "back to the list"
		if c.subBack {
			back = "back to the conversation"
		}
		hint = keysFit(w-4, "enter", "send to the main session", "esc · ←", back, "↑", "pick a step", "ctrl+f", "find in chat", "ctrl+o", "show all")
		if _, live, _ := m.pickedSub(c); live {
			hint = keysFit(w-4, "enter", "send to the main session", "ctrl+x", "stop this subagent", "esc · ←", back, "↑", "pick a step", "ctrl+f", "find in chat")
		}
	}
	if c.sel != "" {
		hint = keysFit(w-4, "enter · space", "open or close", "↑↓", "pick a step", "esc", "done picking", "ctrl+o", "show all")
		if selTurn(c) != nil {
			hint = keysFit(w-4, "alt+r", "rewind to here", "alt+f", "fork from here", "enter · space", "open or close", "↑↓", "pick", "esc", "done picking")
		}
		if strings.HasPrefix(c.sel, "run:") {
			hint = keysFit(w-4, "enter", "watch this subagent", "x", "stop it", "↑↓", "pick", "esc", "done picking")
		}
		if _, live, ok := m.pickedSub(c); ok && live && strings.HasPrefix(c.sel, "sub:") {
			hint = keysFit(w-4, "enter", "watch it", "x", "stop it", "↑↓", "pick", "esc", "done picking")
		}
		if strings.HasPrefix(c.sel, "img:") {
			hint = keysFit(w-4, "⌫", "take it off the message", "↑↓", "pick", "esc", "done picking")
		}
		if strings.HasPrefix(c.sel, "mem:") {
			hint = keysFit(w-4, "enter", "edit it here", "ctrl+g", "open in $EDITOR", "x", "delete", "↑↓", "pick", "esc", "done picking")
		}
		if c.memEdit {
			hint = m.docHint(c.memEd, w-4)
		}
	} else if m.viewName(c) == "memory" && len(c.input) == 0 {
		hint = keysFit(w-4, "↑↓", "pick a file", "[ ]", "views")
	}
	if qs := m.queueOf(c); len(qs.items) > 0 {
		if _, ok := queueSel(c, len(qs.items)); ok {
			hint = queueHint(qs, w-4)
		} else if m.viewName(c) == "queue" && len(c.input) == 0 {
			hint = keysFit(w-4, "↑↓", "pick a message", "ctrl+s", "send it all now", "[ ]", "views")
		}
	}
	if !m.paneFocus {
		hint = keysFit(w-4, "enter · →", "type here", "ctrl+n", "next needing you")
		b.holder = "enter or → to talk to this agent"
		out = append(out[:len(out)-len(b.lines())], b.lines()...)
	}
	if m.zenFull() {
		return out
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
	empty := len(c.input) == 0
	if s == "alt+c" && empty {
		// With nothing typed, alt+c copies the drawing you've picked.
		if i := strings.LastIndex(c.sel, ":s:"); i >= 0 {
			if rows := convo.Drawing(c.sess.Step(c.sel[i+3:])); rows != nil {
				m.copyText(strings.Join(rows, "\n"))
				return nil
			}
		}
		// On a turn, it copies Claude's answer as written, markdown and all.
		if t, _, _ := strings.Cut(c.sel, ":"); isTurnRef(t) {
			for _, turn := range c.sess.Turns {
				if "t"+strconv.Itoa(turn.N) == t {
					if a := turn.Answer(); a != "" {
						m.copyText(strings.TrimSpace(a))
						return nil
					}
				}
			}
		}
	}
	if (s == "alt+r" || s == "alt+f") && empty {
		// On a turn in the history: rewind back to it, or fork from it.
		if t := selTurn(c); t != nil {
			if a := m.agentByKey(c.key); a != nil {
				if s == "alt+f" {
					m.openForkAt(c, a, t)
					return nil
				}
				return m.openRewindTo(c, a, t)
			}
		}
	}
	if cmd, used := m.slashKey(c, s); used {
		return cmd
	}
	if cmd, used := m.queueKey(c, s); used {
		return cmd
	}
	if m.imageKey(c, s) {
		return nil
	}
	// ctrl+x on a subagent, picked or watched, stops that one alone; x
	// does too on its row.
	if s == "ctrl+x" || s == "x" && empty && !m.watchingSub(c) {
		if sa, live, ok := m.pickedSub(c); ok && (live || s == "x") {
			return m.stopSub(c, sa, live)
		}
	}
	if cmd, used := m.memoryKey(c, k, s); used {
		return cmd
	}
	if s == "esc" && c.editQ > 0 {
		c.input, c.back = c.input[:0], 0
		return m.endQueueEdit(c)
	}
	if cmd, used := m.cardKey(c, s, empty); used {
		return cmd
	}
	if (s == "backspace" || s == "ctrl+h") && empty && len(c.images) > 0 {
		c.images = c.images[:len(c.images)-1]
		return nil
	}
	if s == "ctrl+v" {
		return pasteClipImage()
	}
	switch s {
	case "esc":
		switch {
		case !empty:
			c.input, c.back = c.input[:0], 0
		case c.txt.on:
			c.txt = textSel{} // first esc drops the dragged-over text
		case c.sel != "":
			c.sel, c.subSel = "", "" // first esc drops the step selection
		case m.watchingSub(c):
			m.closeSub(c)
		case m.zen:
			// Zen keeps the keys on the agent; tab or ctrl+z leaves zen.
		default:
			m.leavePane()
		}
		return nil
	case "ctrl+c":
		switch {
		case c.anchor > 0 && c.anchor-1 != len(c.input)-c.back:
			// a selection: copy it (handled by the editor below)
		case empty && c.txt.on:
			// Text dragged over in the conversation: copy it again, as a
			// terminal would, rather than quitting.
			m.copyText(selectedText(c.shown, c.txt.a, c.txt.b, c.paneW))
			return nil
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
		// Enter on a running subagent in the dock watches it.
		if id, ok := strings.CutPrefix(c.sel, "run:"); ok && empty {
			for i, v := range m.views(c) {
				if v == "subagents" {
					c.view = i
				}
			}
			m.openSub(c, id)
			c.subBack = true
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
		// Now, whatever's waiting: the queue, then what's in the box.
		if !empty || len(c.images) > 0 {
			return m.sendPane(c, true)
		}
		if len(m.queueOf(c).items) > 0 {
			return m.sendQueueNow(c, "")
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
			case m.watchingSub(c):
				m.closeSub(c)
			case c.sel != "":
				c.sel = ""
			case m.zen:
				// Zen keeps the keys on the agent; tab or ctrl+z leaves zen.
			default:
				m.leavePane()
			}
			return nil
		}
	case "alt+d":
		// Done with this agent: to Done, its idle process stopped.
		return m.markDone(m.agentByKey(c.key))
	case "alt+h":
		// Hold or release the queue from any view, as the dock says.
		c.editHeld = false // yours now, not the edit's
		return m.holdQueue(c, !m.queueOf(c).held)
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
	case "shift+left", "shift+right", "alt+left", "alt+right":
		if empty {
			if cmd, ok := m.stepSplit(strings.HasSuffix(s, "right")); ok {
				return cmd
			}
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
			c.back, c.anchor = len(c.input)-pos, pos+1 // a drag from here selects
			m.boxDrag = 1
			return true
		}
	}
	b := m.promptBox
	if m.zenFull() || b.w == 0 {
		return false
	}
	rows := len(b.lines()) - 2
	if y > m.promptBoxY && y <= m.promptBoxY+rows && x < b.w {
		m.paneFocus, m.embedded = false, false
		pos := b.at(y-m.promptBoxY-1, x)
		m.setCursor(pos)
		m.anchor = pos + 1 // a drag from here selects
		m.boxDrag = 2
		return true
	}
	return false
}

// dragBox moves the cursor of the box a drag started in to the text under
// the pointer, so the text between it and where the drag began is selected.
// Past the box's edges it stops at the box's first or last row, and never
// takes in anything drawn outside it.
func (m *Model) dragBox(x, y int) {
	switch c := m.host; {
	case m.boxDrag == 1 && c != nil:
		x0 := 2
		if m.listW > 0 {
			x0 = m.listW + 3
		}
		pos := c.box.near(y-c.boxY-1, x-x0)
		c.back = len(c.input) - min(pos, len(c.input))
	case m.boxDrag == 2:
		m.setCursor(min(m.promptBox.near(y-m.promptBoxY-1, x), len(m.input)))
	}
}

// endBoxDrag finishes a drag in an input box: what it selected goes to the
// clipboard, as a terminal's own selection would.
func (m *Model) endBoxDrag() {
	var buf []rune
	var pos, anchor int
	switch c := m.host; {
	case m.boxDrag == 1 && c != nil:
		buf, pos, anchor = c.input, len(c.input)-c.back, c.anchor-1
	case m.boxDrag == 2:
		buf, pos, anchor = m.input, m.cursorPos(), m.anchor-1
	}
	drag := m.boxDrag
	m.boxDrag = 0
	if anchor >= 0 && anchor != pos && anchor <= len(buf) {
		m.copyText(string(buf[min(anchor, pos):max(anchor, pos)]))
		return
	}
	// A plain click leaves no selection behind.
	if drag == 1 && m.host != nil {
		m.host.anchor = 0
	} else if drag == 2 {
		m.anchor = 0
	}
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
	// Claude Code sessions are typed into, where tags would show as typed.
	text = strings.TrimSpace(c.pastes.expand(text, c.client != nil || m.agentByKey(c.key) == nil || m.agentByKey(c.key).Agtop))
	c.pastes = pastes{}
	if c.editQ > 0 {
		i, was := c.editQ-1, c.editWas
		c.input, c.back = c.input[:0], 0
		if c.client == nil {
			m.editLocal(c, i, was, text)
			return m.endQueueEdit(c)
		}
		if items := c.sess.Info.Queue; i < len(items) && items[i] == was {
			items[i] = text
		}
		// Saved before the queue is let go, so the old text never goes.
		cl, release := c.client, c.editHeld
		c.editQ, c.editHeld = 0, false
		if release {
			c.sess.Info.QueueHeld = false
		}
		return hostCmd(func() error {
			err := cl.EditQueued(i, was, text)
			if release {
				err = errors.Join(err, cl.HoldQueue(false))
			}
			return err
		})
	}
	if isHashCmd(text) {
		c.input, c.back = c.input[:0], 0
		return m.command(m.agentByKey(c.key), text)
	}
	if strings.HasPrefix(text, "/") {
		if cmd, ok := m.runAgtopCommand(c, text); ok {
			c.input, c.back = c.input[:0], 0
			return cmd
		}
	}
	if m.askCold(c, text, func() tea.Cmd { return m.sendPane(c, now) }) {
		return nil
	}
	images := c.images
	// Paths typed or dropped without a paste become attachments too.
	if rest, imgs := extractImages(text); imgs != nil {
		images, text = append(images, imgs...), rest
	}
	c.input, c.back, c.images = c.input[:0], 0, nil
	if strings.HasPrefix(c.sel, "img:") {
		c.sel = ""
	}
	c.scroll = 0
	c.lastSend = time.Now()
	if a := m.focused(); a != nil {
		m.markSeen(a)
	}
	if now && len(images) == 0 && len(m.queueOf(c).items) > 0 {
		return m.sendQueueNow(c, text)
	}
	if c.client == nil {
		return m.sendOffline(c, text, images, now)
	}
	m.markSending(c, text)
	cl := c.client
	switch {
	case len(images) > 0:
		return sendingVia(c.key, hostCmd(func() error { return cl.SendImages(text, images) }))
	case now:
		return sendingVia(c.key, hostCmd(func() error { return cl.SendNow(text) }))
	}
	return sendingVia(c.key, hostCmd(func() error { return cl.Send(text) }))
}

// coldMin is the context, in tokens, below which re-reading it uncached
// isn't worth asking about.
const coldMin = 40_000

// askCold asks before a message you send wakes a session whose prompt cache
// has expired, since it re-reads the whole context uncached; y runs send.
// It's only asked where you pressed send: queued messages, retries, a
// limit's reset and other programs' sends never ask.
func (m *Model) askCold(c *hostConn, text string, send func() tea.Cmd) bool {
	if c == nil || c.sess == nil || text == "/clear" {
		return false
	}
	at, cold := c.sess.CacheCold(time.Now())
	if !cold || at.Equal(c.coldOK) || c.sess.Context > 0 && c.sess.Context < coldMin {
		return false
	}
	if a := m.agentByKey(c.key); a != nil && busy(a) || c.sess.Info.State == "working" {
		return false // the turn under way keeps it warm
	}
	what := "the whole context"
	if c.sess.Context > 0 {
		what = convo.Tokens(c.sess.Context) + " tokens of context"
	}
	m.confirm = &confirmation{
		question: "Send to a cold cache?",
		detail:   fmt.Sprintf("its prompt cache expired %s ago · this re-reads %s uncached", dur(time.Since(at)), what),
		onYes: func() tea.Cmd {
			c.coldOK = at
			return send()
		},
	}
	return true
}

func (m *Model) isOpen(c *hostConn, ref string) bool {
	if v, ok := c.open[ref]; ok {
		return v
	}
	if strings.Contains(ref, ":s:") {
		return c.sess.StepOpen(ref, c.drewConvo && c.drawn.Verbose)
	}
	// What the renderer opens by default: recent turns and failures. The
	// options the pane last drew with find it in the renderer's cache;
	// any others would redraw every turn, twice.
	o := convo.Options{Width: max(40, m.w/2), Now: time.Now(), Open: c.open}
	if c.drewConvo {
		o = c.drawn
	}
	// Folded if any of its rows shows ▸ (a failed step's shows on the error
	// line under it).
	seen := false
	for _, l := range c.sess.Render(o) {
		if l.Ref == ref {
			seen = true
			if strings.Contains(ansi.Strip(l.Text), "▸") {
				return false
			}
		} else if seen {
			break
		}
	}
	return seen
}

// dockRefs are the rows ↑↓ go through between the conversation and the
// box, top to bottom as they're drawn: under the conversation the running
// subagents and the queue, then in any view the attachments. A card
// waiting sits above them all.
func (m *Model) dockRefs(c *hostConn) []string {
	var refs []string
	if m.viewName(c) == "conversation" {
		for _, sa := range c.dockRuns() {
			refs = append(refs, "run:"+sa.ID)
		}
		for i := range m.queueOf(c).items {
			refs = append(refs, fmt.Sprintf("q:%d", i))
		}
	}
	for i := range c.images {
		refs = append(refs, fmt.Sprintf("img:%d", i))
	}
	return refs
}

// imageSel is the attachment picked, if one is.
func imageSel(c *hostConn) (int, bool) {
	var i int
	if _, err := fmt.Sscanf(c.sel, "img:%d", &i); err != nil || i < 0 || i >= len(c.images) {
		return 0, false
	}
	return i, true
}

// imageKey acts on a picked attachment: ⌫ takes it off the message. It
// reports whether it used the key.
func (m *Model) imageKey(c *hostConn, s string) bool {
	i, ok := imageSel(c)
	if !ok {
		return false
	}
	switch s {
	case "backspace", "delete", "ctrl+h", "x":
		c.images = slices.Delete(c.images, i, i+1)
		// The pick stays where it was, on the next one; none left, it's
		// back to typing.
		c.sel = ""
		if len(c.images) > 0 {
			c.sel = fmt.Sprintf("img:%d", min(i, len(c.images)-1))
		}
		return true
	}
	return false
}

// moveSel moves the selection over rows you can act on: turns and steps,
// then what's between them and the box. ↑ from the box goes up through
// them nearest first; ↓ from it has nowhere to go.
func (m *Model) moveSel(c *hostConn, d int) {
	refs := append(slices.Clip(c.bodyRefs), m.dockRefs(c)...)
	if len(refs) == 0 {
		return
	}
	cur := c.sel
	if c.subOpen != "" && m.viewName(c) == "subagents" {
		cur = c.subSel
	}
	if cur == "" && d > 0 {
		return
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
	if ref == "subback" {
		m.closeSub(c)
		return
	}
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
func (m *Model) sendOffline(c *hostConn, text string, images []string, now bool) tea.Cmd {
	a := m.agentByKey(c.key)
	switch {
	case a == nil:
		return nil
	case a.Interactive:
		m.flash(a.DisplayName+" is "+a.Where()+"; agtop can't send to it", true)
		return nil
	case a.Past:
		return m.moveToAgtopWith(a, withImages(text, images))
	case a.Agtop:
		cfg, err := host.ReadConfig(a.ID)
		if err != nil {
			cfg = host.Config{ID: a.ID, SessionID: a.SessionID, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName}
		}
		cfg.Resume, cfg.Prompt, cfg.Images = true, text, images
		cfg.Lean, cfg.IdleStop = m.store.Config.Dispatch.Lean, host.Duration(m.store.Config.Dispatch.Rest())
		m.flash("resuming "+a.DisplayName+"…", false)
		m.markSending(c, text)
		return sendingVia(c.key, func() tea.Msg {
			if _, err := host.Spawn(cfg); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "resumed " + a.DisplayName}
		})
	}
	text = withImages(text, images)
	if !now && (busy(a) || len(m.queueOf(c).items) > 0) {
		// It's working: the message waits in the queue and goes when it
		// is idle, together with anything else waiting.
		m.queueLocal(a.Key, text)
		m.flash(fmt.Sprintf("queued · %d waiting · goes to %s within 15s", len(m.localQ[a.Key].items), a.DisplayName), false)
		return nil
	}
	m.loader.Nudge(a.Key)
	m.markSeen(a)
	m.markSending(c, text)
	return sendingVia(c.key, reply(a, text))
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
	cfg.Lean, cfg.IdleStop = m.store.Config.Dispatch.Lean, host.Duration(m.store.Config.Dispatch.Rest())
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
	name := sessionName(convo.FoldPastes(text))
	if name == "" && len(images) > 0 {
		name = "about " + filepath.Base(images[0])
	}
	if name == "" {
		name = "fresh session in " + filepath.Base(dir)
	}
	cfg := host.Config{
		Account: m.store.Config.ActiveAccount(), Cwd: dir, Prompt: text, Images: images, Name: name,
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission, LimitMode: d.OnLimit, Lean: d.Lean, IdleStop: host.Duration(d.Rest()),
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
func (m *Model) moveToAgtop(a *fleet.Agent) tea.Cmd { return m.moveToAgtopWith(a, "") }

// moveToAgtopWith moves it over with prompt as the first message there.
func (m *Model) moveToAgtopWith(a *fleet.Agent, prompt string) tea.Cmd {
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
		SessionID: a.SessionID, Resume: true, Prompt: prompt, Account: a.Acct, Cwd: a.Cwd, Name: a.DisplayName,
		Model: d.Model, Effort: d.Effort, PermissionMode: d.Permission, LimitMode: d.OnLimit, Lean: d.Lean, IdleStop: host.Duration(d.Rest()),
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

type question = headless.Question

func isQuestion(p []*convo.Step) bool {
	return len(p) > 0 && p[0].Approval != nil && p[0].Approval.Tool == "AskUserQuestion"
}

func questions(req *headless.PermissionRequest) (title string, qs []question) { return req.Questions() }

// syncQuestion resets the answering state when a new question arrives.
func (c *hostConn) syncQuestion(req *headless.PermissionRequest) {
	if c.qFor != req.ID {
		c.qFor, c.qIdx, c.qCursor, c.qPicks, c.qAnswer = req.ID, 0, 0, map[int]map[int]bool{}, map[string]string{}
	}
}

// picks is what's ticked on a multi-select question.
func (c *hostConn) picks(i int) map[int]bool {
	if c.qPicks[i] == nil {
		c.qPicks[i] = map[int]bool{}
	}
	return c.qPicks[i]
}

// optionLabel is an option's label without "(Recommended)", and whether it
// had it.
func optionLabel(l string) (string, bool) {
	l = strings.TrimSpace(l)
	i := strings.Index(strings.ToLower(l), "(recommended)")
	if i < 0 {
		return l, false
	}
	return strings.TrimSpace(l[:i] + l[i+len("(recommended)"):]), true
}

// questionKey answers Claude's questions. While the card has the keys, ↑↓
// choose, a digit or enter picks (or ticks, for multi-select, where enter
// confirms), and ←→ move between questions; typed text and enter answer in
// your own words. With several questions the last step is a review, where
// enter sends. It reports whether it used the key.
func (m *Model) questionKey(c *hostConn, req *headless.PermissionRequest, s string, empty bool) (tea.Cmd, bool) {
	c.syncQuestion(req)
	_, qs := questions(req)
	if len(qs) == 0 {
		return nil, false
	}
	if c.qIdx >= len(qs) {
		// The review: enter sends, a digit or ← goes back to one.
		switch {
		case s == "enter":
			return m.sendAnswers(c, req, qs), true
		case s == "left":
			m.goQuestion(c, qs, len(qs)-1)
			return nil, true
		case empty && len(s) == 1 && s[0] >= '1' && s[0] <= '9' && int(s[0]-'1') < len(qs):
			m.goQuestion(c, qs, int(s[0]-'1'))
			return nil, true
		}
		return nil, false
	}
	q := qs[c.qIdx]
	picked := c.picks(c.qIdx)
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
		case "left", "right":
			if len(qs) > 1 {
				to := c.qIdx - 1
				if s == "right" {
					to = c.qIdx + 1
				}
				if to >= 0 {
					m.goQuestion(c, qs, to)
				}
				return nil, true
			}
		case "space":
			if q.MultiSelect && c.qCursor < len(q.Options) {
				picked[c.qCursor] = !picked[c.qCursor]
				return nil, true
			}
		case "enter":
			switch {
			case c.qCursor >= len(q.Options):
				c.cardFocus = false // "your own words": type in the box
				return nil, true
			case !q.MultiSelect:
				return m.answerQuestion(c, req, qs, q.Options[c.qCursor].Label), true
			case !anyPicked(picked):
				picked[c.qCursor] = true
			}
		}
	}
	if empty && len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		i := int(s[0] - '1')
		if i >= len(q.Options) {
			return nil, true
		}
		if q.MultiSelect {
			picked[i] = !picked[i]
			return nil, true
		}
		return m.answerQuestion(c, req, qs, q.Options[i].Label), true
	}
	if s == "enter" {
		if !empty {
			text := strings.TrimSpace(string(c.input))
			c.input, c.back = c.input[:0], 0
			return m.answerQuestion(c, req, qs, text), true
		}
		if q.MultiSelect && anyPicked(picked) {
			var labels []string
			for i, o := range q.Options {
				if picked[i] {
					labels = append(labels, o.Label)
				}
			}
			return m.answerQuestion(c, req, qs, strings.Join(labels, ", ")), true
		}
		return nil, true
	}
	return nil, false
}

func anyPicked(p map[int]bool) bool {
	for _, v := range p {
		if v {
			return true
		}
	}
	return false
}

// goQuestion moves to question i (len(qs) is the review), with the cursor
// on the option already chosen there.
func (m *Model) goQuestion(c *hostConn, qs []question, i int) {
	if len(qs) < 2 {
		i = min(i, len(qs)-1)
	}
	c.qIdx, c.qCursor = max(0, min(i, len(qs))), 0
	if c.qIdx < len(qs) {
		for j, o := range qs[c.qIdx].Options {
			if c.qAnswer[qs[c.qIdx].Question] == o.Label {
				c.qCursor = j
			}
		}
	}
}

// answerQuestion records one answer, then moves to the next question still
// unanswered. A lone question sends at once; several end on the review.
func (m *Model) answerQuestion(c *hostConn, req *headless.PermissionRequest, qs []question, answer string) tea.Cmd {
	c.qAnswer[qs[c.qIdx].Question] = answer
	if len(qs) == 1 {
		return m.sendAnswers(c, req, qs)
	}
	next := len(qs)
	for k := 1; k < len(qs); k++ {
		j := (c.qIdx + k) % len(qs)
		if _, done := c.qAnswer[qs[j].Question]; !done {
			next = j
			break
		}
	}
	m.goQuestion(c, qs, next)
	return nil
}

// sendAnswers replies: Claude Code takes the answers as the tool's input,
// keyed by question text, with the preview of each chosen option beside
// it. Questions left unanswered go without one.
func (m *Model) sendAnswers(c *hostConn, req *headless.PermissionRequest, qs []question) tea.Cmd {
	b := answerInput(req, qs, c.qAnswer)
	id := req.ID
	c.qFor = ""
	return hostCmd(func() error { return c.client.Allow(id, b, false) })
}

// answerInput is the tool input that answers req.
func answerInput(req *headless.PermissionRequest, qs []question, answers map[string]string) json.RawMessage {
	return req.AnswerInput(qs, answers)
}

// shownAnswer is an answer as the card shows it.
func shownAnswer(a string) string {
	l, _ := optionLabel(a)
	return oneLine(l)
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
	// The card sits above the dock's rows: ↑ from the box goes up through
	// them first, and onto the card from the top one; ↓ off the card comes
	// back down onto that row.
	top := ""
	if s == "up" || s == "down" {
		if dock := m.dockRefs(c); len(dock) > 0 {
			top = dock[0]
		}
	}
	switch {
	case !c.cardFocus && empty && s == "up" && c.sel == top:
		c.cardFocus, c.sel = true, ""
		return nil, true
	case c.cardFocus && s == "down":
		c.cardFocus, c.sel = false, top
		return nil, true
	case c.cardFocus && s == "up" && len(c.bodyRefs) > 0:
		// Above the card: the conversation's last row.
		c.cardFocus, c.sel, c.selMoved = false, c.bodyRefs[len(c.bodyRefs)-1], true
		return nil, true
	case c.cardFocus && s == "esc":
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
	return dim("↑ to answer")
}

// selTurn is the turn picked in the conversation, or the one the picked
// step belongs to.
func selTurn(c *hostConn) *convo.Turn {
	t, _, _ := strings.Cut(c.sel, ":")
	if !isTurnRef(t) {
		return nil
	}
	for _, turn := range c.sess.Turns {
		if "t"+strconv.Itoa(turn.N) == t {
			return turn
		}
	}
	return nil
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

// pickedSub is the subagent run you're on: the one watched, or the one
// picked in the dock or the subagents view; live when it's still working.
func (m *Model) pickedSub(c *hostConn) (sa convo.Subagent, live, ok bool) {
	id := ""
	switch {
	case m.watchingSub(c):
		id = c.subOpen
	case strings.HasPrefix(c.sel, "run:"):
		id = strings.TrimPrefix(c.sel, "run:")
	case strings.HasPrefix(c.sel, "sub:"):
		id = strings.TrimPrefix(c.sel, "sub:")
	default:
		return sa, false, false
	}
	for _, x := range c.subs {
		if x.ID == id {
			_, live = c.subState(x)
			return x, live, true
		}
	}
	return sa, false, false
}

// stopSub stops one subagent, and nothing else: the turn and the other
// runs carry on.
func (m *Model) stopSub(c *hostConn, sa convo.Subagent, live bool) tea.Cmd {
	switch {
	case !live:
		m.flash("that subagent has already finished", false)
		return nil
	case c.client == nil:
		m.flash("agtop can stop a subagent only in a session it runs; this one is Claude Code's", true)
		return nil
	}
	m.flash("stopping "+sa.Type+" · the turn carries on", false)
	cl, id := c.client, sa.ID
	return hostCmd(func() error { return cl.StopTask(id) })
}
