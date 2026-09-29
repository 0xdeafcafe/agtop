package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// The Wall place: every agent at once, a tile each, what it is saying and
// running streaming up its tile as it happens. A tile's edge is the colour
// of its state, and lights up while its transcript is being written; the
// older lines fade. Only what's open is on it, unless a shows the day's.

type wallState struct {
	sel string // the agent picked, by key
	top int    // the first row of tiles shown, when they don't all fit
	all bool   // everything from the last day, not only what's open
	// reading are the transcripts being read for tiles, by agent key.
	reading map[string]bool
	// tiles are where each agent's tile was last drawn, for clicks;
	// above and below are how many weren't, for want of room.
	tiles        []wallTile
	above, below int
}

type wallTile struct {
	key        string
	x, y, w, h int
}

// wallReadMsg is a tile's transcript tail, read off the UI's goroutine; ok
// is false when it hadn't changed.
type wallReadMsg struct {
	key string
	e   previewEntry
	ok  bool
}

// Tiles are at least this big; below it they page.
const (
	wallMinW = 40
	wallMinH = 9
)

// wallAgents are the tiles, in an order that doesn't shuffle as they
// work: what needs you first, then what's at work, then the rest, each
// by when it started.
func (m *Model) wallAgents() []*fleet.Agent {
	now := m.snap.At
	var out []*fleet.Agent
	for _, a := range m.snap.Agents {
		if a.Done || a.Past {
			continue
		}
		open := a.Live() || a.Busy() || a.PID != 0 || a.Halted() || a.YourTurn(now)
		if open || m.wall.all && now.Sub(a.UpdatedAt) < workSince {
			out = append(out, a)
		}
	}
	rank := func(a *fleet.Agent) int {
		switch {
		case a.NeedsYou() || a.Halted():
			return 0
		case a.Live():
			return 1
		case a.Busy():
			return 2
		case a.Waiting() || a.YourTurn(now):
			return 3
		case a.PID != 0:
			return 4
		}
		return 5
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// wallGrid lays n tiles out in w×h: the columns, the rows that fit on
// screen, and each tile's height. It picks tiles about three and a half
// times as wide as tall (a cell is twice as tall as wide), wasting few.
func wallGrid(n, w, h int) (cols, rows, tileH int) {
	if n == 0 || w <= 0 || h <= 0 {
		return 1, 1, max(h, 0)
	}
	maxCols := max(1, (w+1)/(wallMinW+1))
	best, bestScore := 0, math.Inf(1)
	for c := 1; c <= min(maxCols, n); c++ {
		r := (n + c - 1) / c
		th := h / r
		if th < wallMinH {
			continue
		}
		tw := (w - (c - 1)) / c
		score := math.Abs(math.Log(float64(tw)/float64(th)/3.5)) + 1.5*float64(r*c-n)/float64(n)
		if score < bestScore {
			best, bestScore = c, score
		}
	}
	if best > 0 {
		r := (n + best - 1) / best
		return best, r, h / r
	}
	// They don't all fit: as many columns as there's room for, and pages
	// of rows.
	rows = max(1, h/wallMinH)
	return min(maxCols, n), rows, h / rows
}

// wallBody draws the tiles into the frame's body: w wide, h tall.
func (m *Model) wallBody(w, h int) []string {
	agents := m.wallAgents()
	m.wall.tiles, m.wall.above, m.wall.below = m.wall.tiles[:0], 0, 0
	if len(agents) == 0 {
		out := make([]string, h)
		msg := "nothing open right now"
		if !m.wall.all {
			msg += faint("  ·  a shows the last day's")
		}
		if h > 0 {
			out[h/2] = blanks((w-cellw.String(msg))/2) + dim(msg)
		}
		return out
	}
	cols, rows, tileH := wallGrid(len(agents), w, h)
	pick := m.wallPick(agents)
	allRows := (len(agents) + cols - 1) / cols
	// Keep the picked tile's row on screen.
	if r := pick / cols; r < m.wall.top {
		m.wall.top = r
	} else if r >= m.wall.top+rows {
		m.wall.top = r - rows + 1
	}
	m.wall.top = max(0, min(m.wall.top, allRows-rows))
	m.wall.above, m.wall.below = m.wall.top*cols, max(0, len(agents)-(m.wall.top+rows)*cols)

	tileW := (w - (cols - 1)) / cols
	extraW := w - (cols - 1) - tileW*cols
	out := make([]string, 0, h)
	for r := 0; r < rows; r++ {
		th := tileH
		if r == rows-1 {
			th = h - tileH*(rows-1) // the last row takes what's left over
		}
		row := make([][]string, 0, cols)
		widths := make([]int, 0, cols)
		x := 0
		for c := 0; c < cols; c++ {
			tw := tileW
			if c < extraW {
				tw++
			}
			i := (m.wall.top+r)*cols + c
			if i < len(agents) {
				a := agents[i]
				row = append(row, m.wallTile(a, tw, th, i == pick))
				m.wall.tiles = append(m.wall.tiles, wallTile{key: a.Key, x: x, y: len(out), w: tw, h: th})
			} else {
				row = append(row, nil)
			}
			widths = append(widths, tw)
			x += tw + 1
		}
		for y := 0; y < th; y++ {
			var b strings.Builder
			for c, t := range row {
				if c > 0 {
					b.WriteByte(' ')
				}
				if y < len(t) {
					b.WriteString(t[y])
				} else {
					b.WriteString(blanks(widths[c]))
				}
			}
			out = append(out, b.String())
		}
	}
	return out
}

func (m *Model) wallPick(agents []*fleet.Agent) int {
	for i, a := range agents {
		if a.Key == m.wall.sel {
			return i
		}
	}
	if len(agents) > 0 {
		m.wall.sel = agents[0].Key
	}
	return 0
}

// wallLook is how a tile shows its agent's state: its edge's colour, the
// glyph by its name, and the word for it.
func (m *Model) wallLook(a *fleet.Agent) (edge, glyph, word string) {
	now := m.snap.At
	switch {
	case a.NeedsYou():
		return cYellow, paint(cYellow+bold, "●"), paint(cYellow+bold, "needs you")
	case a.Halted():
		return cRed, paint(cRed, "✕"), paint(cRed, "stopped")
	case a.Live():
		return cOrange, paint(cOrange, spinner[(m.tick+len(a.ID))%len(spinner)]), paint(cOrange, "working")
	case a.Busy():
		return cQueue, paint(cQueue, "◌"), paint(cQueue, lanesLine(a))
	case a.Waiting():
		return cYellow, paint(cYellow, "○"), paint(cYellow, "waiting on you")
	case a.YourTurn(now):
		return cGreen, paint(cGreen, "●"), paint(cGreen, "your turn")
	case a.PID != 0:
		return cEdge, paint(cSub, "○"), dim("idle")
	}
	return cFaint, faint("·"), faint(age(a.Age(now)) + " ago")
}

// wallTile draws one agent's tile, w×h, its edge heavy when it's picked.
func (m *Model) wallTile(a *fleet.Agent, w, h int, picked bool) []string {
	if w < 8 || h < 3 {
		return nil
	}
	now := m.snap.At
	p := m.previews[a.Key].p
	edge, glyph, word := m.wallLook(a)
	// Its transcript was written in the last couple of seconds: the edge
	// flashes bright.
	if !p.At.IsZero() && now.Sub(p.At) < 2*time.Second && (a.Live() || a.Busy()) {
		edge = cBright
	}
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	nameC := cText + bold
	if picked {
		tl, tr, bl, br, hz, vt = "┏", "┓", "┗", "┛", "━", "┃"
		nameC = cBright + bold
	}
	iw := w - 4 // inside the edge, a space either side

	// The top edge carries the name and, on the right, the model and age.
	model := strings.TrimPrefix(p.Model, "claude-")
	if model == "" {
		model = strings.TrimPrefix(a.Spend.Model, "claude-")
	}
	if !unmarked(agent.Kind(a.Kind)) {
		model = strings.TrimSpace(agentName(a.Kind) + " " + model)
	}
	meta := strings.Join(nonEmpty(model, age(a.Age(now))), " · ")
	name := oneLine(a.DisplayName)
	roomName := w - 12 - cellw.String(meta)
	if roomName < 8 {
		meta, roomName = "", w-10
	}
	title := glyph + " " + paint(nameC, ansi.Truncate(name, max(roomName, 1), "…"))
	right := ""
	if meta != "" {
		right = " " + dim(meta) + " "
	}
	fill := w - 2 - 1 - cellw.String(title) - 2 - cellw.String(right) - 1
	top := paint(edge, tl+hz) + " " + title + " " + paint(edge, strings.Repeat(hz, max(fill, 0))) + right + paint(edge, hz+tr)
	top = fit(top, w)

	inner := make([]string, 0, h-2)
	// Where: the checkout and branch, and how full its context is.
	where := filepath.Base(a.Repo)
	if a.Repo == "" {
		where = filepath.Base(a.Cwd)
	}
	loc := paint(cSub, where)
	if a.Branch != "" {
		loc += faint(" ⎇ ") + dim(a.Branch)
	}
	ctx := ""
	if p.Context > 0 {
		pct := float64(p.Context) / float64(claude.ContextWindow(p.Model)) * 100
		ctx = ctxBar(pct) + " " + dim(fmt.Sprintf("%2.0f%%", pct))
	}
	inner = append(inner, wallSpread(loc, ctx, iw))

	// The bottom line: what it's doing now, what it costs, its todos.
	var now1 string
	switch {
	case a.NeedsYou() || a.Waiting():
		now1 = word + dim(" · ") + paint(cText, oneLine(firstNonEmpty(a.Needs, a.Detail, p.Text)))
	case a.Halted():
		now1 = word + dim(" · ") + paint(cSub, oneLine(a.Spend.Halt.Text))
	case a.Live() && p.Doing != "":
		now1 = glyph + " " + paint(cText, oneLine(tildify(p.Doing)))
	case a.Live() && a.Detail != "":
		now1 = glyph + " " + paint(cText, oneLine(a.Detail))
	default:
		now1 = word
	}
	var stats []string
	if a.Todos > 0 {
		c := cSub
		if a.TodosDone == a.Todos {
			c = cGreen
		}
		stats = append(stats, paint(c, fmt.Sprintf("☑ %d/%d", a.TodosDone, a.Todos)))
	}
	if n := a.Subs.Direct + a.Subs.Nested; n > 0 {
		stats = append(stats, paint(cQueue, fmt.Sprintf("⑂%d", n)))
	}
	if a.Spend.Cost > 0 {
		stats = append(stats, dim(moneyShort(a.Spend.Cost)))
	}
	foot := wallSpread(now1, strings.Join(stats, " "), iw)

	// Between them, the stream: its latest messages and tool calls, the
	// newest at the bottom, fading as they get older.
	room := h - 2 - 2
	if room > 0 {
		stream := wallStream(p, a, iw, room)
		for len(stream) < room {
			stream = append([]string{""}, stream...)
		}
		inner = append(inner, stream...)
	}
	inner = append(inner, foot)

	out := make([]string, 0, h)
	out = append(out, top)
	side := paint(edge, vt)
	for _, l := range inner[:min(len(inner), h-2)] {
		out = append(out, side+" "+fit(l, iw)+" "+side)
	}
	out = append(out, paint(edge, bl+strings.Repeat(hz, w-2)+br))
	return out
}

// wallStream is the end of what an agent said and did, room lines of w,
// oldest first. Lines fade with age: the newest two things bright, a few
// more dim, the rest faint.
func wallStream(p claude.Preview, a *fleet.Agent, w, room int) []string {
	if len(p.Recent) == 0 {
		// The foot already says its Detail; here, what it was asked.
		say := a.Intent
		if say == "" {
			return []string{faint("…")}
		}
		lines := wrap(oneLine(say), w)
		for i := range lines {
			lines[i] = dim(lines[i])
		}
		return lines[:min(len(lines), room)]
	}
	var out []string
	for back := 0; back < len(p.Recent) && len(out) < room; back++ {
		e := p.Recent[len(p.Recent)-1-back]
		tone := 0
		switch {
		case back >= 5:
			tone = 2
		case back >= 2:
			tone = 1
		}
		ink := func(c string) string {
			switch tone {
			case 1:
				if c == cText {
					return cSub
				}
				return cDim
			case 2:
				return cFaint
			}
			return c
		}
		var lines []string
		switch e.Role {
		case "user":
			ls := wrap(oneLine(e.Text), w-2)
			if len(ls) > 2 {
				ls = append(ls[:1], ansi.Truncate(ls[1], w-3, "")+"…")
			}
			for i, l := range ls {
				lead := "  "
				if i == 0 {
					lead = paint(ink(cBlue), "❯ ")
				}
				lines = append(lines, lead+paint(ink(cBlue), l))
			}
		case "tool":
			tool, arg, _ := strings.Cut(e.Text, "\x00")
			dot := ink(cOrange)
			if back > 0 || !a.Live() {
				dot = ink(cSub)
			}
			arg = ansi.Truncate(oneLine(tildify(arg)), max(w-cellw.String(tool)-4, 1), "…")
			lines = append(lines, paint(dot, "● ")+paint(ink(cText)+bold, tool)+" "+paint(ink(cSub), arg))
		default:
			ls := wrap(mdPlainNoUnder.Replace(oneLine(e.Text)), w-2)
			if keep := 3 + 2*boolInt(back == 0); len(ls) > keep {
				ls = append(ls[:keep-1], ansi.Truncate(ls[keep-1], w-3, "")+"…")
			}
			for _, l := range ls {
				lines = append(lines, "  "+paint(ink(cText), l))
			}
		}
		// Prepend whole messages; the oldest may lose its first lines.
		if over := len(out) + len(lines) - room; over > 0 {
			lines = lines[over:]
		}
		out = append(lines, out...)
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// wallSpread puts l on the left of w cells and r on the right, cutting l
// first when they don't both fit.
func wallSpread(l, r string, w int) string {
	rw := cellw.String(r)
	if rw == 0 {
		return fit(l, w)
	}
	if rw+2 > w {
		return fit(l, w)
	}
	return fit(l, w-rw-1) + " " + r
}

// wallTick keeps every tile's transcript tail fresh while the Wall is
// open: each is looked at, and read when it grew, off the UI's goroutine.
func (m *Model) wallTick() tea.Cmd {
	if m.mode != modeWall {
		return nil
	}
	if m.wall.reading == nil {
		m.wall.reading = map[string]bool{}
	}
	var cmds []tea.Cmd
	for _, a := range m.wallAgents() {
		if a.TranscriptPath == "" || m.wall.reading[a.Key] || len(cmds) >= 24 {
			continue
		}
		m.wall.reading[a.Key] = true
		key, path, had := a.Key, a.TranscriptPath, int64(-1)
		if e, ok := m.previews[a.Key]; ok {
			had = e.size
		}
		cmds = append(cmds, func() tea.Msg {
			st, err := os.Stat(path)
			if err != nil || st.Size() == had {
				return wallReadMsg{key: key}
			}
			return wallReadMsg{key: key, ok: true, e: previewEntry{p: claude.ReadPreview(path, 128<<10), size: st.Size()}}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) onWallRead(msg wallReadMsg) {
	delete(m.wall.reading, msg.key)
	if msg.ok {
		m.previews[msg.key] = msg.e
	}
}

func (m *Model) wallHint() string {
	all := "the last day's"
	if m.wall.all {
		all = "only what's open"
	}
	pairs := []string{"←↑↓→", "move", "enter", "open", "a", all, "alt+g", "keep going", "esc", "back"}
	if a := m.agentByKey(m.wall.sel); a != nil && a.Halted() {
		pairs[7] = "continue"
	}
	more := ""
	if m.wall.above > 0 {
		more += paint(cSub, fmt.Sprintf("↑ %d above", m.wall.above))
	}
	if m.wall.below > 0 {
		if more != "" {
			more += dim(" · ")
		}
		more += paint(cSub, fmt.Sprintf("↓ %d more", m.wall.below))
	}
	if more == "" {
		return keysFit(m.w-4, pairs...)
	}
	return more + "   " + keysFit(m.w-4-cellw.String(more)-3, pairs...)
}

func (m *Model) wallKey(s string) tea.Cmd {
	agents := m.wallAgents()
	if len(agents) == 0 {
		switch s {
		case "a":
			m.wall.all = !m.wall.all
		case "esc", "q":
			m.setView(placeAgents)
		}
		return nil
	}
	i := m.wallPick(agents)
	cols, _, _ := wallGrid(len(agents), m.w-4, m.wallH())
	move := func(d int) {
		if j := i + d; j >= 0 && j < len(agents) {
			m.wall.sel = agents[j].Key
		}
	}
	a := agents[i]
	switch s {
	case "esc", "q":
		m.setView(placeAgents)
	case "left", "h":
		move(-1)
	case "right", "l":
		move(1)
	case "up", "k":
		move(-cols)
	case "down", "j":
		move(cols)
	case "home", "g":
		m.wall.sel = agents[0].Key
	case "end", "G":
		m.wall.sel = agents[len(agents)-1].Key
	case "a":
		m.wall.all = !m.wall.all
	case "enter":
		return m.goAgent(a)
	case "alt+g":
		return m.keepGoing(a)
	}
	return nil
}

// wallH is the body's height in the frame: see frame.
func (m *Model) wallH() int { return m.h - m.headH() - 3 }

// wallClick picks the tile under the mouse; a second click opens it.
func (m *Model) wallClick(x, y int) tea.Cmd {
	bx, by := x-2, y-m.headH()-1 // the frame's indent and the row under the header
	for _, t := range m.wall.tiles {
		if bx < t.x || bx >= t.x+t.w || by < t.y || by >= t.y+t.h {
			continue
		}
		double := t.key == m.wall.sel && time.Since(m.lastClick) < 400*time.Millisecond
		m.wall.sel, m.lastClick = t.key, time.Now()
		if double {
			if a := m.agentByKey(t.key); a != nil {
				return m.goAgent(a)
			}
		}
		return nil
	}
	return nil
}
