package convo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/charmbracelet/x/ansi"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agtools"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// Line is one drawn row. Ref names what it belongs to, for selection and the
// mouse: "t13" for a turn heading, "t13:s:<tool id>" for a step,
// "t13:run:4" for a folded run of steps. Empty for rows you can't act on.
type Line struct {
	Text string
	Ref  string
	// Wrap is set on a row that carries on the line above it, where the
	// text was wrapped to fit rather than broken, so copying it joins them.
	Wrap bool
}

// Options say how to draw.
type Options struct {
	Width    int
	Now      time.Time
	Tick     int
	Open     map[string]bool // fold overrides by ref; absent means the default
	Verbose  bool            // ctrl+o: open everything, trim nothing
	Selected string
	Focused  bool
	Marks    map[string]bool // files you've marked reviewed in the changes view
	// Wide lets rows run the whole width, past capRow: for a session shown
	// alone on a wide screen, where the right edge is the screen's.
	Wide bool
}

// rowCap is how wide a row's numbers and rules may run.
func (o Options) rowCap() int {
	if o.Wide {
		return max(o.Width, capRow)
	}
	return capRow
}

const (
	capRow   = 124 // numbers never drift further right than this
	capProse = 100 // prose wraps here however wide the pane
	gutter   = 4   // where narration, steps and the answer all start
)

type cached struct {
	key   cacheKey
	lines []Line
}

// cacheKey is everything that affects a turn's drawing: its content, the
// width, its folds, the selection inside it, and for a live turn the clock.
type cacheKey struct {
	width, ver int
	wide       bool
	pal        int // the palette it was drawn in
	open, verb bool
	folds, sel string
	focused    bool
	tick       int
	now        int64
	clock      bool
	latest     string // the session's newest step, when it's in this turn
}

// Render draws every turn, oldest first.
func (s *Session) Render(o Options) []Line { return s.RenderInto(o, nil) }

// RenderInto is Render writing into buf's storage, so a caller that draws
// every frame reuses one slice instead of allocating the whole session's
// lines each time. The result is only good until the next call.
func (s *Session) RenderInto(o Options, buf []Line) []Line {
	if o.Width < 20 {
		o.Width = 20
	}
	s.memoTurn()
	folds := foldsByTurn(o.Open)
	latest := s.latest()
	if cap(s.parts) < len(s.Turns) {
		s.parts = make([][]Line, len(s.Turns))
	}
	parts := s.parts[:len(s.Turns)]
	n := 0
	for i, t := range s.Turns {
		recent := i >= len(s.Turns)-2
		parts[i] = s.turn(t, o, recent, folds, latest)
		n += len(parts[i])
	}
	out := buf[:0]
	if cap(out) < n {
		out = make([]Line, 0, n+n/4)
	}
	for _, p := range parts {
		out = append(out, p...)
	}
	clear(parts) // don't keep turns' lines alive through this slice
	return out
}

// foldsByTurn groups the fold overrides by the turn they're in, each
// turn's sorted, so a turn's cache key names only its own.
func foldsByTurn(open map[string]bool) map[string]string {
	if len(open) == 0 {
		return nil
	}
	by := map[string][]string{}
	for k, v := range open {
		t, _, _ := strings.Cut(k, ":")
		by[t] = append(by[t], k+"="+strconv.FormatBool(v))
	}
	out := make(map[string]string, len(by))
	for t, fs := range by {
		sort.Strings(fs)
		out[t] = strings.Join(fs, ",")
	}
	return out
}

// latest is the newest step the session has drawn at the top level, which
// shows opened until a newer one takes its place.
func (s *Session) latest() *Step {
	for i := len(s.Turns) - 1; i >= 0; i-- {
		items := s.Turns[i].Items
		for j := len(items) - 1; j >= 0; j-- {
			if it := items[j]; it.Kind == KStep && !hidden(it.Step) {
				return it.Step
			}
		}
	}
	return nil
}

// StepOpen is whether a step's row draws opened when nothing's overridden
// it: in verbose, or when it's the session's newest step.
func (s *Session) StepOpen(ref string, verbose bool) bool {
	if verbose {
		return true
	}
	st := s.latest()
	if st == nil {
		return false
	}
	for _, t := range s.Turns {
		for _, it := range t.Items {
			if it.Step == st {
				return ref == "t"+strconv.Itoa(t.N)+":s:"+st.ID
			}
		}
	}
	return false
}

func (s *Session) turn(t *Turn, o Options, recent bool, folds map[string]string, latest *Step) []Line {
	if t.ref == "" {
		t.ref = "t" + strconv.Itoa(t.N)
	}
	ref := t.ref
	open := recent || t.Live || t.Err != ""
	if v, ok := o.Open[ref]; ok {
		open = v
	}
	d := drawer{s: s, t: t, o: o, ref: ref, cw: min(o.Width, o.rowCap())}
	for _, it := range t.Items {
		if latest != nil && it.Step == latest {
			d.latest = latest
		}
	}
	key := s.cacheKey(t, o, ref, open, folds)
	if d.latest != nil {
		key.latest = d.latest.ID
	}
	if c, ok := s.cache[t]; ok && c.key == key {
		return c.lines
	}
	if open {
		d.open()
	} else {
		d.folded()
	}
	// An open turn ends with a plain gap (the running turn's rail ends
	// with it); folded turns stack row on row.
	if open {
		d.lines = append(d.lines, Line{Text: row("", "", "", d.o.Width, d.cw)})
	}
	s.cache[t] = cached{key: key, lines: d.lines}
	return d.lines
}

func (s *Session) cacheKey(t *Turn, o Options, ref string, open bool, folds map[string]string) cacheKey {
	k := cacheKey{width: o.Width, wide: o.Wide, ver: t.ver, open: open, verb: o.Verbose, folds: folds[ref], pal: palette}
	if o.Selected == ref || strings.HasPrefix(o.Selected, ref) && strings.HasPrefix(o.Selected[len(ref):], ":") {
		k.sel, k.focused = o.Selected, o.Focused
	}
	if t.Live || waiting(t) {
		k.clock, k.tick, k.now = true, o.Tick, o.Now.Unix()
	}
	return k
}

// waiting is whether a step in the turn waits on you. Every change to a
// step touches its turn, so the answer holds until the turn changes.
func waiting(t *Turn) bool {
	if t.waitVer == t.ver+1 {
		return t.waits
	}
	t.waits = false
	for _, st := range t.steps {
		if st.Status == Waiting {
			t.waits = true
			break
		}
	}
	t.waitVer = t.ver + 1
	return t.waits
}

type drawer struct {
	s     *Session
	t     *Turn
	o     Options
	ref   string
	cw    int
	lines []Line
	// worked is whether a step has been drawn yet, so the answer after
	// them stands further apart.
	worked bool
	// marks are what the command being drawn echoes between parts of its
	// output, drawn as headings.
	marks map[string]bool
	// lg is the language of the output being drawn, when it's code read
	// from a file; byPath has each line say its own file (grep's a.go:12:).
	lg     *lang
	byPath bool
	hs     hlState
	// hsPath and hsN are the file and line the highlighter's state is from.
	hsPath string
	hsN    int
	// spans are the languages of a chain's output, part by part.
	spans []span
	// subject is whether the next line of a git log's message is its title.
	subject bool
	// latest is the session's newest step when it's in this turn: it
	// shows opened, and never folds into a run.
	latest *Step
}

var (
	spineLive = paint(cOrange, "▏")
	spineErr  = paint(cRed, "▏")
)

func (d *drawer) spine() string {
	switch {
	case d.t.Live:
		return spineLive
	case d.t.Err != "":
		return spineErr
	}
	return " "
}

// add appends a row; a selected row gets the selection surface and marker.
func (d *drawer) add(ref, b, left, right string) {
	if ref != "" && ref == d.o.Selected {
		b = bgSelU
		mark := faint("▍")
		if d.o.Focused {
			b, mark = bgSel, paint(cOrange, "▍")
		}
		// The marker takes column 0, where the spine would be.
		left = mark + strings.TrimPrefix(left, d.spine())
	}
	d.lines = append(d.lines, Line{Text: row(b, left, right, d.o.Width, d.cw), Ref: ref})
}

// wrapped marks the row just added as carrying on the one before it.
func (d *drawer) wrapped() { d.lines[len(d.lines)-1].Wrap = true }

func (d *drawer) blank() {
	d.lines = append(d.lines, Line{Text: row("", d.spine(), "", d.o.Width, d.cw)})
}

func (d *drawer) mark() string {
	t := d.t
	switch {
	case t.Live:
		return paint(cOrange, "✻")
	case t.Stopped:
		return dim("⏹")
	case t.Err != "":
		return paint(cRed, "✗")
	}
	return paint(cGreen, "✓")
}

func (d *drawer) meta() string {
	t := d.t
	end := t.End
	if t.Live {
		end = d.o.Now
	}
	var parts []string
	if n := t.Steps(); n > 0 {
		parts = append(parts, plural(n, "step"))
	}
	if !t.Start.IsZero() {
		parts = append(parts, dur(end.Sub(t.Start)))
	}
	if m := money(t.Cost); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "   ")
}

// imageNames names a turn's images: their files, or Image #1, #2 for the
// ones a transcript only knows were there.
func imageNames(images []string) []string {
	out := make([]string, len(images))
	for i, im := range images {
		out[i] = im
		if im == "image" || im == "" {
			out[i] = fmt.Sprintf("Image #%d", i+1)
		} else {
			out[i] = ImageLabel(im)
		}
	}
	return out
}

var screenshotRe = regexp.MustCompile(`^(?:Screenshot|Screen Shot|CleanShot) \d{4}-\d{2}-\d{2} at (.+)\.(?i:png|jpe?g)$`)

// ImageLabel names an image file for a chip: its file name, with a macOS
// screenshot's long "Screenshot 2026-09-24 at 10.21.03.png" cut to the
// time that tells it from its neighbours.
func ImageLabel(path string) string {
	name := filepath.Base(path)
	if m := screenshotRe.FindStringSubmatch(strings.ReplaceAll(name, "\u202f", " ")); m != nil {
		return "screenshot " + m[1]
	}
	return name
}

// imageChips lays images out as chips, as many to a row as fit in w, a
// chip never broken across rows.
func imageChips(names []string, w int) []string {
	var rows []string
	row, n := "", 0
	for _, name := range names {
		cw := 2 + len([]rune(name))
		if n > 0 && n+3+cw > w {
			rows, row, n = append(rows, row), "", 0
		}
		if n > 0 {
			row, n = row+"   ", n+3
		}
		row, n = row+paint(cBlue, "▣ ")+paint(cText, name), n+cw
	}
	if n > 0 {
		rows = append(rows, row)
	}
	return rows
}

// folded is one row: your ask, then how it came out.
func (d *drawer) folded() {
	t := d.t
	ask := oneLine(FoldPastes(t.Prompt))
	if ask == "" && len(t.Images) > 0 {
		ask = strings.Join(imageNames(t.Images), " ")
	}
	if ask == "" {
		ask = "picked up on its own"
	}
	outcome := t.Outcome()
	if t.Err != "" {
		outcome = paint(cRed, t.Err)
	} else {
		outcome = text(outcome)
	}
	askW := max(16, d.cw*2/5)
	if w := len([]rune(ask)); w > askW {
		ask = string([]rune(ask)[:askW-1]) + "…"
	}
	who := styledAsk(ask, cSub)
	if strings.TrimSpace(t.Prompt) == "" && t.From == "" {
		who = dim("◌ picked up on its own")
	}
	if t.From != "" {
		who = dim("◌ "+t.From+" · ") + sub(ask)
	}
	left := "  " + faint("▸") + " " + d.mark() + " " + dim(fmt.Sprintf("#%d", t.N)) + "  " + who
	if outcome != "" && outcome != text("") {
		left += "  " + dim("→") + " " + outcome
	}
	d.add(d.ref, "", left, dim(d.meta()))
}

func (d *drawer) open() {
	t := d.t
	band := bgWell
	switch {
	case t.Live:
		band = bgLive
	case t.Err != "":
		band = bgErr
	}
	right := dim(fmt.Sprintf("#%d", t.N))
	if m := d.meta(); m != "" {
		if t.Live {
			right += "  " + paint(cOrange, m)
		} else {
			right += "  " + dim(m)
		}
	}
	// Images show as the box showed them, unless the words already place
	// them ([Image #1]).
	var imgs []string
	if len(t.Images) > 0 && !strings.Contains(t.Prompt, "[Image #") {
		imgs = imageNames(t.Images)
	}
	ask := FoldPastes(t.Prompt)
	if ask == "" {
		ask = "picked up on its own"
	}
	headW := max(20, d.cw-11-len([]rune(stripANSI(right)))-2)
	styled, label := styledAsk(oneLine(ask), cText+bold), dim("you")
	if strings.TrimSpace(t.Prompt) == "" && t.From == "" {
		styled, label = dim("picked up on its own"), dim("◌")
	}
	if t.From != "" {
		styled, label = sub(oneLine(ask)), dim("◌ "+t.From)
	}
	rowW := min(headW-len([]rune(stripANSI(label)))+3, capProse)
	rows := wrap(styled, rowW)
	if strings.TrimSpace(t.Prompt) == "" && t.From == "" && imgs != nil {
		rows, label, imgs = imageChips(imgs, rowW), dim("you"), nil
	}
	if len(rows) > 3 {
		rows = append(rows[:2], rows[2]+dim(" …"))
	}
	for i, r := range rows {
		if i == 0 {
			d.add(d.ref, band, d.spine()+" "+faint("▾")+" "+d.mark()+" "+label+"  "+r, right)
		} else {
			d.add(d.ref, band, d.spine()+"          "+r, "")
			d.wrapped()
		}
	}
	for _, r := range imageChips(imgs, min(d.cw-11, capProse)) {
		d.add(d.ref, band, d.spine()+"          "+r, "")
	}
	d.blank()

	items := t.Items
	// A running turn keeps its last few steps in view; everything clean
	// before them folds, so a busy agent doesn't flood the screen.
	keep := len(items)
	if t.Live {
		seen := 0
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Kind == KStep {
				seen++
				if seen == 3 {
					keep = i
					break
				}
			}
		}
		if seen < 3 {
			keep = 0
		}
	}
	for i := 0; i < len(items); i++ {
		it := items[i]
		// A run of two or more clean steps folds to one row; a failure
		// never folds.
		if !d.o.Verbose && it.Kind == KStep && i < keep {
			j := i
			for j < keep && j < len(items) && items[j].Kind == KStep && foldable(items[j].Step) && !d.testsFailed(items[j].Step) && items[j].Step != d.latest {
				j++
			}
			if j-i >= 2 {
				runRef := fmt.Sprintf("%s:run:%d", d.ref, i)
				if !d.o.Open[runRef] {
					d.run(runRef, items[i:j])
					// What the run did that you'd want to see stays out.
					for _, x := range items[i:j] {
						d.cards(x.Step, gutter+2)
					}
					i = j - 1
					continue
				}
				// Opened: every step of the run, under a row that folds it
				// back, and nothing after it refolds.
				d.add(runRef, "", d.spine()+blanks(gutter-1)+faint("▾ "+plural(j-i, "step")), "")
				for _, x := range items[i:j] {
					d.step(x.Step, 0)
				}
				i = j - 1
				continue
			}
		}
		switch it.Kind {
		case KText:
			if it.Answer {
				d.answer(it.Text)
			} else {
				// Narration is the thread you read: set apart from the
				// steps around it, which stay close together.
				d.gap()
				d.prose(it.Text, gutter, cSub)
				d.gap()
			}
		case KThinking:
			// Thinking shows while it happens; afterwards only in verbose.
			// While it happens, the live line at the end says so.
			switch {
			case d.o.Verbose:
				d.add("", "", d.spine()+strings.Repeat(" ", gutter-1)+dim("✻ thought"), "")
				if strings.TrimSpace(it.Text) != "" {
					d.prose(it.Text, gutter+2, cDim)
				}
			}
		case KCompact:
			d.compacted(it)
		case KNotice:
			col, mark := cDim, "·"
			switch it.Level {
			case "warning":
				col, mark = cYellow, "●"
			case "error":
				col, mark = cRed, "●"
			}
			for k, r := range wrap(oneLine(it.Text), min(d.cw-10, capProse)) {
				lead := paint(col, mark) + " "
				if k > 0 {
					lead = "  "
				}
				d.add("", "", d.spine()+"   "+lead+paint(col, r), "")
				if k > 0 {
					d.wrapped()
				}
			}
		case KInterject:
			rows := imageChips(imageNames(it.Images), min(d.cw-10, capProse))
			if strings.TrimSpace(it.Text) != "" {
				rows = append(wrap(styledAsk(oneLine(it.Text), cText), min(d.cw-10, capProse)), rows...)
			}
			for k, r := range rows {
				lead := dim("you") + "  "
				if k > 0 {
					lead = "     "
				}
				d.add("", "", d.spine()+"   "+lead+r, "")
				if k > 0 {
					d.wrapped()
				}
			}
		case KStep:
			d.step(it.Step, 0)
		}
	}
	if t.Live {
		d.liveLine()
	}
	switch {
	case t.Stopped:
		d.add("", "", d.spine()+"   "+dim("⏹ stopped"), "")
	case t.Err != "" && !t.Live:
		d.add("", bgErr, d.spine()+"   "+paint(cRed, "✗ "+t.Err), dim("your next message picks it up"))
	}
	// The rail stops at the last thing drawn, never on an empty row.
	for n := len(d.lines); n > 1 && strings.TrimSpace(stripANSI(d.lines[n-1].Text)) == strings.TrimSpace(stripANSI(d.spine())) && d.lines[n-1].Ref == ""; n-- {
		d.lines = d.lines[:n-1]
	}
}

// compacted is the line where the conversation was compacted: what set it
// off and how much context went; ctrl+o shows the summary it left.
func (d *drawer) compacted(it *Item) {
	c := it.Compact
	label := paint(cBlue, "◇ context compacted")
	var facts []string
	if c.Trigger != "" {
		facts = append(facts, c.Trigger)
	}
	switch {
	case c.PreTokens > 0 && c.PostTokens > 0:
		facts = append(facts, tokens(c.PreTokens)+" → "+tokens(c.PostTokens)+" tokens")
	case c.PreTokens > 0:
		facts = append(facts, "from "+tokens(c.PreTokens)+" tokens")
	}
	if it.Text != "" && !d.o.Verbose {
		facts = append(facts, "ctrl+o shows the summary")
	}
	left := d.spine() + "   " + faint("── ") + label
	if len(facts) > 0 {
		left += dim("  " + strings.Join(facts, " · "))
	}
	left += " " + faint(strings.Repeat("─", max(0, d.cw-cellw.String(stripANSI(left))-2)))
	d.add("", "", left, "")
	if d.o.Verbose && it.Text != "" {
		d.prose(it.Text, 6, cDim)
	}
}

// liveLine ends a running turn with what Claude is doing right now, how
// long the turn has run and roughly how much it has written, so a quiet
// stretch (thinking, a long answer being composed) never looks stalled.
func (d *drawer) liveLine() {
	t := d.t
	verb, since := "working", time.Time{}
	if n := len(t.Items); n > 0 {
		switch last := t.Items[n-1]; {
		case last.Kind == KThinking && !t.Thinking.IsZero():
			verb, since = "thinking", t.Thinking
		case last.Kind == KText && d.s.streaming == last:
			verb = "writing"
		case last.Kind == KStep && last.Step.Status == Running:
			return // the step's own row is spinning
		}
	}
	line := paint(cOrange, spinner[d.o.Tick%len(spinner)]+" "+verb+"…")
	var facts []string
	if !since.IsZero() {
		facts = append(facts, dur(d.o.Now.Sub(since)))
	}
	if !t.Start.IsZero() {
		facts = append(facts, "turn "+dur(d.o.Now.Sub(t.Start)))
	}
	if t.Streamed > 0 {
		facts = append(facts, "↓ "+tokens(t.Streamed/4)+" tokens")
	}
	if len(facts) > 0 {
		line += dim("  " + strings.Join(facts, " · "))
	}
	d.add("", "", d.spine()+"   "+line, "")
}

// verb is a step in a word or two, for a folded run: the program a command
// ran, or what a tool did.
func (d *drawer) verb(st *Step) string {
	switch st.Tool {
	case "Bash":
		// A chain that committed or pushed is named for that, not its git add.
		if cs := d.stepCards(st); len(cs) > 0 {
			return cs[0].verb()
		}
		cmd := readInput(st.Input).str("command")
		switch sh := d.shellShape(cmd); sh.kind {
		case "read", "search", "write":
			return sh.kind
		case "commit":
			return "git commit"
		case "":
			if _, ps := d.phrases(cmd); len(ps) > 0 {
				return ps[0].verb
			}
			return "shell"
		default:
			return strings.Fields(cdRe.ReplaceAllString(strings.TrimSpace(cmd), ""))[0]
		}
	case "Read":
		return "read"
	case "Grep", "Glob":
		return "search"
	case "WebFetch":
		return "fetch"
	case "WebSearch":
		return "web search"
	case "Task", "Agent":
		return agentName(st)
	case "Skill", "SlashCommand":
		in := readInput(st.Input)
		return firstNonEmpty(in.str("skill"), in.str("command"), "skill")
	}
	if strings.HasPrefix(st.Tool, "mcp__") {
		if parts := strings.SplitN(strings.TrimPrefix(st.Tool, "mcp__"), "__", 2); len(parts) == 2 {
			return parts[1]
		}
	}
	return st.Tool
}

func (d *drawer) run(ref string, items []*Item) {
	counts := map[string]int{}
	var order []string
	var first, last time.Time
	for _, it := range items {
		g := d.stepMemo(it.Step, 'v', d.verb)
		if counts[g] == 0 {
			order = append(order, g)
		}
		counts[g]++
		if first.IsZero() || it.Step.Start.Before(first) {
			first = it.Step.Start
		}
		if it.Step.End.After(last) {
			last = it.Step.End
		}
	}
	// What the steps were, by name: "sed ×2, grep, go build".
	var names []string
	for i, g := range order {
		if i == 4 {
			names = append(names, fmt.Sprintf("+%d more", len(order)-4))
			break
		}
		if counts[g] > 1 {
			g += fmt.Sprintf(" ×%d", counts[g])
		}
		names = append(names, g)
	}
	left := d.spine() + blanks(gutter-1) + faint("▸ "+plural(len(items), "step")+": ") + dim(strings.Join(names, ", "))
	left += faint(" · all ok")
	if !first.IsZero() && !last.IsZero() && last.Sub(first) >= 100*time.Millisecond {
		left += faint(" · " + dur(last.Sub(first)))
	}
	d.worked = true
	d.add(ref, "", left, "")
}

// gap is a blank row, unless the last row already is one or there is none.
func (d *drawer) gap() {
	if n := len(d.lines); n > 0 && !d.isBlank(n-1) {
		d.blank()
	}
}

func (d *drawer) isBlank(i int) bool {
	l := d.lines[i]
	return l.Ref == "" && strings.TrimSpace(stripANSI(l.Text)) == strings.TrimSpace(stripANSI(d.spine()))
}

// prose is Claude's narration: the work axis, secondary colour.
func (d *drawer) prose(s string, indent int, c string) {
	for _, para := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		// A paragraph already drawn this way comes from the memo, so text
		// streaming in only wraps its last paragraph again.
		k := memoKey{text: para, style: c, spine: d.spine(), n: indent, width: d.o.Width, cw: d.cw}
		if ls, ok := d.s.memoGet(k); ok {
			d.lines = append(d.lines, ls...)
			continue
		}
		from := len(d.lines)
		for k, r := range wrap(paint(c, inline(para, c)), min(d.cw-indent-1, capProse)) {
			d.add("", "", d.spine()+blanks(indent-1)+r, "")
			if k > 0 {
				d.wrapped()
			}
		}
		d.s.memoPut(k, d.lines[from:])
	}
}

// answer is the turn's final words: the conversation axis, full text colour,
// with light markdown.
func (d *drawer) answer(s string) {
	// After work, the answer stands two rows clear of it.
	d.gap()
	if d.worked {
		d.blank()
	}
	w := min(d.cw-5, capProse)
	lines := strings.Split(strings.TrimRight(s, " \t\n"), "\n")
	var items []listLevel // the list items open, outermost first
	for li := 0; li < len(lines); li++ {
		ln := lines[li]
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "|") {
			// A markdown table: every row up to the first that isn't one.
			end := li
			for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
				end++
			}
			if end-li >= 2 {
				d.table(lines[li:end], d.spine()+"   ", min(d.cw-5, d.o.rowCap()))
				li = end - 1
				continue
			}
		}
		pad := d.spine() + "   "
		if strings.HasPrefix(trim, "```") {
			// A code block: every line to the fence that closes it.
			end := li + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				end++
			}
			d.code(lines[li+1:end], strings.TrimSpace(strings.TrimPrefix(trim, "```")), pad)
			li = end
			continue
		}
		switch {
		case trim == "":
			d.blank()
			continue
		case strings.HasPrefix(trim, "#"):
			items = items[:0]
			d.add("", "", pad+paint(cWhite+bold, strings.TrimSpace(strings.TrimLeft(trim, "#"))), "")
			continue
		}
		// A list item, or a line of one, sits at its depth: two columns a
		// level, the bullet changing with it, wrapped lines under the text.
		col := indentOf(ln)
		marker, body, isItem := listItem(trim)
		lead, ind := marker, ""
		switch {
		case isItem:
			for len(items) > 0 && items[len(items)-1].col >= col {
				items = items[:len(items)-1]
			}
			depth := len(items)
			if marker == "" {
				lead = bullets[depth%len(bullets)]
			}
			items = append(items, listLevel{col: col, text: 2*depth + len([]rune(lead)) + 1})
			ind = blanks(2 * depth)
		case col > 0 && len(items) > 0:
			// Indented under an item: its text goes on there.
			for len(items) > 1 && items[len(items)-1].col >= col {
				items = items[:len(items)-1]
			}
			ind = blanks(items[len(items)-1].text)
		default:
			items = items[:0]
		}
		k := memoKey{text: trim, style: "answer:" + lead, spine: d.spine(), n: len(ind), width: d.o.Width, cw: d.cw}
		if ls, ok := d.s.memoGet(k); ok {
			d.lines = append(d.lines, ls...)
			continue
		}
		from := len(d.lines)
		if lead != "" {
			lead = dim(lead) + " "
		}
		leadW := len([]rune(stripANSI(lead)))
		for k, r := range wrap(text(inline(body, cText)), max(8, w-len(ind)-leadW)) {
			if k > 0 && lead != "" {
				r = blanks(leadW) + r
			} else {
				r = lead + r
			}
			d.add("", "", pad+ind+r, "")
			if k > 0 {
				d.wrapped()
			}
		}
		d.s.memoPut(k, d.lines[from:])
	}
}

// listLevel is an open list item: the column its marker is at in the
// source, and the one its text starts at as drawn.
type listLevel struct{ col, text int }

// bullets mark unordered items, one per depth.
var bullets = []string{"•", "◦", "▪"}

// listItem splits a markdown list item into its marker and text: marker
// is "" for a bullet, the number for an ordered item.
func listItem(trim string) (marker, body string, ok bool) {
	if len(trim) >= 2 && (trim[0] == '-' || trim[0] == '*' || trim[0] == '+') && trim[1] == ' ' {
		return "", strings.TrimSpace(trim[2:]), true
	}
	if m := numbered.FindStringSubmatch(trim); m != nil {
		return m[1], m[2], true
	}
	return "", trim, false
}

// indentOf is a line's leading indent in columns, a tab to the next stop
// of four.
func indentOf(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

// Answer draws text the way a turn's final words are drawn (headings,
// lists, code and tables), w wide, for showing it outside the conversation.
func (s *Session) Answer(text string, w int) []Line {
	d := drawer{s: s, t: &Turn{}, o: Options{Width: w}, cw: min(w, capRow)}
	d.answer(text)
	return d.lines
}

// code draws a fenced code block highlighted in the language its fence
// names (```go); a diff block colours its added and removed lines. A block
// is drawn once and kept while it's in view.
func (d *drawer) code(lines []string, tag, pad string) {
	k := memoKey{text: strings.Join(lines, "\n"), style: "code:" + tag, spine: d.spine(), width: d.o.Width, cw: d.cw, n: palette}
	if ls, ok := d.s.memoGet(k); ok {
		d.lines = append(d.lines, ls...)
		return
	}
	from := len(d.lines)
	w := min(d.cw-7, d.o.rowCap())
	lg := langFor(tag)
	if f := strings.Fields(tag); lg == nil && len(f) > 0 {
		lg = langFor(f[0]) // ```go title="x.go"
	}
	isDiff := tag == "diff" || tag == "patch"
	var st hlState
	for _, l := range lines {
		l = truncateCells(expandTabs(l), w)
		switch {
		case isDiff && strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			d.add("", bgAdd, pad+" "+plusSign()+text(l[1:]), "")
		case isDiff && strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			d.add("", bgDel, pad+" "+minusSign()+text(l[1:]), "")
		case isDiff && strings.HasPrefix(l, "@@"):
			d.add("", bgWell, pad+" "+paint(cBlue, l), "")
		default:
			d.add("", bgWell, pad+" "+highlight(lg, &st, l, cSub, nil), "")
		}
	}
	d.s.memoPut(k, d.lines[from:])
}

var tableSep = regexp.MustCompile(`^:?-{2,}:?$`)

// table draws markdown table rows as aligned columns: the header bold over
// a rule, cells shortened when the table is wider than the room.
func (d *drawer) table(rows []string, pad string, w int) {
	var cells [][]string
	head := -1
	for _, r := range rows {
		r = strings.TrimSpace(r)
		r = strings.TrimSuffix(strings.TrimPrefix(r, "|"), "|")
		parts := strings.Split(r, "|")
		sep := true
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
			if !tableSep.MatchString(parts[i]) {
				sep = false
			}
		}
		if sep {
			head = len(cells) - 1
			continue
		}
		cells = append(cells, parts)
	}
	cols := 0
	for _, r := range cells {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return
	}
	width := make([]int, cols)
	for _, r := range cells {
		for i, c := range r {
			width[i] = max(width[i], cellw.String(stripMarkdown(c)))
		}
	}
	// Too wide: take room from the widest column until it fits.
	gap := 3
	for total := sum(width) + gap*(cols-1); total > w; total = sum(width) + gap*(cols-1) {
		i := 0
		for j := range width {
			if width[j] > width[i] {
				i = j
			}
		}
		if width[i] <= 6 {
			break
		}
		width[i]--
	}
	for ri, r := range cells {
		var b strings.Builder
		for i := range cols {
			c := ""
			if i < len(r) {
				c = stripMarkdown(r[i])
			}
			c = truncateCells(c, width[i])
			switch {
			case ri == head:
				b.WriteString(paint(cWhite+bold, c))
			default:
				b.WriteString(text(c))
			}
			if i < cols-1 {
				b.WriteString(blanks(width[i] - cellw.String(c) + gap))
			}
		}
		d.add("", "", pad+b.String(), "")
		if ri == head {
			d.add("", "", pad+faint(strings.Repeat("─", min(w, sum(width)+gap*(cols-1)))), "")
		}
	}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

// memoKey names a paragraph as drawn: its text, how, and at what width.
type memoKey struct {
	text, style, spine string
	n, width, cw       int
}

// memoTurn ages the paragraph memo: what the last two renders drew stays,
// the rest goes.
func (s *Session) memoTurn() {
	s.memoOld, s.memo = s.memo, make(map[memoKey][]Line, len(s.memo))
	s.chainsOld, s.chains = s.chains, make(map[string]string, len(s.chains))
	if f := s.Info.Cwd + "|" + s.Cwd; f != s.rowsFor {
		s.rows, s.rowsFor = nil, f // paths read relative to other folders now
	}
	s.rowsOld, s.rows = s.rows, make(map[stepKey]string, len(s.rows))
	s.cardsOld, s.cards = s.cards, make(map[stepKey][]card, len(s.cards))
}

// stepKey names a step's label, summary or verb as drawn: they read only
// the step, so they hold until its status, output or subagent steps change.
// A running turn redraws every second; its finished steps don't need to
// parse their input and output again each time.
type stepKey struct {
	st                  *Step
	what                byte
	status              Status
	out, res, kids, pal int
}

func (d *drawer) stepMemo(st *Step, what byte, f func(*Step) string) string {
	s := d.s
	k := stepKey{st: st, what: what, status: st.Status, out: len(st.Output), res: len(st.Result), kids: len(st.Children), pal: palette}
	if v, ok := s.rows[k]; ok {
		return v
	}
	v, ok := s.rowsOld[k]
	if !ok {
		v = f(st)
	}
	if s.rows != nil {
		s.rows[k] = v
	}
	return v
}

func (s *Session) memoGet(k memoKey) ([]Line, bool) {
	if ls, ok := s.memo[k]; ok {
		return ls, true
	}
	if ls, ok := s.memoOld[k]; ok && s.memo != nil {
		s.memo[k] = ls
		return ls, true
	}
	return nil, false
}

func (s *Session) memoPut(k memoKey, ls []Line) {
	if s.memo != nil {
		s.memo[k] = append([]Line(nil), ls...)
	}
}

const spaces = "                                                                                                                                "

// blanks is n spaces.
func blanks(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(spaces) {
		return spaces[:n]
	}
	return strings.Repeat(" ", n)
}

// styledAsk draws your words with the things that act singled out: a
// shell command tinted, slash commands and @files in bold white, image and
// paste markers as chips, links underlined.
func styledAsk(s, base string) string {
	if cmd, ok := strings.CutPrefix(s, "! "); ok {
		return paint(cWhite+bold, "$ ") + tint(cmd)
	}
	s = specialRe.ReplaceAllStringFunc(s, func(m string) string {
		switch {
		case strings.HasPrefix(m, "[image: "):
			return reset + paint(cBlue, "▣ ") + paint(cText, ImageLabel(strings.TrimSuffix(m[len("[image: "):], "]"))) + base
		case strings.HasPrefix(m, "[Image"):
			return reset + paint(cBlue, "▣ ") + paint(cText, strings.Trim(m, "[]")) + base
		case strings.HasPrefix(m, "[Pasted"):
			return reset + paint(cBlue, "▤ ") + paint(cText, strings.Trim(m, "[]")) + base
		case strings.HasPrefix(m, "http"):
			return reset + link(m) + base
		}
		return reset + paint(cWhite+bold, m) + base
	})
	return paint(base, inline(s, base))
}

var specialRe = regexp.MustCompile(`\[Image #\d+\]|\[image: [^\]]+\]|\[Pasted text #\d+[^\]]*\]|https?://[^\s)>\]]+|(^|\s)/[a-z][\w:-]*(?:$|[\s.,;:!?)])|@[\w./-]+`)

var pastedRe = regexp.MustCompile(`(?s)\s*<pasted_content id="[^"]*">\n?(.*?)\n?</pasted_content(?: id="[^"]*")?>\s*`)

// EachPaste replaces each paste Claude Code marks in a message, the text
// between <pasted_content> tags, with what f makes of it.
func EachPaste(s string, f func(text string) string) string {
	if !strings.Contains(s, "<pasted_content") {
		return s
	}
	return pastedRe.ReplaceAllStringFunc(s, func(m string) string {
		return " " + f(pastedRe.FindStringSubmatch(m)[1]) + " "
	})
}

// FoldPastes shows each paste in a message as the chip it was in the box.
func FoldPastes(s string) string {
	n := 0
	return strings.TrimSpace(EachPaste(s, func(text string) string {
		n++
		return fmt.Sprintf("[Pasted text #%d +%d lines]", n, strings.Count(strings.TrimRight(text, "\n"), "\n")+1)
	}))
}

// link underlines a URL and makes it clickable in terminals that support
// OSC 8 hyperlinks.
func link(url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + paint(cBlue+"\x1b[4m", url) + "\x1b]8;;\x1b\\"
}

// Inline styles **bold** and `code` in s and links its URLs, returning to
// base colour after each.
func Inline(s, base string) string { return inline(s, base) }

var numbered = regexp.MustCompile(`^(\d+[.)])\s+(.*)$`)

// inline styles **bold** and `code`, returning to base colour after each,
// draws markdown images and links, and links URLs. Byte scans, the same as
// replacing \*\*([^*]+)\*\*, `([^`]+)` and https?://[^\s)>\]"'`]+ in turn.
func inline(s, base string) string {
	if !strings.ContainsAny(s, "*`h[") {
		return s
	}
	s = pairs(s, "**", bold, reset+base)
	// Code stands one shade above its words: white in the answer, text
	// colour in quieter narration, so it never outshines the answer.
	code := cWhite
	if base == cSub || base == cDim {
		code = cText
	}
	s = pairs(s, "`", code, reset+base)
	return mdLinks(s, base)
}

// mdLinks draws each markdown image, ![alt](src), as a chip naming it and
// each [text](url) as its text, both opening what they point at when
// clicked, and links the bare URLs between them.
func mdLinks(s, base string) string {
	if !strings.Contains(s, "](") {
		return links(s, base)
	}
	var b strings.Builder
	last := 0
	for p := 0; ; {
		i := strings.Index(s[p:], "](")
		if i < 0 {
			break
		}
		mid := p + i
		open := strings.LastIndexByte(s[last:mid], '[')
		end := strings.IndexByte(s[mid+2:], ')')
		if open < 0 || end < 0 {
			p = mid + 2
			continue
		}
		open += last
		end += mid + 2
		label, target := s[open+1:mid], strings.TrimSpace(s[mid+2:end])
		if strings.ContainsAny(label, "[]") || target == "" || strings.ContainsAny(target, " \t") {
			p = mid + 2
			continue
		}
		image := open > last && s[open-1] == '!'
		start := open
		if image {
			start--
		}
		b.WriteString(links(s[last:start], base))
		b.WriteString(reset)
		if image {
			b.WriteString(imageChip(label, target))
		} else if u := linkTarget(target); u != "" {
			b.WriteString("\x1b]8;;" + u + "\x1b\\" + paint(cBlue+"\x1b[4m", label) + "\x1b]8;;\x1b\\")
		} else {
			b.WriteString(paint(cBlue, label))
		}
		b.WriteString(base)
		last, p = end+1, end+1
	}
	if last == 0 {
		return links(s, base)
	}
	b.WriteString(links(s[last:], base))
	return b.String()
}

// imageChip draws an image Claude points at as the prompt draws one sent
// with it, ▣ and its name, with its file after, opening it when clicked.
func imageChip(alt, src string) string {
	name := path.Base(src)
	if alt == "" {
		alt = name
	}
	chip := paint(cBlue, "▣ ") + paint(cText, alt)
	if name != alt {
		chip += paint(cDim, " "+name)
	}
	if u := linkTarget(src); u != "" {
		return "\x1b]8;;" + u + "\x1b\\" + chip + "\x1b]8;;\x1b\\"
	}
	return chip
}

// linkTarget is the URL a markdown link or image opens: web addresses as
// they are, absolute paths and ~ as file URLs, nothing for the rest.
func linkTarget(t string) string {
	switch {
	case strings.HasPrefix(t, "http://"), strings.HasPrefix(t, "https://"), strings.HasPrefix(t, "file://"):
		return t
	case strings.HasPrefix(t, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		t = filepath.Join(home, t[2:])
	}
	if !filepath.IsAbs(t) {
		return ""
	}
	return (&url.URL{Scheme: "file", Path: t}).String()
}

// pairs wraps text between two delimiters in open and close, dropping the
// delimiters, where the text is at least one character and holds no
// delimiter character.
func pairs(s, delim, open, close string) string {
	d := delim[0]
	var b strings.Builder
	last := 0
	for p := 0; ; {
		i := strings.Index(s[p:], delim)
		if i < 0 {
			break
		}
		i += p
		from := i + len(delim)
		j := strings.IndexByte(s[from:], d)
		if j <= 0 || !strings.HasPrefix(s[from+j:], delim) {
			p = i + 1
			continue
		}
		if b.Len() == 0 {
			b.Grow(len(s) + 32)
		}
		b.WriteString(s[last:i])
		b.WriteString(open)
		b.WriteString(s[from : from+j])
		b.WriteString(close)
		last = from + j + len(delim)
		p = last
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// links makes each URL a link, returning to base colour after it.
func links(s, base string) string {
	var b strings.Builder
	last := 0
	for p := 0; ; {
		i := strings.Index(s[p:], "http")
		if i < 0 {
			break
		}
		i += p
		j := i + 4
		if j < len(s) && s[j] == 's' && strings.HasPrefix(s[j+1:], "://") {
			j += 4
		} else if strings.HasPrefix(s[j:], "://") {
			j += 3
		} else {
			p = i + 1
			continue
		}
		end := j
		for end < len(s) && !urlStop(s[end]) {
			end++
		}
		if end == j {
			p = i + 1
			continue
		}
		b.WriteString(s[last:i])
		b.WriteString(reset)
		b.WriteString(link(s[i:end]))
		b.WriteString(base)
		last, p = end, end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func urlStop(c byte) bool {
	switch c {
	case '\t', '\n', '\f', '\r', ' ', ')', '>', ']', '"', '\'', '`':
		return true
	}
	return false
}

// stripANSI drops colour codes (ESC [ digits and semicolons m), as ansiRe
// would, with a byte scan.
// cleanOutput takes every escape sequence and control character out of a
// line of a tool's output, keeping tabs for expandTabs.
func cleanOutput(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\t' || r == 0x7f }) {
		return s
	}
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func stripANSI(s string) string {
	i := strings.IndexByte(s, 0x1b)
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i >= 0 {
		b.WriteString(s[:i])
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				s = s[j+1:]
				i = strings.IndexByte(s, 0x1b)
				continue
			}
		}
		b.WriteByte(0x1b)
		s = s[i+1:]
		i = strings.IndexByte(s, 0x1b)
	}
	b.WriteString(s)
	return b.String()
}

// foldable steps are the clean, routine ones a finished turn can fold into
// a single row. Edits never fold: they're what you'd review.
func foldable(st *Step) bool {
	if st.Status != OK || hidden(st) {
		return false
	}
	switch glyphFor(st.Tool) {
	case "✎", "⇉", "◆", "◇":
		return false
	}
	return true
}

// hidden steps are bookkeeping the task line already shows.
func hidden(st *Step) bool {
	switch st.Tool {
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet":
		return true
	}
	return false
}

func (d *drawer) statusMark(st *Step) string {
	switch st.Status {
	case Running:
		return paint(cOrange, spinner[(d.o.Tick+len(st.ID))%len(spinner)])
	case OK:
		if d.testsFailed(st) {
			return paint(cRed, "✗")
		}
		return paint(cOKq, "✓")
	case Failed:
		return paint(cRed, "✗")
	case Waiting:
		return paint(cYellow, "●")
	case Denied:
		return dim("⊘")
	default:
		return dim("◌")
	}
}

func (d *drawer) step(st *Step, depth int) {
	if hidden(st) {
		return
	}
	ref := d.ref + ":s:" + st.ID
	indent := 4 + depth*4
	if st.Tool == agtools.Show && st.Status != Failed && d.figure(st, ref, indent) {
		return
	}
	// How it came out follows the label, so the eye never has to cross the
	// pane for it; the label gives way first when the row is too long.
	lead := d.spine() + strings.Repeat(" ", indent-1) + d.statusMark(st) + " "
	label, cells := d.stepMemo(st, 'l', d.label), d.cells(st)
	if cells != "" {
		cells = faint("  · ") + cells
		room := d.cw - cellw.String(lead) - cellw.String(cells) - 1
		if room >= 12 && cellw.String(label) > room {
			label = ansi.Truncate(label, room, "…")
		}
	}
	left := lead + label + cells
	d.worked = true
	// The latest step shows what it ran, unless a card says what it did.
	open := d.o.Verbose || st == d.latest && len(d.stepCards(st)) == 0
	if v, ok := d.o.Open[ref]; ok {
		open = v
	}
	// A failure shows just its error until you open it for everything.
	// It and its row share a red surface, so the two read as one.
	brief := st.Status == Failed && !open
	if brief {
		d.add(ref, bgErr, left, "")
		// A card says what went wrong better than a line of the output.
		if len(d.stepCards(st)) == 0 {
			d.errorLine(st, indent+4, ref)
		}
	} else {
		d.add(ref, "", left, "")
	}
	// What it did comes after what it ran, when that's open.
	if open {
		d.body(st, indent+4)
	}
	d.cards(st, indent+2)
	// A subagent shows its own steps while it works, or when opened.
	if len(st.Children) > 0 && (st.Status == Running || open) {
		for _, c := range st.Children {
			d.step(c, depth+1)
		}
	}
}

func (d *drawer) cells(st *Step) string {
	var parts []string
	if st.Status == Waiting {
		wait := ""
		if !st.Start.IsZero() {
			wait = faint(" · ") + dim(dur(d.o.Now.Sub(st.Start)))
		}
		return paint(cYellow, "waiting on you") + wait
	}
	if blocked(st) {
		return paint(cRed, "blocked by the harness")
	}
	// A card under the row says it better.
	if s := d.stepMemo(st, 's', d.summary); s != "" && len(d.stepCards(st)) == 0 {
		parts = append(parts, s)
	}
	if st.Tool == "Bash" && st.Exit > 0 {
		switch st.Exit {
		case 124:
			parts = append(parts, paint(cRed, "timed out"))
		case 137, 143:
			parts = append(parts, paint(cRed, "killed"))
		default:
			parts = append(parts, paint(cRed, fmt.Sprintf("exit %d", st.Exit)))
		}
	}
	switch {
	case st.Status == Running && !st.Start.IsZero():
		parts = append(parts, paint(cOrange, dur(d.o.Now.Sub(st.Start))))
	case !st.End.IsZero() && !st.Start.IsZero() && st.End.Sub(st.Start) >= 100*time.Millisecond:
		parts = append(parts, faint(dur(st.End.Sub(st.Start))))
	}
	return strings.Join(parts, faint(" · "))
}

// --- labels ---

type input map[string]any

func (in input) str(k string) string {
	v, _ := in[k].(string)
	return v
}

func readInput(raw json.RawMessage) input {
	var m input
	_ = json.Unmarshal(raw, &m)
	return m
}

// agentName is who a Task or Agent call ran: the agent Claude Code reports
// in its result (it resolves an omitted or aliased subagent_type), else the
// one asked for, else just "subagent".
func agentName(st *Step) string {
	var r struct {
		AgentType string `json:"agentType"`
	}
	if len(st.Result) > 0 && json.Unmarshal(st.Result, &r) == nil && r.AgentType != "" {
		return r.AgentType
	}
	return firstNonEmpty(readInput(st.Input).str("subagent_type"), "subagent")
}

func glyphFor(tool string) string {
	switch tool {
	case "Bash":
		return "$"
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return "✎"
	case "Read":
		return "◧"
	case "Grep", "Glob":
		return "⌕"
	case "Task", "Agent":
		return "⇉"
	case "WebFetch", "WebSearch":
		return "↗"
	case "Artifact":
		return "◆"
	case "Skill", "SlashCommand":
		return "✦"
	case agtools.Show:
		return "◇"
	}
	return "•"
}

// glyphColor is a step's glyph: faint, like the row it leads.
func glyphColor(g string) string {
	return faint(g)
}

// rel shortens a path to the session's folder when it's inside it, looking
// through symlinks such as macOS's /tmp → /private/tmp.
func (d *drawer) rel(p string) string {
	for _, base := range d.s.bases() {
		if r, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func (d *drawer) label(st *Step) string {
	in := readInput(st.Input)
	g := glyphColor(glyphFor(st.Tool))
	// A step is the log's quiet voice; one still going, waiting or failed
	// reads a shade up.
	base := cDim
	if st.Status != OK {
		base = cSub
	}
	lbl := func(s string) string { return paint(base, s) }
	switch st.Tool {
	case "Bash":
		cmd := in.str("command")
		// What the command is for reads faster than the command; the command
		// itself follows, quieter, and shows whole when the row is opened.
		if desc := oneLine(in.str("description")); desc != "" {
			return g + " " + lbl(desc) + "  " + faint(program(cmd))
		}
		// A read, a search or a write says so, as the tool it stands in for.
		if sh := d.shellShape(cmd); sh.kind != "" {
			l := glyphColor(sh.glyph) + " "
			if sh.in != "" {
				l += faint("in " + sh.in + " · ")
			}
			return l + lbl(sh.what)
		}
		// A chain, or a command too long to read at a glance, says what
		// each of its commands does.
		if l := d.chain(cmd, base); l != "" {
			return l
		}
		return g + " " + d.command(cmd, base)
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		p := in.str("file_path")
		if p == "" {
			p = in.str("notebook_path")
		}
		return g + " " + lbl(d.rel(p))
	case "Read":
		return g + " " + lbl(d.rel(in.str("file_path")))
	case "Grep":
		l := g + " " + lbl(in.str("pattern"))
		if p := in.str("path"); p != "" {
			l += faint(" in ") + lbl(d.rel(p))
		}
		return l
	case "Glob":
		return g + " " + lbl(in.str("pattern"))
	case "Task", "Agent":
		return g + " " + lbl(agentName(st)) + "  " + faint(oneLine(in.str("description")))
	case "WebFetch":
		return g + " " + lbl(in.str("url"))
	case "WebSearch":
		return g + " " + lbl(in.str("query"))
	case "Skill", "SlashCommand":
		name := firstNonEmpty(in.str("skill"), in.str("command"), in.str("name"))
		return g + " " + lbl(name) + "  " + faint(oneLine(in.str("args")))
	case "AskUserQuestion":
		q := ""
		if qs, ok := in["questions"].([]any); ok && len(qs) > 0 {
			if m, ok := qs[0].(map[string]any); ok {
				q, _ = m["question"].(string)
			}
		}
		return paint(cYellow, "?") + " " + text(oneLine(q))
	case agtools.Show:
		return g + " " + lbl(firstNonEmpty(oneLine(in.str("title")), "drawing"))
	case "Artifact":
		t := in.str("title")
		if t == "" {
			t = filepath.Base(in.str("file_path"))
		}
		return g + " " + lbl(t)
	}
	name := st.Tool
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			name = parts[1] + faint(" · "+parts[0])
		}
	}
	return g + " " + lbl(name) + "  " + faint(firstValue(in))
}

func firstValue(in input) string {
	for _, k := range []string{"description", "prompt", "query", "path", "url", "name", "skill"} {
		if v := in.str(k); v != "" {
			return oneLine(v)
		}
	}
	return ""
}

var cdRe = regexp.MustCompile(`^cd\s+("[^"]+"|'[^']+'|\S+)\s*&&\s*`)

// command is a shell command's first line in the row's colour c: a cd into
// another folder leads, quieter, and a longer command says how much more.
func (d *drawer) command(cmd, c string) string {
	cmd = strings.TrimSpace(cmd)
	// Paths inside the session's folder read better relative to it.
	for _, base := range d.s.bases() {
		cmd = strings.ReplaceAll(cmd, base+"/", "")
	}
	lines := strings.Split(cmd, "\n")
	first := lines[0]
	lead := ""
	if m := cdRe.FindStringSubmatch(first); m != nil {
		dir := strings.Trim(m[1], `"'`)
		first = first[len(m[0]):]
		if dir != d.s.Info.Cwd && dir != "." {
			lead = faint("in " + d.rel(dir) + " · ")
		}
	}
	out := lead + paint(c, first)
	if n := len(lines) - 1; n > 0 {
		out += faint(fmt.Sprintf("  +%d lines", n))
	}
	return out
}

// program is a command's first word, or first two for git, go, npm and the
// like, so a row can say "git status" without the whole pipeline.
func program(cmd string) string {
	cmd = strings.TrimSpace(cdRe.ReplaceAllString(strings.TrimSpace(cmd), ""))
	f := strings.Fields(strings.SplitN(cmd, "\n", 2)[0])
	// Leading VAR=value assignments aren't the program.
	for len(f) > 1 && strings.Contains(f[0], "=") && !strings.HasPrefix(f[0], "-") {
		f = f[1:]
	}
	if len(f) == 0 {
		return ""
	}
	p := f[0]
	switch p {
	case "git", "go", "npm", "pnpm", "yarn", "cargo", "docker", "kubectl", "gh", "make", "uv", "bun":
		if len(f) > 1 && !strings.HasPrefix(f[1], "-") {
			p += " " + f[1]
		}
	}
	if strings.ContainsAny(cmd, "|;&") {
		p += " …"
	}
	return p
}

var shOps = map[string]bool{"&&": true, "||": true, "|": true, ";": true, ">": true, ">>": true, "<": true, "2>&1": true, "&": true}

func tint(cmd string) string {
	var b strings.Builder
	head := true
	for _, tok := range shellTokens(cmd) {
		switch {
		case strings.TrimSpace(tok) == "":
			b.WriteString(tok)
		case shOps[tok]:
			b.WriteString(paint(cOrange, tok))
			head = true
		case strings.HasPrefix(tok, `"`) || strings.HasPrefix(tok, `'`):
			b.WriteString(paint(cGreen, tok))
			head = false
		case strings.HasPrefix(tok, "$"):
			b.WriteString(paint(cBlue, tok))
			head = false
		case head:
			b.WriteString(paint(cText+bold, tok))
			head = false
		case strings.HasPrefix(tok, "-"):
			b.WriteString(sub(tok))
		default:
			b.WriteString(text(tok))
		}
	}
	return b.String()
}

// shLine is one line of a command as shellLines lays it out: depth 1 is a
// pipe's later stage; verbatim is a heredoc's body, shown as written.
type shLine struct {
	text     string
	depth    int
	verbatim bool
}

// shellLines lays a command out the way a person would write it rather than
// as the one long line an agent sends: each command on its own line, && and
// || leading the line they join, a pipe's stages indented under it. Quotes,
// $(…) and heredoc bodies are never split.
func shellLines(cmd string) []shLine {
	var out []shLine
	var cur strings.Builder
	depth := 0 // of the line being built
	emit := func(next int) {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, shLine{text: s, depth: depth})
		}
		cur.Reset()
		depth = next
	}
	quote, parens := byte(0), 0
	heredoc := ""
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(cmd) {
				i++
				cur.WriteByte(cmd[i])
			} else if c == quote {
				quote = 0
			}
		case c == '\\' && i+1 < len(cmd):
			cur.WriteByte(c)
			i++
			cur.WriteByte(cmd[i])
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '(':
			parens++
			cur.WriteByte(c)
		case c == ')':
			parens = max(0, parens-1)
			cur.WriteByte(c)
		case parens > 0:
			cur.WriteByte(c)
		case c == '<' && strings.HasPrefix(cmd[i:], "<<") && !strings.HasPrefix(cmd[i:], "<<<"):
			// Remember the word that ends the heredoc; its body starts at
			// the next newline.
			rest := strings.TrimLeft(strings.TrimPrefix(cmd[i+2:], "-"), " ")
			end := strings.IndexAny(rest, " \n;|&<>")
			if end < 0 {
				end = len(rest)
			}
			heredoc = strings.Trim(rest[:end], `'"`)
			cur.WriteString("<<")
			i++
		case c == '\n':
			emit(0)
			if heredoc != "" {
				body := cmd[i+1:]
				for n, l := range strings.Split(body, "\n") {
					out = append(out, shLine{text: l, verbatim: true})
					i += len(l) + 1
					if strings.TrimSpace(l) == heredoc || n > 10000 {
						break
					}
				}
				heredoc = ""
			}
		case c == ';':
			emit(0)
		case c == '&' && strings.HasPrefix(cmd[i:], "&&"), c == '|' && strings.HasPrefix(cmd[i:], "||"):
			emit(0)
			cur.WriteString(cmd[i:i+2] + " ")
			i = skipSpaces(cmd, i+1)
		case c == '|' && !(i > 0 && cmd[i-1] == '>'):
			emit(1)
			cur.WriteString("| ")
			i = skipSpaces(cmd, i)
		default:
			cur.WriteByte(c)
		}
	}
	emit(0)
	return out
}

// skipSpaces is i moved past the spaces that follow it.
func skipSpaces(s string, i int) int {
	for i+1 < len(s) && s[i+1] == ' ' {
		i++
	}
	return i
}

// shellTokens splits on spaces, keeping quoted strings and spaces whole.
func shellTokens(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			flush()
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				out = append(out, s[i:])
				return out
			}
			out = append(out, s[i:i+j+2])
			i += j + 1
		case c == ' ' || c == '\t':
			flush()
			out = append(out, string(c))
		case c == ';' || c == '|' || c == '&':
			flush()
			if i+1 < len(s) && (s[i+1] == c) {
				out = append(out, s[i:i+2])
				i++
			} else {
				out = append(out, string(c))
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// --- summaries ---

var (
	goOK   = regexp.MustCompile(`(?m)^ok\s+\S+`)
	goFail = regexp.MustCompile(`(?m)^(FAIL\s+\S+|--- FAIL)`)
	jsSum  = regexp.MustCompile(`Tests?\s+(?:\x1b\[[0-9;]*m)?(?:(\d+) failed[^|\n]*\|\s*)?(\d+) passed`)
)

func (d *drawer) summary(st *Step) string {
	switch st.Tool {
	case "Bash":
		out := bashOut(st)
		if n := len(goFail.FindAllString(out, -1)); n > 0 {
			s := fmt.Sprintf("%d failed", n)
			var names []string
			for _, m := range goFailName.FindAllStringSubmatch(out, 3) {
				names = append(names, m[1])
			}
			if len(names) > 0 {
				s += ": " + strings.Join(names, ", ")
			}
			return paint(cRed, s)
		}
		if m := jsSum.FindStringSubmatch(out); m != nil {
			if m[1] != "" && m[1] != "0" {
				return paint(cRed, m[1]+" failed") + faint(" · "+m[2]+" passed")
			}
			return faint(m[2] + " passed")
		}
		if n := len(goOK.FindAllString(out, -1)); n > 0 {
			if n == 1 {
				return faint("ok")
			}
			return faint(fmt.Sprintf("%d ok", n))
		}
		if st.Status != OK {
			return ""
		}
		sh := d.shellShape(readInput(st.Input).str("command"))
		n := countLines(out)
		switch {
		case sh.kind == "commit":
			res := sh.res
			if m := commitRe.FindStringSubmatch(out); m != nil {
				res = strings.TrimSpace(res + " " + m[1])
			}
			return faint(res)
		case sh.kind == "search" && sh.ctx && n > 0:
			return faint(plural(n, "line"))
		case sh.kind == "search" && n > 0:
			if n == 1 {
				return faint("1 match")
			}
			return faint(fmt.Sprintf("%d matches", n))
		case sh.kind == "search":
			return faint("no matches")
		case sh.res != "":
			return faint(sh.res)
		case n == 0:
			return faint("no output")
		case n > 1:
			return faint(fmt.Sprintf("%d lines", n))
		}
	case "Edit", "MultiEdit", "Write":
		var r struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(st.Result, &r)
		if r.Type == "create" {
			return paint(cGreen, fmt.Sprintf("new · %d lines", countLines(r.Content)))
		}
		add, del := 0, 0
		for _, p := range headless.Patches(st.Result) {
			for _, l := range p.Lines {
				switch {
				case strings.HasPrefix(l, "+"):
					add++
				case strings.HasPrefix(l, "-"):
					del++
				}
			}
		}
		if add+del > 0 {
			return paint(cGreen, fmt.Sprintf("+%d", add)) + " " + paint(cRed, fmt.Sprintf("−%d", del))
		}
	case "Read":
		var r struct {
			File struct {
				NumLines   int `json:"numLines"`
				StartLine  int `json:"startLine"`
				TotalLines int `json:"totalLines"`
			} `json:"file"`
		}
		if json.Unmarshal(st.Result, &r) == nil && r.File.NumLines > 0 {
			f := r.File
			if f.NumLines < f.TotalLines {
				return faint(fmt.Sprintf("lines %d–%d", f.StartLine, f.StartLine+f.NumLines-1))
			}
			return faint(fmt.Sprintf("%d lines", f.TotalLines))
		}
	case "Grep", "Glob":
		if st.Status == OK {
			n := countLines(st.Output)
			if strings.HasPrefix(strings.TrimSpace(st.Output), "No ") {
				n = 0
			}
			noun := "results"
			if st.Tool == "Glob" {
				noun = "files"
			}
			return faint(fmt.Sprintf("%d %s", n, noun))
		}
	case "Task", "Agent":
		if n := len(st.Children); n > 0 {
			return faint(plural(n, "step"))
		}
	}
	return ""
}

func bashOut(st *Step) string {
	var r struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if json.Unmarshal(st.Result, &r) == nil && (r.Stdout != "" || r.Stderr != "") {
		return r.Stdout + "\n" + r.Stderr
	}
	return st.Output
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// --- bodies ---

func (d *drawer) body(st *Step, indent int) {
	// Code read from files shows highlighted.
	d.lg, d.byPath = nil, false
	d.resetHL()
	defer func() { d.lg, d.byPath, d.spans = nil, false, nil }()
	in := readInput(st.Input)
	switch st.Tool {
	case "Read":
		d.lg = langFor(in.str("file_path"))
	case "Grep":
		d.byPath = true
	case "Bash":
		switch sh := d.shellShape(in.str("command")); sh.kind {
		case "read":
			d.lg = langFor(sh.what)
		case "search":
			d.byPath = true
			if _, p, ok := strings.Cut(sh.what, " in "); ok {
				d.lg = langFor(strings.Split(p, ", ")[0])
			}
		case "":
			d.spans = d.chainSpans(in.str("command"))
		}
	}
	switch st.Tool {
	case "Edit", "MultiEdit", "Write":
		if d.diff(st, indent) {
			return
		}
	case "Bash":
		if cmd := strings.TrimSpace(readInput(st.Input).str("command")); cmd != "" {
			d.shellBody(cmd, indent)
			d.marks = echoMarks(cmd)
			defer func() { d.marks = nil }()
		}
		var r struct {
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		}
		if json.Unmarshal(st.Result, &r) == nil && (r.Stdout != "" || r.Stderr != "") {
			d.output(r.Stdout, indent, st.Status == Failed && r.Stderr == "")
			d.spans = nil
			if strings.TrimSpace(r.Stderr) != "" {
				if strings.TrimSpace(r.Stdout) != "" {
					d.add("", "", d.spine()+strings.Repeat(" ", indent-1)+faint(fmt.Sprintf("stderr · %d lines", countLines(r.Stderr))), "")
				}
				d.output(r.Stderr, indent, st.Exit != 0)
			}
			return
		}
		if st.Status == OK && strings.TrimSpace(st.Output) == "" {
			d.add("", "", d.spine()+strings.Repeat(" ", indent-1)+faint("no output"), "")
			return
		}
	}
	d.output(strings.TrimLeft(exitRe.ReplaceAllString(toolErrTag.Replace(st.Output), ""), "\n"), indent, st.Status == Failed)
}

// shellBody is an opened shell step's command: on one line when it fits,
// else laid out a command a line; a heredoc's body shows its first lines.
func (d *drawer) shellBody(cmd string, indent int) {
	pad := d.spine() + strings.Repeat(" ", indent-1)
	room := d.cw - indent - 4
	if !strings.Contains(cmd, "\n") && cellw.String(cmd) <= room {
		d.add("", bgWell, pad+faint("$ ")+quietTint(expandTabs(cmd)), "")
		return
	}
	body, bodyShown := 0, 0
	// A pipe carries on its command's line while that still fits; each
	// command of a chain keeps a line of its own, the && or || that joins it
	// out in the margin so the commands line up.
	var lines []shLine
	gutter := 2
	for _, l := range shellLines(cmd) {
		if n := len(lines); n > 0 && strings.HasPrefix(l.text, "| ") && !lines[n-1].verbatim && cellw.String(lines[n-1].text)+1+cellw.String(l.text)+lines[n-1].depth*2 <= room {
			lines[n-1].text += " " + l.text
			continue
		}
		if !l.verbatim && (strings.HasPrefix(l.text, "&& ") || strings.HasPrefix(l.text, "|| ")) {
			gutter = 3
		}
		lines = append(lines, l)
	}
	for _, l := range lines {
		if l.verbatim {
			body++
		}
	}
	blank := strings.Repeat(" ", gutter)
	var lg *lang
	var hs hlState
	for i, l := range lines {
		lead := faint("$") + blank[1:] // the command, not each line of a heredoc
		if i > 0 {
			lead = blank
		}
		if op := l.text[:min(3, len(l.text))]; !l.verbatim && (op == "&& " || op == "|| ") {
			lead, l.text = paint(cOrange, op[:2])+" ", l.text[3:]
		}
		if !l.verbatim && strings.Contains(l.text, "<<") {
			lg, hs = heredocLang(l.text), hlState{}
		}
		if l.verbatim {
			bodyShown++
			switch {
			case d.o.Verbose || body <= 8 || bodyShown <= 6:
			case bodyShown == 7:
				d.add("", bgWell, pad+lead+folded(body-7), "")
				continue
			case bodyShown < body:
				continue // the last line, the heredoc's end word, still shows
			}
		}
		hang := strings.Repeat(" ", l.depth*2)
		colored := quietTint(expandTabs(l.text))
		if l.verbatim {
			colored = highlight(lg, &hs, expandTabs(l.text), cOut, nil)
			if bodyShown == body {
				colored = faint(l.text) // the word that ends it
			}
		}
		for j, r := range wrap(colored, room-len(hang)) {
			if j > 0 {
				lead, r = blank, "  "+r // a wrapped line hangs under its own start
			}
			d.add("", bgWell, pad+lead+hang+r, "")
			if j > 0 {
				d.wrapped()
			}
		}
	}
}

// errRe finds the line of a failure's output that says what went wrong.
var errRe = regexp.MustCompile(`(?i)(error|fatal|panic|exception|traceback|failed|\bfail\b|not found|no such|denied|cannot|can't|undefined|unexpected|invalid|refused|timed out)`)

var (
	// tallyRe is a test runner's verdict line, which says only that it failed.
	tallyRe = regexp.MustCompile(`^(FAIL|ok|PASS)(\s+\S+(\s+[\d.]+s|\s+\[[^\]]+\])?)?$|^exit status \d+$`)
	// fileLineRe is a message placed at a file and line: path.go:12: or :12:3:.
	fileLineRe = regexp.MustCompile(`^\S+\.\w+:\d+(:\d+)?: \S`)
)

// errorLine is a failure in brief: the line of its output that says what
// went wrong (the last one that reads like an error, else the last line),
// in red on the failure's surface, with the way to see the rest.
func (d *drawer) errorLine(st *Step, indent int, ref string) {
	text := st.Output
	var r struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if st.Tool == "Bash" && json.Unmarshal(st.Result, &r) == nil && r.Stdout+r.Stderr != "" {
		text = r.Stdout + "\n" + r.Stderr
	}
	// The harness's refusal says why in its own words; its tags don't.
	if m := blockedRe.FindStringSubmatch(st.Output); m != nil && st.Status == Failed {
		text = m[1]
	}
	text = toolErrTag.Replace(text)
	var last, hit, at string
	for _, l := range strings.Split(stripANSI(collapseCR(text)), "\n") {
		l = strings.TrimSpace(expandTabs(l))
		if l == "" || exitRe.MatchString(l) && errRe.FindString(l) == "" || tallyRe.MatchString(l) {
			continue
		}
		last = l
		if errRe.MatchString(l) {
			hit = l
		}
		// The first file:line: message is where it went wrong: a compiler's
		// first error, a failing test's own words.
		if at == "" && fileLineRe.MatchString(l) {
			at = l
		}
	}
	if at != "" {
		hit = at
	}
	if hit == "" {
		hit = last
	}
	if hit == "" {
		return
	}
	// One quiet line: the error itself, cut to fit; the rest is a key away.
	pad := d.spine() + strings.Repeat(" ", indent-3)
	right := ""
	if d.o.Selected == ref {
		right = dim("enter shows all")
	}
	w := d.cw - indent - 20
	d.add(ref, bgErr, pad+faint("▸ ")+paint(cRed, truncateCells(hit, max(20, w))), right)
}

// output draws text in a well: head and tail when it's long, all of it in
// verbose mode, tinted red when it's a failure.
func (d *drawer) output(s string, indent int, failed bool) {
	s = collapseCR(strings.TrimRight(s, "\n"))
	if strings.TrimSpace(s) == "" {
		return
	}
	// JSON a tool printed (an API's answer, a --json flag, an MCP result)
	// is laid out and coloured, however it came.
	isJSON := false
	if !failed && d.lg == nil && !d.byPath {
		if p, ok := prettyJSON(s); ok {
			s, isJSON, d.hs = p, true, hlState{}
		}
	}
	lines := strings.Split(s, "\n")
	b, edge := bgWell, faint("▏")
	if failed {
		b, edge = bgErr, paint(cRed, "▎")
	}
	pad := d.spine() + strings.Repeat(" ", indent-1)
	w := d.cw - indent - 2
	// What a tool printed is quieter than anything Claude says, and plain
	// text: a heading or a list in a file never passes for Claude's own.
	// A failure's error lines stay red.
	// A chain's parts each have their own language, in the order it ran them.
	// A part of known length covers that many lines; one of unknown length
	// runs until a line that looks like the start of the next and not like
	// its own.
	spanOf := make([]int, 0, len(lines))
	for j, k := 0, 0; len(d.spans) > 0 && len(spanOf) < len(lines); k++ {
		sp, l := d.spans[j], cleanOutput(lines[len(spanOf)])
		if sp.n > 0 && k >= sp.n || sp.n == 0 && j+1 < len(d.spans) && d.spans[j+1].starts(l) && !sp.starts(l) {
			if j++; j == len(d.spans) {
				break
			}
			k = 0
		}
		spanOf = append(spanOf, j)
	}
	// A diff's lines are coloured by what they do, and its code in the
	// language of the file it's in.
	var diffLg *lang
	diffLine := func(i int, l string) bool {
		if failed || i >= len(spanOf) || !d.spans[spanOf[i]].diff {
			return false
		}
		if i == 0 || spanOf[i-1] != spanOf[i] {
			diffLg = d.spans[spanOf[i]].lg
		}
		l = truncateCells(l, w)
		switch {
		case strings.HasPrefix(l, "+++ "):
			if p := strings.TrimPrefix(strings.Fields(l[4:] + " ")[0], "b/"); langFor(p) != nil {
				diffLg = langFor(p)
			}
			d.resetHL()
			d.add("", b, pad+edge+faint(l), "")
		case strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "--- "):
			d.add("", b, pad+edge+faint(l), "")
		case strings.HasPrefix(l, "@@"):
			d.resetHL()
			d.add("", b, pad+edge+paint(cBlue, l), "")
		case strings.HasPrefix(l, "+"):
			d.add("", bgAdd, pad+edge+plusSign()+highlight(diffLg, &d.hs, l[1:], cOut, nil), "")
		case strings.HasPrefix(l, "-"):
			d.add("", bgDel, pad+edge+minusSign()+highlight(diffLg, &d.hs, l[1:], cOut, nil), "")
		case d.spans[spanOf[i]].git != "":
			d.add("", b, pad+edge+d.gitLine(d.spans[spanOf[i]].git, l), "")
		default:
			d.add("", b, pad+edge+highlight(diffLg, &d.hs, l, cOut, nil), "")
		}
		return true
	}
	// code is how long line i's prefix is, the path in it and the language
	// its code is in; a nil language when it isn't code.
	code := func(i int, l string) (int, string, *lang) {
		lg, byPath := d.lg, d.byPath
		if i < len(spanOf) {
			lg, byPath = d.spans[spanOf[i]].lg, d.spans[spanOf[i]].byPath
		}
		if failed || lg == nil && !byPath || i < len(spanOf) && d.spans[spanOf[i]].diff {
			return 0, "", nil
		}
		n, path := codePrefix(l)
		if byPath && path != "" {
			lg = langFor(path)
		}
		return n, path, lg
	}
	// Code lines start in one column after their prefixes, less the
	// indentation they all share: a chain's parts each line up their own,
	// and lines with no prefix aren't pushed along to meet the others.
	group := func(i, n int) int {
		g := 0
		if i < len(spanOf) {
			g = spanOf[i] + 1
		}
		if n > 0 {
			return g*2 + 1
		}
		return g * 2
	}
	prefixW, shared := make([]int, 2*len(d.spans)+2), make([]int, 2*len(d.spans)+2)
	for g := range shared {
		shared[g] = -1
	}
	for i, l := range lines {
		l = expandTabs(cleanOutput(l))
		n, _, lg := code(i, l)
		if lg == nil || strings.TrimSpace(l[n:]) == "" {
			continue
		}
		g := group(i, n)
		prefixW[g] = max(prefixW[g], cellw.String(l[:n]))
		ind := len(l[n:]) - len(strings.TrimLeft(l[n:], " "))
		if shared[g] < 0 || ind < shared[g] {
			shared[g] = ind
		}
	}
	last := -1
	emit := func(i int, l string) {
		// A tool's own escape codes (colours, cursor moves, titles) would
		// reach the terminal or throw widths off; agtop does the colour.
		l = expandTabs(cleanOutput(l))
		if d.marks[strings.TrimSpace(l)] {
			d.add("", b, pad+edge+markHeading(strings.TrimSpace(l), w), "")
			return
		}
		if i < len(spanOf) && !failed {
			if k := spanOf[i]; k != last {
				d.resetHL()
				last = k
			}
		}
		if diffLine(i, l) {
			return
		}
		if n, path, lg := code(i, l); lg != nil {
			d.carry(path, l[:n])
			g := group(i, n)
			pre := l[:n] + strings.Repeat(" ", max(0, prefixW[g]-cellw.String(l[:n])))
			body := l[n:]
			if sh := shared[g]; sh > 0 && len(body)-len(strings.TrimLeft(body, " ")) >= sh {
				body = body[sh:]
			}
			body = truncateCells(body, max(4, w-cellw.String(pre)))
			d.add("", b, pad+edge+faint(pre)+highlight(lg, &d.hs, body, cOut, nil), "")
			return
		}
		l = truncateCells(l, w)
		if i < len(spanOf) && !failed && d.spans[spanOf[i]].git != "" {
			d.add("", b, pad+edge+d.gitLine(d.spans[spanOf[i]].git, l), "")
			return
		}
		if isJSON {
			d.add("", b, pad+edge+highlight(langJSON, &d.hs, l, cOut, nil), "")
			return
		}
		c := cOut
		if failed && errRe.MatchString(l) {
			c = cRed
		}
		d.add("", b, pad+edge+paint(c, l), "")
	}
	more := func(n int) {
		d.resetHL() // what follows the gap doesn't go on from what came before it
		d.add("", b, pad+edge+folded(n), "")
	}
	if !d.o.Verbose && len(lines) > 8 {
		// The top and the end, where results and errors land; a failure
		// also keeps the error lines from the middle.
		for i, l := range lines[:3] {
			emit(i, l)
		}
		mid := lines[3 : len(lines)-5]
		cut := 0
		kept := 0
		for i, l := range mid {
			if failed && kept < 3 && errRe.MatchString(l) || d.marks[strings.TrimSpace(l)] {
				if cut > 0 {
					more(cut)
					cut = 0
				}
				emit(3+i, l)
				kept++
				continue
			}
			cut++
		}
		if cut > 0 {
			more(cut)
		}
		for i, l := range lines[len(lines)-5:] {
			emit(len(lines)-5+i, l)
		}
		return
	}
	for i, l := range lines {
		if i >= 2000 {
			d.add("", b, pad+edge+faint(fmt.Sprintf("… %d more lines", len(lines)-i)), "")
			break
		}
		emit(i, l)
	}
}

func (d *drawer) resetHL() {
	d.hs, d.hsPath, d.hsN = hlState{}, "", 0
}

// carry keeps the highlighter's state from the line before only when this
// line goes on from it in the same file: grep's matches are fragments, and
// a string or comment one leaves open mustn't colour the next.
func (d *drawer) carry(path, prefix string) {
	n := lineNo(prefix)
	if path != d.hsPath || n != 0 && d.hsN != 0 && n != d.hsN+1 {
		d.hs = hlState{}
	}
	d.hsPath, d.hsN = path, n
}

// prettyJSON is s indented two spaces a level when it's one JSON object or
// array, as is when it's JSON lines; ok is false for anything else.
func prettyJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 || len(t) > 4<<20 || t[0] != '{' && t[0] != '[' {
		return "", false
	}
	if json.Valid([]byte(t)) {
		var buf bytes.Buffer
		if json.Indent(&buf, []byte(t), "", "  ") != nil {
			return "", false
		}
		return buf.String(), true
	}
	for _, l := range strings.Split(t, "\n") {
		if l = strings.TrimSpace(l); l != "" && (l[0] != '{' && l[0] != '[' || !json.Valid([]byte(l))) {
			return "", false
		}
	}
	return t, true
}

func (d *drawer) diff(st *Step, indent int) bool {
	var r struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(st.Result, &r)
	in := readInput(st.Input)
	lg := langFor(firstNonEmpty(in.str("file_path"), in.str("notebook_path")))
	pad := d.spine() + strings.Repeat(" ", indent-1)
	w := d.cw - indent - 9
	if r.Type == "create" {
		// A new file is a diff where every line is added.
		var hs hlState
		lines := strings.Split(strings.TrimRight(r.Content, "\n"), "\n")
		for i, l := range lines {
			if i >= 30 && !d.o.Verbose {
				d.add("", bgWell, pad+faint(fmt.Sprintf("  … %d more lines · ctrl+o shows the rest", len(lines)-i)), "")
				break
			}
			d.diffLine(pad, w, lg, &hs, '+', i+1, l, "", false)
		}
		return true
	}
	patches := headless.Patches(st.Result)
	if len(patches) == 0 {
		return false
	}
	shown := 0
	for pi, p := range patches {
		if pi > 0 {
			d.add("", bgWell, pad+faint("  ⋯"), "")
		}
		// The old and the new side each carry their own strings and
		// comments from line to line.
		var oldSt, newSt hlState
		oldN, newN := p.OldStart, p.NewStart
		ls := p.Lines
		for i := 0; i < len(ls); i++ {
			// A run of removed lines and the added lines after it pair up
			// in order, so each pair can show the words that changed.
			if l := ls[i]; l != "" && l[0] == '-' {
				j := i
				for j < len(ls) && ls[j] != "" && ls[j][0] == '-' {
					j++
				}
				k := j
				for k < len(ls) && ls[k] != "" && ls[k][0] == '+' {
					k++
				}
				dels, adds := ls[i:j], ls[j:k]
				for n, l := range dels {
					if shown >= 30 && !d.o.Verbose {
						d.add("", bgWell, pad+faint("  … ctrl+o shows the rest"), "")
						return true
					}
					shown++
					var pair string
					if n < len(adds) {
						pair = adds[n][1:]
					}
					d.diffLine(pad, w, lg, &oldSt, '-', oldN, l[1:], pair, n < len(adds))
					oldN++
				}
				for n, l := range adds {
					if shown >= 30 && !d.o.Verbose {
						d.add("", bgWell, pad+faint("  … ctrl+o shows the rest"), "")
						return true
					}
					shown++
					var pair string
					if n < len(dels) {
						pair = dels[n][1:]
					}
					d.diffLine(pad, w, lg, &newSt, '+', newN, l[1:], pair, n < len(dels))
					newN++
				}
				i = k - 1
				continue
			}
			if shown >= 30 && !d.o.Verbose {
				d.add("", bgWell, pad+faint("  … ctrl+o shows the rest"), "")
				return true
			}
			shown++
			l := ls[i]
			if l == "" {
				l = " "
			}
			if l[0] == '+' {
				d.diffLine(pad, w, lg, &newSt, '+', newN, l[1:], "", false)
				newN++
				continue
			}
			for i, r := range d.codeRows(highlight(lg, &newSt, expandTabs(l[1:]), cSub, nil), w) {
				if i == 0 {
					d.add("", bgWell, pad+faint(fmt.Sprintf("%5d ", newN))+"  "+r, "")
				} else {
					d.add("", bgWell, pad+blanks(8)+r, "")
					d.wrapped()
				}
			}
			oldSt = newSt // a line both sides share leaves them alike
			oldN++
			newN++
		}
	}
	return true
}

// diffLine is one added (+) or removed (-) line at number n, its code
// highlighted on the line's colour and, when it pairs with a line on the
// other side, the words that changed a step brighter.
func (d *drawer) diffLine(pad string, w int, lg *lang, st *hlState, sign byte, n int, code, pair string, paired bool) {
	body, other := expandTabs(code), expandTabs(pair)
	if showSpace {
		body, other = stripANSI(code), stripANSI(pair) // tabs drawn as →
	}
	row, hi, mark := bgAdd, bgAddHi, plusSign()
	if sign == '-' {
		row, hi, mark = bgDel, bgDelHi, minusSign()
	}
	var em *emph
	if paired {
		if from, to, ok := changed(body, other); ok {
			em = &emph{from: from, to: to, on: hi, off: row}
		}
	}
	for i, r := range d.codeRows(paintCode(lg, st, body, cText, em, showSpace), w) {
		if i == 0 {
			d.add("", row, pad+faint(fmt.Sprintf("%5d ", n))+mark+" "+r, "")
		} else {
			d.add("", row, pad+blanks(8)+r, "")
			d.wrapped()
		}
	}
}

// codeRows wraps a highlighted line of code to rows w cells wide, keeping
// its colours across the breaks. Past a few rows the rest is cut, unless
// verbose.
func (d *drawer) codeRows(s string, w int) []string {
	w = max(w, 4)
	n := cellw.String(s)
	if n <= w {
		return []string{s}
	}
	most := 6
	if d.o.Verbose {
		most = n
	}
	var rows []string
	for at := 0; at < n; at += w {
		if len(rows) == most-1 && at+w < n {
			rows = append(rows, ansi.Truncate(ansi.Cut(s, at, n), w, "›"))
			break
		}
		rows = append(rows, ansi.Cut(s, at, min(at+w, n)))
	}
	return rows
}

// collapseCR keeps only the last state of lines redrawn with carriage
// returns, so progress bars show where they ended.
func collapseCR(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if j := strings.LastIndex(strings.TrimRight(l, "\r"), "\r"); j >= 0 {
			lines[i] = l[j+1:]
		}
	}
	return strings.Join(lines, "\n")
}

// diffText is a line of a diff highlighted in lg over colour c, cut to w
// cells, its spaces and tabs marked when that's on.
func diffText(lg *lang, st *hlState, s, c string, w int) string {
	if !showSpace {
		return highlight(lg, st, truncateCells(expandTabs(s), w), c, nil)
	}
	return ansi.Truncate(paintCode(lg, st, stripANSI(s), c, nil, true), max(w, 4), "›")
}

func expandTabs(s string) string { return strings.ReplaceAll(stripANSI(s), "\t", "    ") }

func truncateCells(s string, w int) string {
	if w < 4 {
		w = 4
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "›"
	}
	return s
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// figure draws what Claude showed with agtop's show tool: the drawing as it
// was sent, in a frame of its own as wide as the pane allows, never wrapped
// or folded. The top edge carries its title and is the row you pick to copy
// it. It reports false while there is no drawing yet to show.
func (d *drawer) figure(st *Step, ref string, indent int) bool {
	rows := Drawing(st)
	if rows == nil {
		return false
	}
	var in agtools.ShowInput
	_ = json.Unmarshal(st.Input, &in)
	title := firstNonEmpty(oneLine(in.Title), "drawing")
	pad := d.spine() + blanks(indent-1)
	wide := 0
	for _, r := range rows {
		wide = max(wide, cellw.String(r))
	}
	room := max(8, d.o.Width-cellw.String(pad)-4) // "│ " … " │"
	if n := len(d.lines); n > 0 && strings.TrimSpace(stripANSI(d.lines[n-1].Text)) != strings.TrimSpace(stripANSI(d.spine())) {
		d.blank()
	}
	inner := min(max(wide, cellw.String(title)+6), room)
	edge := func(l, r, label string) string {
		head := faint(l + "─")
		if label != "" {
			head += " " + label + " "
		}
		fill := inner + 2 - cellw.String(head) + 1
		if fill < 1 {
			head = ansi.Truncate(head, inner+1, "…")
			fill = inner + 3 - cellw.String(head)
		}
		return pad + head + faint(strings.Repeat("─", max(0, fill))+r)
	}
	d.addWide(ref, edge("╭", "╮", glyphColor("◇")+" "+text(title)))
	for _, r := range rows {
		if cellw.String(r) > inner {
			r = ansi.Truncate(r, inner-1, "") + faint("›")
		}
		d.addWide("", pad+faint("│")+" "+paint(cWhite, r)+blanks(inner-cellw.String(r))+" "+faint("│"))
	}
	foot := ""
	if wide > inner {
		foot = dim(fmt.Sprintf("%d more columns · pick it, alt+c copies it whole", wide-inner))
	}
	d.addWide("", edge("╰", "╯", foot))
	d.blank()
	return true
}

// Drawing is the drawing a show step carries, one string a row, tabs as
// spaces and blank rows at either end dropped; nil if it has none yet.
func Drawing(st *Step) []string {
	if st == nil || st.Tool != agtools.Show {
		return nil
	}
	var in agtools.ShowInput
	if json.Unmarshal(st.Input, &in) != nil {
		return nil
	}
	rows := strings.Split(expandTabs(collapseCR(in.Drawing)), "\n")
	for len(rows) > 0 && strings.TrimSpace(rows[0]) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// addWide is add for a row that may use the pane's whole width rather than
// stopping where numbers line up.
func (d *drawer) addWide(ref, left string) {
	b := ""
	if ref != "" && ref == d.o.Selected {
		b = bgSelU
		mark := faint("▍")
		if d.o.Focused {
			b, mark = bgSel, paint(cOrange, "▍")
		}
		left = mark + strings.TrimPrefix(left, d.spine())
	}
	d.lines = append(d.lines, Line{Text: row(b, left, "", d.o.Width, d.o.Width), Ref: ref})
}
